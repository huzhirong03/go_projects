package extractor

import (
	"context"
	"path/filepath"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// perKeywordSurgeryWriter：单源场景下的 per_keyword 输出，走 zip 手术。
//
// 与老 perKeywordWriter（流式 excelize）对比：
//
// | 维度         | perKeywordWriter（excelize） | perKeywordSurgeryWriter（zip 手术） |
// |--------------|------------------------------|-------------------------------------|
// | 样式保真     | 部分（列宽/字体 OK，条件格式丢） | 100%（字节级复制）                  |
// | 图片保真     | 98%（excelize 偏移约 5px）    | 100%（anchor 字节搬运）             |
// | 速度         | 基线                         | 快 30-50%（图片零解码）             |
// | 适用场景     | 任意（含多源 per_keyword）    | **只支持单源**                      |
//
// 单源判定：pipeline 层在 scan 结束后根据 countDistinctPaths(files)==1 选择。
// 多源场景用户可以改用 per_source 策略（已是 zip 手术）。
//
// EmitRow 只累积命中的源行号（按关键词分桶 + sheet 分桶），Finalize 时对每个
// 关键词调一次 excelio.ExtractToNewFileSurgery，产出 N 个文件（N = 关键词数）。
type perKeywordSurgeryWriter struct {
	outDir    string
	prefix    string
	headerRow int

	schema *UnifiedSchema
	dedup  *deduper

	// kw -> sheet -> []sourceRow
	hits map[string]map[string][]int
	// kw -> source path（单源场景下每个 kw 只对应一个源）
	kwSource map[string]string
	// kw -> 命中行包含的图片数（仅统计用，不触发迁移）
	picCount map[string]int

	imgCount int
	ts       string
}

func newPerKeywordSurgeryWriter(outDir, prefix string, headerRow int, dedupCfg dedupConfig) *perKeywordSurgeryWriter {
	return &perKeywordSurgeryWriter{
		outDir:    outDir,
		prefix:    prefix,
		headerRow: headerRow,
		dedup:     newDeduper(dedupCfg),
		hits:      map[string]map[string][]int{},
		kwSource:  map[string]string{},
		picCount:  map[string]int{},
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
	// CSV 源理论上不会走到这里（pipeline 层已保证单源=单 xlsx 时才选 surgery writer）
	// 防御：遇到 CSV 返回错误提示降级
	if core.DetectSourceKind(row.SourceFile) == core.SourceCSV {
		return core.New("SURGERY_UNSUPPORTED_CSV",
			"perKeywordSurgeryWriter 不支持 CSV 源；调用方应改用 perKeywordWriter")
	}
	if _, ok := p.hits[row.MatchedKW]; !ok {
		p.hits[row.MatchedKW] = map[string][]int{}
	}
	sheet := fs.File.SheetName
	p.hits[row.MatchedKW][sheet] = append(p.hits[row.MatchedKW][sheet], row.SourceRow)
	p.kwSource[row.MatchedKW] = row.SourceFile
	p.picCount[row.MatchedKW] += len(row.Pictures)
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
	// 为保证可重复顺序：按关键词排序处理。但 MatchedKW 可能含中文，这里直接按
	// map 迭代序——Go 标准做法（调用方不应依赖输出文件顺序）。
	for kw, sheetHits := range p.hits {
		fname := sanitizeFileName(p.prefix+kw) + "_" + p.ts + ".xlsx"
		outPath := filepath.Join(p.outDir, fname)

		srcPath, ok := p.kwSource[kw]
		if !ok {
			continue
		}

		// 文件占用/可读性校验：用 merged writer 的同款 retry/skip/cancel 对话框
		skip := false
		switch askOfficeLockDecision(ctx, emitter, srcPath) {
		case fileOpenSkip:
			if emitter != nil {
				emitter.Log(core.LogWarn, "已跳过正在打开的文件: "+srcPath)
			}
			skip = true
		case fileOpenCancel:
			return paths, core.ErrCanceled
		}
		if skip {
			continue
		}

		// 构造 specs：每个命中 sheet 一条 spec。保留源 sheet 名（用户的数据结构
		// 感知一致；如果后续改成"关键词作为 sheet 名"是小改动）。
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
			return paths, err
		}
		paths = append(paths, outPath)
		p.imgCount += p.picCount[kw]
	}
	return paths, nil
}

func (p *perKeywordSurgeryWriter) Close() error { return nil }

func (p *perKeywordSurgeryWriter) ImagesMigrated() int { return p.imgCount }
