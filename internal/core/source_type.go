package core

import (
	"path/filepath"
	"strings"
)

// SourceKind 区分输入源文件类型。CSV 与 XLSX 走两条互不干扰的读取路径，
// 所有需要按源类型分发的代码（scanner / extractor / splitter）都通过此枚举判断。
type SourceKind int

const (
	// SourceXLSX 包含 .xlsx / .xlsm 等 OOXML 工作簿。
	SourceXLSX SourceKind = iota
	// SourceCSV 纯文本 CSV，无 Sheet/样式/图片，输出退化为纯数据 xlsx。
	SourceCSV
	// SourceUnsupported 无法识别的扩展名（如 .xls / .ods / .numbers）。
	SourceUnsupported
)

// DetectSourceKind 仅按扩展名判断。读不到文件不在此层处理。
func DetectSourceKind(path string) SourceKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".xlsm":
		return SourceXLSX
	case ".csv":
		return SourceCSV
	default:
		return SourceUnsupported
	}
}

// IsSupported 是否在白名单内（用于扫描器过滤）。
func IsSupported(path string) bool {
	return DetectSourceKind(path) != SourceUnsupported
}
