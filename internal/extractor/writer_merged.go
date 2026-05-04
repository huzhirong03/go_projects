package extractor

import (
	"archive/zip"
	"context"
	"path/filepath"
	"sort"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// mergedWriter：所有源文件的命中行合并到一个输出文件，**使用 zip 手术原汁原味**。
//
// 实现思路：
//   - 以“命中行最多”的源作为 primary（模板母体）。primary 完整走 zip 手术：
//     表头、命中行、图片锁单元格、样式全部 1:1 复刻。
//   - 其他源作为 secondary：命中行插入 primary 表末，样式用模板列样式。
//   - 图片跨源迁移到 primary 的 xl/media/，drawing 结构跟随追写。
//
// 限制（MVP）：仅多个源重复使用同一个 sheet 名（例如都叫“商品清单”）时才能合并。
// 多 sheet 命中时只取每个源“命中行最多的那个 sheet”作代表。
type mergedWriter struct {
	outDir   string
	prefix   string                    // 文件名前缀，默认为空串
	schema   *UnifiedSchema            // Begin 时保存，供 deduper bind 去重列使用
	dedup    *deduper                  // V1.1+：去重器；column="" 时为 no-op
	hits     map[string]*perSourceHits // key = 源文件路径，复用 per_source 的累积结构
	imgCount int
	ts       string
}

func newMergedWriter(outDir, sheet, prefix string, dedupCfg dedupConfig) *mergedWriter {
	_ = sheet // 不再需要预设“结果”底 sheet，名字从 primary 源文件继承
	return &mergedWriter{
		outDir: outDir,
		prefix: prefix,
		dedup:  newDeduper(dedupCfg),
		hits:   map[string]*perSourceHits{},
		ts:     timestamp(),
	}
}

// Begin 保存 schema 用于 deduper bind。不重写表头（zip 手术路径继承源文件的表头）。
func (m *mergedWriter) Begin(schema *UnifiedSchema) error {
	m.schema = schema
	m.dedup.Bind(schema.Columns)
	return nil
}

// EmitRow 仅累积命中信息；真正的文件操作在 Finalize 里做。
// CSV 源不走 zip 手术，改用流式合并：额外缓存整行内容到 csvRows。
//
// 去重判断在最前面：bucket="" 代表全局唯一桶，所有源所有 sheet 合并后按去重列去重。
// CSV 和 xlsx 两条路径都受此影响（保持策略一致）。
func (m *mergedWriter) EmitRow(row MatchedRow, fs *FileSchema) error {
	if m.dedup.ShouldDrop("", row.Values) {
		return nil
	}
	h, ok := m.hits[row.SourceFile]
	if !ok {
		h = &perSourceHits{path: row.SourceFile, sheetRows: map[string][]int{}}
		m.hits[row.SourceFile] = h
	}
	if core.DetectSourceKind(row.SourceFile) == core.SourceCSV {
		h.csvRows = append(h.csvRows, row)
		h.csvSchema = fs
		return nil
	}
	sheet := fs.File.SheetName
	h.sheetRows[sheet] = append(h.sheetRows[sheet], row.SourceRow)
	h.picCount += len(row.Pictures)
	return nil
}

// hasCSVSource 是否含 CSV 源（决定 finalize 走流式还是 zip 手术路径）。
func (m *mergedWriter) hasCSVSource() bool {
	for path := range m.hits {
		if core.DetectSourceKind(path) == core.SourceCSV {
			return true
		}
	}
	return false
}

// singleSourceHit 判断是否只有唯一一个 xlsx 源。返回其路径和 hit；
// 否则返回空路径和 nil。调用方用来决定走"多 sheet 保留"直通路径。
func (m *mergedWriter) singleSourceHit() (string, *perSourceHits) {
	if len(m.hits) != 1 {
		return "", nil
	}
	for path, h := range m.hits {
		// 额外保险：跳过 CSV（上游已有 hasCSVSource 过滤，这里防御）
		if core.DetectSourceKind(path) == core.SourceCSV {
			return "", nil
		}
		return path, h
	}
	return "", nil
}

// finalizeSingleSource 单源场景的直通路径：
//
// 直接对源文件走一次 CloneAndExtractZipMulti，传入该源**所有命中 sheet** 的
// keepRows map。好处：
//   - 保留所有命中 sheet（避免数据丢失）
//   - 保留 sheet 间的公式引用 / 数据验证 / 命名区域（避免 Excel "部分内容有问题"）
//   - 图片锚点按 zip 手术规则自动随命中行重映射
//
// 注意：如果源 xlsx 里还有"没命中任何行"的 sheet，会被 zip surgery 删掉。
// 对用户来说这符合 merged 语义（只要命中的内容）。
func (m *mergedWriter) finalizeSingleSource(
	ctx context.Context, emitter core.EventEmitter,
	path string, h *perSourceHits,
) ([]string, error) {
	// 走 filterMergePlans 的最小化等效：检查文件可读，遇到被占用时让用户 retry/skip
	probe := []mergePlan{{path: h.path, sheet: "", rows: nil}}
	filtered, err := filterMergePlans(ctx, emitter, probe)
	if err != nil {
		return nil, err
	}
	if len(filtered) == 0 {
		return nil, nil // 用户选择跳过
	}

	// 构造 keepSheetRows：每个命中 sheet 带表头 + 命中行（去重 + 排序）
	headerRow := 1 // 与原 finalize 一致
	keepMap := make(map[string][]int, len(h.sheetRows))
	for sn, rs := range h.sheetRows {
		withHeader := append([]int{headerRow}, rs...)
		keepMap[sn] = excelio.SortedUnique(withHeader)
	}
	if len(keepMap) == 0 {
		return nil, nil
	}

	outPath := filepath.Join(m.outDir,
		sanitizeFileName(m.prefix+"搜索结果")+"_"+m.ts+".xlsx")
	if err := excelio.CloneAndExtractZipMulti(path, outPath, keepMap); err != nil {
		return nil, err
	}
	m.imgCount += h.picCount
	return []string{outPath}, nil
}

// Finalize 选 primary（命中行最多的源）后调 zip 手术一次性合并。
func (m *mergedWriter) Finalize() ([]string, error) {
	return m.finalize(nil, nil)
}

func (m *mergedWriter) FinalizeWithPrompt(ctx context.Context, emitter core.EventEmitter) ([]string, error) {
	return m.finalize(ctx, emitter)
}

func (m *mergedWriter) finalize(ctx context.Context, emitter core.EventEmitter) ([]string, error) {
	if len(m.hits) == 0 {
		return nil, nil
	}
	// 混合/纯 CSV 源时退化为流式合并，不走 zip 手术（CSV 不是 zip）。
	// 代价：xlsx 源的样式/图片也会按流式输出失去保真；
	// 选了 merged 且涉及 CSV 就接受这一点，需要保真请用 per_source。
	if m.hasCSVSource() {
		return m.finalizeStreaming(ctx, emitter)
	}

	// 单源多 sheet 命中时走直通：保留该源所有命中的 sheet（不 merge），
	// 避免：
	//   1) 数据丢失 —— 原逻辑只选 bestSheet，其他 sheet 的命中行被吞
	//   2) Excel "部分内容有问题" —— 跨 sheet 公式（如 "=年级统计!A1"）和
	//      跨 sheet 数据验证 在其他 sheet 被删后悬空引用
	if path, hit := m.singleSourceHit(); hit != nil {
		return m.finalizeSingleSource(ctx, emitter, path, hit)
	}

	// 按“在某个 sheet 里命中行最多”选 primary；同时记下每个源作代的 sheet。
	plans := make([]mergePlan, 0, len(m.hits))
	for _, h := range m.hits {
		bestSheet := ""
		bestRows := []int(nil)
		for sn, rs := range h.sheetRows {
			if len(rs) > len(bestRows) {
				bestSheet = sn
				bestRows = rs
			}
		}
		plans = append(plans, mergePlan{path: h.path, sheet: bestSheet, rows: bestRows, picSrc: h.picCount})
	}
	filtered, err := filterMergePlans(ctx, emitter, plans)
	if err != nil {
		return nil, err
	}
	plans = filtered
	if len(plans) == 0 {
		return nil, nil
	}

	// 选 primary：hits 最多优先，同位按路径字典序，保证可重复。
	sort.Slice(plans, func(i, j int) bool {
		if len(plans[i].rows) != len(plans[j].rows) {
			return len(plans[i].rows) > len(plans[j].rows)
		}
		return plans[i].path < plans[j].path
	})
	primary := plans[0]

	// 输出文件名：<prefix>搜索结果_<时间戳>.xlsx（prefix 默认空）
	outPath := filepath.Join(m.outDir, sanitizeFileName(m.prefix+"搜索结果")+"_"+m.ts+".xlsx")

	// 构造 primary 的 keepRows：表头行 + 命中行（去重、排序由下层处理）
	// 表头行从 task.HeaderRow 传进来，这里没有，默认 1（与原实现一致）。
	headerRow := 1
	primaryKeep := append([]int{headerRow}, primary.rows...)

	ps := excelio.MergeSource{
		SrcPath:   primary.path,
		SheetName: primary.sheet,
		KeepRows:  excelio.SortedUnique(primaryKeep),
	}

	usedPicCount := primary.picSrc
	// 其他源作为 secondaries，KeepRows 不含表头（表头从 primary 继承）。
	var secs []excelio.MergeSource
	for _, p := range plans[1:] {
		if p.sheet != primary.sheet {
			// MVP 限制：sheet 名必须一致；不一致的跳过（不报错，避免打断其他有效源）
			continue
		}
		secs = append(secs, excelio.MergeSource{
			SrcPath:   p.path,
			SheetName: p.sheet,
			KeepRows:  excelio.SortedUnique(p.rows),
		})
		usedPicCount += p.picSrc
	}

	if err := excelio.CloneAndMergePreserved(ps, outPath, secs); err != nil {
		return nil, err
	}

	m.imgCount += usedPicCount

	return []string{outPath}, nil
}

func (m *mergedWriter) Close() error { return nil }

func (m *mergedWriter) ImagesMigrated() int { return m.imgCount }

type mergePlan struct {
	path   string
	sheet  string
	rows   []int
	picSrc int
}

func filterMergePlans(ctx context.Context, emitter core.EventEmitter, plans []mergePlan) ([]mergePlan, error) {
	out := make([]mergePlan, 0, len(plans))
	for _, p := range plans {
		for {
			skipFile := false
			switch askOfficeLockDecision(ctx, emitter, p.path) {
			case fileOpenRetry:
				continue
			case fileOpenSkip:
				emitter.Log(core.LogWarn, "已跳过正在打开的文件: "+p.path)
				skipFile = true
			case fileOpenCancel:
				return nil, core.ErrCanceled
			}
			if skipFile {
				break
			}
			err := probeZipReadable(p.path)
			if err == nil {
				out = append(out, p)
				break
			}
			switch askFileOpenDecision(ctx, emitter, p.path, err) {
			case fileOpenRetry:
				continue
			case fileOpenSkip:
				emitter.Log(core.LogWarn, "已跳过无法读取的文件: "+p.path)
			case fileOpenAbort:
				return nil, err
			default:
				return nil, core.ErrCanceled
			}
			break
		}
	}
	return out, nil
}

func probeZipReadable(path string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return core.Wrap("SRC_OPEN_FAILED", "打开源 xlsx 失败: "+path, err)
	}
	return zr.Close()
}
