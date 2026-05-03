// Package excelio 封装 excelize，提供面向本项目的流式读写原语。
// 原则：
//   - 大文件必须用流式 API（Rows 迭代器），禁止 GetRows 一次加载。
//   - 所有打开的资源必须由调用方显式 Close，避免 defer 堆积在循环里。
package excelio

import (
	"excel-master/internal/core"
	"excel-master/pkg/logger"

	"github.com/xuri/excelize/v2"
)

// Reader 是对 excelize.File 的读路径包装。
type Reader struct {
	f    *excelize.File
	path string
}

// Open 以只读模式打开一个 xlsx 文件。
// 调用方必须 defer r.Close()。
func Open(path string) (*Reader, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, core.Wrap("EXCEL_OPEN_FAILED", "打开 Excel 失败: "+path, err)
	}
	return &Reader{f: f, path: path}, nil
}

// Close 释放底层句柄。
func (r *Reader) Close() error {
	if r == nil || r.f == nil {
		return nil
	}
	if err := r.f.Close(); err != nil {
		logger.Warn("关闭 Excel 失败: %v", err)
		return err
	}
	return nil
}

// Path 返回原始文件路径。
func (r *Reader) Path() string { return r.path }

// File 暴露底层 excelize.File，仅供同包 writer/image/style 使用。
func (r *Reader) File() *excelize.File { return r.f }

// SheetNames 返回所有 Sheet 名称。
func (r *Reader) SheetNames() []string {
	return r.f.GetSheetList()
}

// CellName 把 (col, row) 1-based 坐标转为 "A1" 形式。封装 excelize 的 CoordinatesToCellName，
// 让上层模块不必直接 import excelize。
func CellName(col, row int) (string, error) {
	return excelize.CoordinatesToCellName(col, row)
}

// CellFormula 返回指定单元格的公式表达式（不含前导 "="，excelize 行为）；
// 如果不是公式或单元格不存在，返回 ""。
//
// 注意：每次调用都查内部映射，对大文件密集使用应在外层缓存。
func (r *Reader) CellFormula(sheet, cell string) (string, error) {
	f, err := r.f.GetCellFormula(sheet, cell)
	if err != nil {
		return "", core.Wrap("EXCEL_READ_FAILED", "读取公式失败: "+sheet+"!"+cell, err)
	}
	return f, nil
}

// ColumnWidths 返回某 Sheet 已显式设置过宽度的列。
// 返回 map: 1-based 列号 -> 列宽（Excel 单位字符宽，常见 8.43~30）。
// 未显式设置过的列不会出现在 map 中。
func (r *Reader) ColumnWidths(sheet string) (map[int]float64, error) {
	out := map[int]float64{}
	cols, err := r.f.GetCols(sheet)
	if err != nil {
		return nil, core.Wrap("EXCEL_READ_FAILED", "读取列失败: "+sheet, err)
	}
	for i := range cols {
		colName, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			continue
		}
		w, err := r.f.GetColWidth(sheet, colName)
		if err != nil {
			continue
		}
		// excelize 的默认宽度是 9.0；只有在用户显式调整或公开为非默认时才有意义。
		// 这里全部记录（包含默认），让目标文件能完整复刻外观。
		out[i+1] = w
	}
	return out, nil
}

// RowHeight 返回指定行的行高（1-based）。
// 第二个返回值为 false 表示用户未显式设置过该行高度（应保持目标默认）。
func (r *Reader) RowHeight(sheet string, row int) (float64, bool, error) {
	h, err := r.f.GetRowHeight(sheet, row)
	if err != nil {
		return 0, false, core.Wrap("EXCEL_READ_FAILED", "读取行高失败", err)
	}
	// excelize 默认 15.0；视为"未自定义"——避免给目标文件设一堆默认行高。
	if h == 15.0 || h == 0 {
		return 0, false, nil
	}
	return h, true, nil
}

// Header 读取指定 Sheet 的表头行。
// headerRow 为 1-based 行号；若 headerRow <= 0，返回空切片表示"无表头"。
func (r *Reader) Header(sheet string, headerRow int) ([]string, error) {
	if headerRow <= 0 {
		return nil, nil
	}
	rows, err := r.f.Rows(sheet)
	if err != nil {
		return nil, core.Wrap("EXCEL_READ_FAILED", "读取 Sheet 失败: "+sheet, err)
	}
	defer rows.Close()
	// 跳到 headerRow
	for i := 1; i <= headerRow; i++ {
		if !rows.Next() {
			return nil, core.New("HEADER_ROW_MISSING", "表头行超出文件范围")
		}
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, core.Wrap("EXCEL_READ_FAILED", "读取表头失败", err)
	}
	return cols, nil
}

// RowIterator 是流式行迭代器。由 Iterate 创建，调用方必须 Close。
type RowIterator struct {
	rows    *excelize.Rows
	sheet   string
	rowNum  int // 当前行号（1-based，最后一次 Next 成功后指向该行）
	started bool
}

// Iterate 创建某个 Sheet 的流式迭代器。
func (r *Reader) Iterate(sheet string) (*RowIterator, error) {
	rows, err := r.f.Rows(sheet)
	if err != nil {
		return nil, core.Wrap("EXCEL_READ_FAILED", "打开行迭代器失败", err)
	}
	return &RowIterator{rows: rows, sheet: sheet}, nil
}

// Next 移到下一行。返回 false 表示迭代结束或出错，通过 Err() 判断。
func (it *RowIterator) Next() bool {
	ok := it.rows.Next()
	if ok {
		it.rowNum++
	}
	it.started = true
	return ok
}

// Columns 读取当前行所有单元格文本。
func (it *RowIterator) Columns() ([]string, error) {
	cols, err := it.rows.Columns()
	if err != nil {
		return nil, core.Wrap("EXCEL_READ_FAILED", "读取行数据失败", err)
	}
	return cols, nil
}

// RowNum 返回当前行号（1-based）。
func (it *RowIterator) RowNum() int { return it.rowNum }

// Err 返回迭代过程中的错误。
func (it *RowIterator) Err() error { return it.rows.Error() }

// Close 释放迭代器。
func (it *RowIterator) Close() error {
	if it == nil || it.rows == nil {
		return nil
	}
	return it.rows.Close()
}
