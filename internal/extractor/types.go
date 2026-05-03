package extractor

import "excel-master/internal/excelio"

// MatchedRow 是一次命中后向 OutputWriter 投递的单元。
//
// V1.1 起，Values 元素可以是 excelize.Cell（带 Formula）；
// outputStream.writeRow 会按 SourceRow → dstRow 做"同行公式偏移"，
// 不安全的公式自动回退为写 Cell.Value。
type MatchedRow struct {
	SourceFile string                 // 源文件路径
	SourceRow  int                    // 源文件中行号（1-based）
	MatchedKW  string                 // 命中的关键词原文
	Values     []any                  // 按 UnifiedSchema 对齐后的单元格值（可含 excelize.Cell）
	Pictures   []excelio.CellPictures // 源文件该行的图片（需要时）
	RowHeight  float64                // 源行的自定义行高，0 表示用默认值
}

// OutputWriter 是 3 种输出策略的统一接口。
type OutputWriter interface {
	// Begin 在知道统一 schema 后初始化，适合写表头。
	Begin(schema *UnifiedSchema) error
	// EmitRow 投递一条命中行。实现可自行决定分流到哪个输出文件。
	EmitRow(row MatchedRow, fs *FileSchema) error
	// Finalize 落盘所有输出文件，返回文件路径列表。
	Finalize() ([]string, error)
	// Close 释放底层资源。多次调用需安全。
	Close() error
}

// Result 是一次 Extract 运行的结果汇总。
type Result struct {
	FilesScanned   int
	FilesMatched   int
	RowsMatched    int
	ImagesMigrated int
	OutputFiles    []string
}
