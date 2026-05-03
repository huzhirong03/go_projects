package excelio

import (
	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"
)

// Writer 是对 excelize 输出流的包装。
// 使用模式：
//
//	w, _ := excelio.NewWriter(outPath)
//	defer w.Close()
//	sw, _ := w.StreamFor("Sheet1")
//	sw.WriteRow(rowIdx, []any{"A", "B"})
//	sw.Flush()
//	w.SaveAs(outPath)
type Writer struct {
	f       *excelize.File
	streams map[string]*StreamSheet
}

// NewWriter 创建一个空的新文件（内存中）。
// Close 释放资源；保存通过 Save() 完成。
func NewWriter() *Writer {
	f := excelize.NewFile()
	// excelize 默认会创建名为 "Sheet1" 的 Sheet，我们后续按需重命名或新增。
	return &Writer{f: f, streams: map[string]*StreamSheet{}}
}

// File 暴露底层文件对象供同包的 style.go / image.go 使用。
func (w *Writer) File() *excelize.File { return w.f }

// StreamFor 返回指定 Sheet 的流式写入器。Sheet 不存在会自动创建。
// 对同一个 Sheet 多次调用会返回同一个 StreamSheet。
func (w *Writer) StreamFor(sheet string) (*StreamSheet, error) {
	if s, ok := w.streams[sheet]; ok {
		return s, nil
	}
	// 若 Sheet 不存在则新建。默认 "Sheet1" 已存在，但名字可能不是调用方想要的。
	if _, err := w.f.GetSheetIndex(sheet); err != nil || !w.hasSheet(sheet) {
		if _, err := w.f.NewSheet(sheet); err != nil {
			return nil, core.Wrap("EXCEL_WRITE_FAILED", "创建 Sheet 失败: "+sheet, err)
		}
	}
	sw, err := w.f.NewStreamWriter(sheet)
	if err != nil {
		return nil, core.Wrap("EXCEL_WRITE_FAILED", "创建 StreamWriter 失败: "+sheet, err)
	}
	s := &StreamSheet{sw: sw, sheet: sheet}
	w.streams[sheet] = s
	return s, nil
}

func (w *Writer) hasSheet(name string) bool {
	for _, n := range w.f.GetSheetList() {
		if n == name {
			return true
		}
	}
	return false
}

// RemoveDefaultSheet 删除 excelize 默认创建的 Sheet1（若存在且不在使用中）。
// 建议在添加了用户自定义 Sheet 之后调用。
func (w *Writer) RemoveDefaultSheet() error {
	const def = "Sheet1"
	if _, inUse := w.streams[def]; inUse {
		return nil
	}
	if !w.hasSheet(def) {
		return nil
	}
	if len(w.f.GetSheetList()) <= 1 {
		return nil // 不能删光
	}
	if err := w.f.DeleteSheet(def); err != nil {
		return core.Wrap("EXCEL_WRITE_FAILED", "删除默认 Sheet 失败", err)
	}
	return nil
}

// Save 写盘到目标路径。调用前会先 Flush 所有 StreamSheet。
func (w *Writer) Save(path string) error {
	for name, s := range w.streams {
		if err := s.Flush(); err != nil {
			return core.Wrap("EXCEL_WRITE_FAILED", "Flush 失败: "+name, err)
		}
	}
	if err := w.f.SaveAs(path); err != nil {
		return core.Wrap("EXCEL_WRITE_FAILED", "保存文件失败: "+path, err)
	}
	return nil
}

// Close 释放内存。必须在 Save 之后或放弃场景调用。
func (w *Writer) Close() error {
	if w == nil || w.f == nil {
		return nil
	}
	return w.f.Close()
}

// StreamSheet 是单个 Sheet 的流式写入器。
// 注意：excelize 的 StreamWriter 要求按行号升序写入，否则会静默失败。
type StreamSheet struct {
	sw    *excelize.StreamWriter
	sheet string
}

// WriteRow 写入一行。rowIdx 为 1-based。
// values 的元素类型应为 excelize 可接受的基本类型或 excelize.Cell。
func (s *StreamSheet) WriteRow(rowIdx int, values []any) error {
	cell, err := excelize.CoordinatesToCellName(1, rowIdx)
	if err != nil {
		return core.Wrap("EXCEL_WRITE_FAILED", "无效的行号", err)
	}
	if err := s.sw.SetRow(cell, values); err != nil {
		return core.Wrap("EXCEL_WRITE_FAILED", "写入行失败", err)
	}
	return nil
}

// WriteRowWithHeight 与 WriteRow 等价，但额外设置该行的行高（Excel 单位）。
// height <= 0 时退化为不设高度。
func (s *StreamSheet) WriteRowWithHeight(rowIdx int, values []any, height float64) error {
	cell, err := excelize.CoordinatesToCellName(1, rowIdx)
	if err != nil {
		return core.Wrap("EXCEL_WRITE_FAILED", "无效的行号", err)
	}
	if height > 0 {
		if err := s.sw.SetRow(cell, values, excelize.RowOpts{Height: height}); err != nil {
			return core.Wrap("EXCEL_WRITE_FAILED", "写入行失败", err)
		}
		return nil
	}
	if err := s.sw.SetRow(cell, values); err != nil {
		return core.Wrap("EXCEL_WRITE_FAILED", "写入行失败", err)
	}
	return nil
}

// SetColumnWidths 批量设置列宽。widths 用 1-based 列号做 key，单位是 Excel 列宽字符数。
// 必须在第一行 WriteRow 之前调用（StreamWriter 的限制）。
func (s *StreamSheet) SetColumnWidths(widths map[int]float64) error {
	if len(widths) == 0 {
		return nil
	}
	for col, w := range widths {
		if err := s.sw.SetColWidth(col, col, w); err != nil {
			return core.Wrap("EXCEL_WRITE_FAILED", "设置列宽失败", err)
		}
	}
	return nil
}

// MergeCell 在流式写入场景下合并单元格。
// 必须在对应行写入前后立即调用（excelize 限制）。
func (s *StreamSheet) MergeCell(startCell, endCell string) error {
	if err := s.sw.MergeCell(startCell, endCell); err != nil {
		return core.Wrap("EXCEL_WRITE_FAILED", "合并单元格失败", err)
	}
	return nil
}

// Flush 完成流式写入，将缓存落到 excelize.File。
func (s *StreamSheet) Flush() error {
	if s == nil || s.sw == nil {
		return nil
	}
	return s.sw.Flush()
}

// SheetName 返回当前 Sheet 名。
func (s *StreamSheet) SheetName() string { return s.sheet }
