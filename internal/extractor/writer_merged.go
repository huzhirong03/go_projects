package extractor

import (
	"path/filepath"

	"excel-master/internal/core"
)

// mergedWriter：所有命中写到同一个文件，末尾追加"命中关键词""来源文件"两列。
type mergedWriter struct {
	outDir   string
	sheet    string
	schema   *UnifiedSchema
	stream   *outputStream
	imgCount int
}

func newMergedWriter(outDir, sheet string) *mergedWriter {
	return &mergedWriter{outDir: outDir, sheet: sheet}
}

func (m *mergedWriter) Begin(schema *UnifiedSchema) error {
	m.schema = schema
	outPath := filepath.Join(m.outDir, "搜索结果_"+timestamp()+".xlsx")
	s, err := openOutput(outPath, m.sheet)
	if err != nil {
		return err
	}
	if err := s.applyColumnWidthsIfNeeded(schema.UnifiedColumnWidths); err != nil {
		_ = s.close()
		return err
	}
	if err := s.writeHeader(schema.Columns, "命中关键词", "来源文件"); err != nil {
		_ = s.close()
		return err
	}
	m.stream = s
	return nil
}

func (m *mergedWriter) EmitRow(row MatchedRow, fs *FileSchema) error {
	if m.schema == nil || m.stream == nil {
		return core.New("WRITER_NOT_BEGAN", "调用 Begin 之前就 EmitRow")
	}
	values := make([]any, 0, len(row.Values)+2)
	values = append(values, row.Values...)
	values = append(values, row.MatchedKW, filepath.Base(row.SourceFile))

	dstRow, err := m.stream.writeRow(values, row.SourceRow, row.RowHeight)
	if err != nil {
		return err
	}
	n, err := m.stream.migratePictures(row.Pictures, fs, dstRow, len(m.schema.Columns))
	m.imgCount += n
	return err
}

func (m *mergedWriter) Finalize() ([]string, error) {
	if m.stream == nil {
		return nil, nil
	}
	if err := m.stream.save(); err != nil {
		return nil, err
	}
	return []string{m.stream.path}, nil
}

func (m *mergedWriter) Close() error {
	if m.stream != nil {
		_ = m.stream.close()
	}
	return nil
}

func (m *mergedWriter) ImagesMigrated() int { return m.imgCount }
