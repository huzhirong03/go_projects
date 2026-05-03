// Package splitter 实现单文件拆分的 3 种模式：按 Sheet / 按行数 / 按列值。
// 所有模式都保持"源文件只读"原则，输出生成独立的 .xlsx 文件。
package splitter

// Result 是一次拆分运行的汇总结果。
type Result struct {
	SourceFile     string
	Mode           string
	RowsScanned    int
	PartsCreated   int
	ImagesMigrated int
	OutputFiles    []string
}
