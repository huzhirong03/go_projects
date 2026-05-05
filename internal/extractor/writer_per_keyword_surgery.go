package extractor

import (
	"context"
	"path/filepath"
	"sort"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// perKeywordSurgeryWriter：xlsx 源（单源 + 多源）的 per_keyword 输出，走 zip 手术。
//
// 与老 perKeywordWriter（流式 excelize）对比：
//
// | 维度         | perKeywordWriter（excelize） | perKeywordSurgeryWriter（zip 手术）         |
// |--------------|------------------------------|---------------------------------------------|
// | 样式保真     | 部分（列宽/字体 OK，条件格式丢） | 单源 100% / 多源同模板 ~99%                 |
// | 图片保真     | 98%（excelize 偏移约 5px）    | 100%（anchor 字节搬运）                     |
// | 速度         | 基线                         | 快 30-50%（图片零解码）                     |
// | 适用场景     | 任意（含 CSV 源）            | xlsx 源（CSV 走原 perKeywordWriter）        |
//
// 单/多源 判定：pipeline 层在 scan 结束后根据 isAllXlsxSources(files) 选择。
// 多源场景下每个关键词的命中可能来自多个源，Finalize 内部按关键词遍历：
//   - 该 kw 只有 1 个源命中 → 调 excelio.ExtractToNewFileSurgery（100% 字节级复刻）
//   - 该 kw 有多个源命中    → 调 excelio.CloneAndMergePreserved（primary 100% +
//     secondary 数据嫁接到 primary 模板，同模板克隆场景下 ~99% 复刻）
//
// 限制（与 mergedWriter 一致，因为复用同一个底层 API）：
//   - secondary 文件单独添加的特殊样式 / 合并单元格 / 条件格式 / 数据验证会丢
//   - secondary 公式取计算结果（行号无法跨源平移，物理限制）
type perKeywordSurgeryWriter struct {
	outDir    string
	prefix    string
	headerRow int

	schema *UnifiedSchema
	dedup  *deduper

	// hits[kw][srcPath][sheet] = []1-based 源行号
	hits map[string]map[string]map[string][]int
	// picCount[kw][srcPath] = 命中行包含的图片数（仅统计用）
	picCount map[string]map[string]int

	imgCount int
	ts       string
}

func newPerKeywordSurgeryWriter(outDir, prefix string, headerRow int, dedupCfg dedupConfig) *perKeywordSurgeryWriter {
	return &perKeywordSurgeryWriter{
		outDir:    outDir,
		prefix:    prefix,
		headerRow: headerRow,
		dedup:     newDeduper(dedupCfg),
		hits:      map[string]map[string]map[string][]int{},
		picCount:  map[string]map[string]int{},
		ts:        timestamp(),
	}
}

func (p *perKeywordSurgeryWriter) Begin(schema *UnifiedSchema) error {
	p.schema = schema
	p.dedup.Bind(schema.Columns)
	return nil
}

// EmitRow 仅累积命中行号；真正的文件操作在 Finalize 里做。
// 去重：bucket = 关键词，同一关键词内按源行内容去重，跨关键词不互相影响。
func (p *perKeywordSurgeryWriter) EmitRow(row MatchedRow, fs *FileSchema) error {
	if p.schema == nil {
		return core.New("WRITER_NOT_BEGAN", "调用 Begin 之前就 EmitRow")
	}
	if p.dedup.ShouldDrop(row.MatchedKW, row.Values) {
		return nil
	}
	// CSV 源理论上不会走到这里（pipeline 层已保证 isAllXlsxSources 才选 surgery writer）
	// 防御：遇到 CSV 返回错误提示降级
	if core.DetectSourceKind(row.SourceFile) == core.SourceCSV {
		return core.New("SURGERY_UNSUPPORTED_CSV",
			"perKeywordSurgeryWriter 不支持 CSV 源；调用方应改用 perKeywordWriter")
	}
	kw := row.MatchedKW
	src := row.SourceFile
	sheet := fs.File.SheetName
	if _, ok := p.hits[kw]; !ok {
		p.hits[kw] = map[string]map[string][]int{}
		p.picCount[kw] = map[string]int{}
	}
	if _, ok := p.hits[kw][src]; !ok {
		p.hits[kw][src] = map[string][]int{}
	}
	p.hits[kw][src][sheet] = append(p.hits[kw][src][sheet], row.SourceRow)
	p.picCount[kw][src] += len(row.Pictures)
	return nil
}

func (p *perKeywordSurgeryWriter) Finalize() ([]string, error) {
	return p.finalize(nil, nil)
}

func (p *perKeywordSurgeryWriter) FinalizeWithPrompt(ctx context.Context, emitter core.EventEmitter) ([]string, error) {
	return p.finalize(ctx, emitter)
}

func (p *perKeywordSurgeryWriter) finalize(ctx context.Context, emitter core.EventEmitter) ([]string, error) {
	paths := make([]string, 0, len(p.hits))
	for kw, srcMap := range p.hits {
		fname := sanitizeFileName(p.prefix+kw) + "_" + p.ts + ".xlsx"
		outPath := filepath.Join(p.outDir, fname)

		var (
			outP string
			err  error
		)
		if len(srcMap) == 1 {
			// 单源：100% 字节级复刻
			outP, err = p.exportSingleSource(ctx, emitter, kw, srcMap, outPath)
		} else {
			// 多源：primary + secondary 嫁接（同模板克隆场景 ~99% 复刻）
			outP, err = p.exportMultiSource(ctx, emitter, kw, srcMap, outPath)
		}
		if err != nil {
			return paths, err
		}
		if outP != "" {
			paths = append(paths, outP)
		}
	}
	return paths, nil
}

// exportSingleSource 处理某个关键词只有 1 个源命中的场景：
// 直接调 ExtractToNewFileSurgery，所有命中 sheet 各一条 spec。
func (p *perKeywordSurgeryWriter) exportSingleSource(
	ctx context.Context, emitter core.EventEmitter,
	kw string, srcMap map[string]map[string][]int, outPath string,
) (string, error) {
	var srcPath string
	var sheetHits map[string][]int
	for s, sh := range srcMap {
		srcPath = s
		sheetHits = sh
		break
	}

	if decision := askOfficeLockDecision(ctx, emitter, srcPath); decision == fileOpenSkip {
		if emitter != nil {
			emitter.Log(core.LogWarn, "已跳过正在打开的文件: "+srcPath)
		}
		return "", nil
	} else if decision == fileOpenCancel {
		return "", core.ErrCanceled
	}

	specs := make([]excelio.InplaceSheetSpec, 0, len(sheetHits))
	seen := map[string]struct{}{}
	for sheet, rows := range sheetHits {
		keep := make([]int, 0, len(rows)+1)
		if p.headerRow > 0 {
			keep = append(keep, p.headerRow)
		}
		keep = append(keep, rows...)
		keep = excelio.SortedUnique(keep)
		newName := excelio.UniqueNameInSet(sheet, seen)
		specs = append(specs, excelio.InplaceSheetSpec{
			SourceSheet:  sheet,
			NewSheetName: newName,
			KeepRows:     keep,
		})
	}
	if err := excelio.ExtractToNewFileSurgery(srcPath, outPath, specs); err != nil {
		return "", err
	}
	p.imgCount += p.picCount[kw][srcPath]
	return outPath, nil
}

// exportMultiSource 处理某个关键词有多个源命中的场景：
// 按命中行数排序，命中最多的源做 primary（模板母体），其他做 secondary。
// 复用 mergedWriter 同款的 CloneAndMergePreserved 嫁接路径。
//
// 限制（与 mergedWriter 一致）：
//   - 所有源必须用相同的 SheetName；不一致的 secondary 自动跳过
//   - 每个源选"命中行最多的那个 sheet"作代表（与 mergedWriter 一致）
func (p *perKeywordSurgeryWriter) exportMultiSource(
	ctx context.Context, emitter core.EventEmitter,
	kw string, srcMap map[string]map[string][]int, outPath string,
) (string, error) {
	// 1. 每源选"命中行最多的 sheet"
	type srcPlan struct {
		path  string
		sheet string
		rows  []int
		pics  int
	}
	plans := make([]srcPlan, 0, len(srcMap))
	for src, sheetHits := range srcMap {
		bestSheet := ""
		var bestRows []int
		for sn, rs := range sheetHits {
			if len(rs) > len(bestRows) {
				bestSheet = sn
				bestRows = rs
			}
		}
		plans = append(plans, srcPlan{
			path: src, sheet: bestSheet, rows: bestRows,
			pics: p.picCount[kw][src],
		})
	}

	// 2. 文件占用预检 + 跳过被锁定的源
	filtered := make([]srcPlan, 0, len(plans))
	for _, pl := range plans {
		switch askOfficeLockDecision(ctx, emitter, pl.path) {
		case fileOpenSkip:
			if emitter != nil {
				emitter.Log(core.LogWarn, "已跳过正在打开的文件: "+pl.path)
			}
			continue
		case fileOpenCancel:
			return "", core.ErrCanceled
		}
		filtered = append(filtered, pl)
	}
	if len(filtered) == 0 {
		return "", nil
	}
	plans = filtered

	// 3. 选 primary：rows 最多优先；同位按路径字典序保证可重复
	sort.Slice(plans, func(i, j int) bool {
		if len(plans[i].rows) != len(plans[j].rows) {
			return len(plans[i].rows) > len(plans[j].rows)
		}
		return plans[i].path < plans[j].path
	})
	primary := plans[0]

	// 4. 构造 primary 的 KeepRows = headerRow + 命中行（去重 + 排序）
	primaryKeep := primary.rows
	if p.headerRow > 0 {
		primaryKeep = append([]int{p.headerRow}, primary.rows...)
	}
	ps := excelio.MergeSource{
		SrcPath:   primary.path,
		SheetName: primary.sheet,
		KeepRows:  excelio.SortedUnique(primaryKeep),
	}

	// 5. secondary：sheet 名必须等于 primary（CloneAndMergePreserved 限制）
	usedPics := primary.pics
	var secs []excelio.MergeSource
	for _, pl := range plans[1:] {
		if pl.sheet != primary.sheet {
			continue // sheet 名不一致：跳过避免 API 报错
		}
		secs = append(secs, excelio.MergeSource{
			SrcPath:   pl.path,
			SheetName: pl.sheet,
			KeepRows:  excelio.SortedUnique(pl.rows),
		})
		usedPics += pl.pics
	}

	if err := excelio.CloneAndMergePreserved(ps, outPath, secs); err != nil {
		return "", err
	}
	p.imgCount += usedPics
	return outPath, nil
}

func (p *perKeywordSurgeryWriter) Close() error { return nil }

func (p *perKeywordSurgeryWriter) ImagesMigrated() int { return p.imgCount }
