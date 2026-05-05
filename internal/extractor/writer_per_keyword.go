package extractor

import (
	"path/filepath"

	"excel-master/internal/core"
)

// perKeywordWriter：每个关键词一个输出文件。
// 文件命名：<prefix><关键词>_<时间戳>.xlsx。prefix 默认为空，可传如 "搜索_"
// 变成 "搜索_<关键词>_<时间戳>.xlsx"。关键词含非法字符自动替换。
type perKeywordWriter struct {
	outDir   string
	sheet    string
	prefix   string
	schema   *UnifiedSchema
	dedup    *deduper                 // V1.1+：按关键词分桶去重；column="" 时为 no-op
	streams  map[string]*outputStream // key = 关键词原文
	imgCount int
	ts       string
}

func newPerKeywordWriter(outDir, sheet, prefix string, dedupCfg dedupConfig) *perKeywordWriter {
	return &perKeywordWriter{
		outDir:  outDir,
		sheet:   sheet,
		prefix:  prefix,
		dedup:   newDeduper(dedupCfg),
		streams: map[string]*outputStream{},
		ts:      timestamp(),
	}
}

func (p *perKeywordWriter) Begin(schema *UnifiedSchema) error {
	p.schema = schema
	p.dedup.Bind(schema.Columns)
	return nil
}

func (p *perKeywordWriter) getOrCreate(kw string) (*outputStream, error) {
	if s, ok := p.streams[kw]; ok {
		return s, nil
	}
	fname := sanitizeFileName(p.prefix+kw) + "_" + p.ts + ".xlsx"
	outPath := filepath.Join(p.outDir, fname)
	s, err := openOutput(outPath, p.sheet)
	if err != nil {
		return nil, err
	}
	if err := s.applyColumnWidthsIfNeeded(p.schema.UnifiedColumnWidths); err != nil {
		_ = s.close()
		return nil, err
	}
	if err := s.writeHeader(p.schema.Columns); err != nil {
		_ = s.close()
		return nil, err
	}
	p.streams[kw] = s
	return s, nil
}

func (p *perKeywordWriter) EmitRow(row MatchedRow, fs *FileSchema) error {
	if p.schema == nil {
		return core.New("WRITER_NOT_BEGAN", "调用 Begin 之前就 EmitRow")
	}
	// 去重：bucket = 关键词。同一关键词的文件内部去重，跨关键词的重复不会被误删。
	if p.dedup.ShouldDrop(row.MatchedKW, row.Values) {
		return nil
	}
	s, err := p.getOrCreate(row.MatchedKW)
	if err != nil {
		return err
	}
	dstRow, err := s.writeRow(row.Values, row.SourceRow, row.RowHeight)
	if err != nil {
		return err
	}
	// 第一次带图命中行：同步把源行 ht 复制到目标 sheet 的 defaultRowHeight，
	// 避免 excelize.AddPictureFromBytes 按 15pt 反算 to.row 把图片撑成 2~3 倍高。
	if len(row.Pictures) > 0 {
		s.ensureDefaultHeightForPics(row.RowHeight)
	}
	n, err := s.migratePictures(row.Pictures, fs, dstRow, len(p.schema.Columns))
	p.imgCount += n
	return err
}

func (p *perKeywordWriter) Finalize() ([]string, error) {
	paths := make([]string, 0, len(p.streams))
	for _, s := range p.streams {
		if err := s.save(); err != nil {
			return paths, err
		}
		paths = append(paths, s.path)
	}
	return paths, nil
}

func (p *perKeywordWriter) Close() error {
	for _, s := range p.streams {
		_ = s.close()
	}
	return nil
}

// ImagesMigrated 暴露给上层做汇总。
func (p *perKeywordWriter) ImagesMigrated() int { return p.imgCount }
