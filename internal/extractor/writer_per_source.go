package extractor

import (
	"path/filepath"
	"strings"

	"excel-master/internal/core"
)

// perSourceWriter：每个源文件一个输出文件。
// 文件命名：<源文件名去扩展名>_已提取_<时间戳>.xlsx
type perSourceWriter struct {
	outDir   string
	sheet    string
	schema   *UnifiedSchema
	streams  map[string]*outputStream // key = 源文件绝对路径
	imgCount int
	ts       string
}

func newPerSourceWriter(outDir, sheet string) *perSourceWriter {
	return &perSourceWriter{
		outDir:  outDir,
		sheet:   sheet,
		streams: map[string]*outputStream{},
		ts:      timestamp(),
	}
}

func (p *perSourceWriter) Begin(schema *UnifiedSchema) error {
	p.schema = schema
	return nil
}

func (p *perSourceWriter) getOrCreate(sourcePath string) (*outputStream, error) {
	if s, ok := p.streams[sourcePath]; ok {
		return s, nil
	}
	base := filepath.Base(sourcePath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	fname := sanitizeFileName(stem) + "_已提取_" + p.ts + ".xlsx"
	outPath := filepath.Join(p.outDir, fname)
	s, err := openOutput(outPath, p.sheet)
	if err != nil {
		return nil, err
	}
	if err := s.applyColumnWidthsIfNeeded(p.schema.UnifiedColumnWidths); err != nil {
		_ = s.close()
		return nil, err
	}
	if err := s.writeHeader(p.schema.Columns, "命中关键词"); err != nil {
		_ = s.close()
		return nil, err
	}
	p.streams[sourcePath] = s
	return s, nil
}

func (p *perSourceWriter) EmitRow(row MatchedRow, fs *FileSchema) error {
	if p.schema == nil {
		return core.New("WRITER_NOT_BEGAN", "调用 Begin 之前就 EmitRow")
	}
	s, err := p.getOrCreate(row.SourceFile)
	if err != nil {
		return err
	}
	values := make([]any, 0, len(row.Values)+1)
	values = append(values, row.Values...)
	values = append(values, row.MatchedKW)

	dstRow, err := s.writeRow(values, row.SourceRow, row.RowHeight)
	if err != nil {
		return err
	}
	n, err := s.migratePictures(row.Pictures, fs, dstRow, len(p.schema.Columns))
	p.imgCount += n
	return err
}

func (p *perSourceWriter) Finalize() ([]string, error) {
	paths := make([]string, 0, len(p.streams))
	for _, s := range p.streams {
		if err := s.save(); err != nil {
			return paths, err
		}
		paths = append(paths, s.path)
	}
	return paths, nil
}

func (p *perSourceWriter) Close() error {
	for _, s := range p.streams {
		_ = s.close()
	}
	return nil
}

func (p *perSourceWriter) ImagesMigrated() int { return p.imgCount }
