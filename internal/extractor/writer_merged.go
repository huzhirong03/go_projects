package extractor

import (
	"path/filepath"
	"sort"

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
	hits     map[string]*perSourceHits // key = 源文件路径，复用 per_source 的累积结构
	imgCount int
	ts       string
}

func newMergedWriter(outDir, sheet string) *mergedWriter {
	_ = sheet // 不再需要预设“结果”底 sheet，名字从 primary 源文件继承
	return &mergedWriter{
		outDir: outDir,
		hits:   map[string]*perSourceHits{},
		ts:     timestamp(),
	}
}

// Begin 对本 writer 是 no-op：schema 原汁原味路径不需要（不重写表头）。
func (m *mergedWriter) Begin(schema *UnifiedSchema) error { return nil }

// EmitRow 仅累积命中信息；真正的文件操作在 Finalize 里做。
func (m *mergedWriter) EmitRow(row MatchedRow, fs *FileSchema) error {
	h, ok := m.hits[row.SourceFile]
	if !ok {
		h = &perSourceHits{path: row.SourceFile, sheetRows: map[string][]int{}}
		m.hits[row.SourceFile] = h
	}
	sheet := fs.File.SheetName
	h.sheetRows[sheet] = append(h.sheetRows[sheet], row.SourceRow)
	h.picCount += len(row.Pictures)
	return nil
}

// Finalize 选 primary（命中行最多的源）后调 zip 手术一次性合并。
func (m *mergedWriter) Finalize() ([]string, error) {
	if len(m.hits) == 0 {
		return nil, nil
	}

	// 按“在某个 sheet 里命中行最多”选 primary；同时记下每个源作代的 sheet。
	type srcPlan struct {
		path   string
		sheet  string
		rows   []int
		picSrc int
	}
	plans := make([]srcPlan, 0, len(m.hits))
	for _, h := range m.hits {
		bestSheet := ""
		bestRows := []int(nil)
		for sn, rs := range h.sheetRows {
			if len(rs) > len(bestRows) {
				bestSheet = sn
				bestRows = rs
			}
		}
		plans = append(plans, srcPlan{path: h.path, sheet: bestSheet, rows: bestRows, picSrc: h.picCount})
	}

	// 选 primary：hits 最多优先，同位按路径字典序，保证可重复。
	sort.Slice(plans, func(i, j int) bool {
		if len(plans[i].rows) != len(plans[j].rows) {
			return len(plans[i].rows) > len(plans[j].rows)
		}
		return plans[i].path < plans[j].path
	})
	primary := plans[0]

	// 输出文件名：沿用 “搜索结果_<时间戳>.xlsx”
	outPath := filepath.Join(m.outDir, "搜索结果_"+m.ts+".xlsx")

	// 构造 primary 的 keepRows：表头行 + 命中行（去重、排序由下层处理）
	// 表头行从 task.HeaderRow 传进来，这里没有，默认 1（与原实现一致）。
	headerRow := 1
	primaryKeep := append([]int{headerRow}, primary.rows...)

	ps := excelio.MergeSource{
		SrcPath:   primary.path,
		SheetName: primary.sheet,
		KeepRows:  excelio.SortedUnique(primaryKeep),
	}

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
	}

	if err := excelio.CloneAndMergePreserved(ps, outPath, secs); err != nil {
		return nil, err
	}

	// 统计迁移图片数：所有源的 picCount 加总
	for _, h := range m.hits {
		m.imgCount += h.picCount
	}

	return []string{outPath}, nil
}

func (m *mergedWriter) Close() error { return nil }

func (m *mergedWriter) ImagesMigrated() int { return m.imgCount }
