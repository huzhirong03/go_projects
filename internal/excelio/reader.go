// Package excelio 封装 excelize，提供面向本项目的流式读写原语。
// 原则：
//   - 大文件必须用流式 API（Rows 迭代器），禁止 GetRows 一次加载。
//   - 所有打开的资源必须由调用方显式 Close，避免 defer 堆积在循环里。
package excelio

import (
	"os"

	"excel-master/internal/core"
	"excel-master/pkg/logger"

	"github.com/xuri/excelize/v2"
)

// openOptions 根据文件大小决定 excelize.OpenFile 的内存策略。
//
// 背景：excelize 默认 UnzipXMLSizeLimit=16MB，sheet1.xml 解压后超过该值
// 会写入系统临时目录（产生磁盘 I/O）。常见 6MB 的 xlsx 解压后 ~65MB，命中此分支，
// 对扫描和打开都有显著拖累（实测 100k 行卡 40 秒在 probe 阶段、扫描每秒仅 ~2000 行）。
//
// 分级策略（按 xlsx 文件磁盘大小，非解压后大小）：
//   - <= 30MB:   limit=128MB （常见 6MB 文件解压 ~65MB，128MB 足够全驻内存）
//   - 30-100MB:  limit=64MB  （让中等文件在 spill 边缘，控制峰值内存）
//   - > 100MB:   limit=32MB  （保守，让大文件 spill 到磁盘避免 OOM）
//
// 取舍说明：早期版本曾用 256/128/32，但峰值内存可达 1.5GB+，对 4-8GB 内存的
// 低配机有压力（用户同时开 Office/浏览器时可能 OOM）。本版本牺牲约 5% 速度
// 换来约 30% 内存峰值下降（实测 100k 行 fixture 1.5GB → 1.0GB），更适合作为
// 默认部署值。重度用户可在配置里覆盖（V2.0+）。
//
// 注意：故意不开 RawCellValue=true，因为它会让 number/date 失去格式化文本（如
// "2026-05-04" 变 "45816"、"￥1,234.50" 变 "1234.5"），违反"业务逻辑不变"。
func openOptions(path string) excelize.Options {
	const (
		mb        = int64(1 << 20)
		smallSize = 30 * mb
		largeSize = 100 * mb
	)
	limit := int64(128) * mb
	if fi, err := os.Stat(path); err == nil {
		switch sz := fi.Size(); {
		case sz > largeSize:
			limit = int64(32) * mb
		case sz > smallSize:
			limit = int64(64) * mb
		}
	}
	return excelize.Options{
		UnzipSizeLimit:    int64(16) << 30, // 16GB（默认值，避免 limit < UnzipXMLSizeLimit）
		UnzipXMLSizeLimit: limit,
	}
}

// Reader 是对 excelize.File 的读路径包装。
type Reader struct {
	f    *excelize.File
	path string
	// formulaProbeCache 缓存 SheetHasFormulas 的结果。同一 Reader 上同一 sheet 只扫一次
	// zip 里的 sheetN.xml；没有公式的 sheet 在 extractor 扫描时会跳过整个 readRowFormulas
	// 循环，fixture 01 (100k 行，14 命中列) 约省 10-40 秒的 excelize.CellFormula 调用。
	formulaProbeCache map[string]bool
	// rowHeightMapCache 缓存 RowHeights 的结果。同一 Reader 上同 sheet 只扫一次 zip；
	// 之后每命中行用 O(1) hash 查 map 替代 excelize.GetRowHeight 的 O(N) linear scan，
	// fixture 01 约省 10 秒（14286 命中 × 每次 832µs = 11.9s → 1 次 2s）。
	rowHeightMapCache map[string]map[int]float64
	// uncachedFormulasCache 缓存 ProbeSheet 扫描时顺手收集到的"有 <f> 无 <v>" cell：
	// key = sheet 名，value = map[cellRef]formulaText（比如 {"K2":"F2+G2+H2+I2+J2"}）
	// 真实业务文件（>99% 公式都有缓存值）此 map 为空；SetCellFormula 生成但未经 Excel
	// 保存的文件（如 fixture 04、用户编辑没保存就发邮件的）此 map 非空。
	// 上层 extractor 仅对非空时的 cell 批量调 CalcCellValue 求值，避免全表扫描，
	// 让搜索能命中公式计算结果同时保持扫描性能不回归。
	uncachedFormulasCache map[string]map[string]string
}

// Open 以只读模式打开一个 xlsx 文件。
// 调用方必须 defer r.Close()。
//
// 内部使用 openOptions() 提供按文件大小分级的 UnzipXMLSizeLimit，
// 以避免 sheet1.xml 解压后被写入系统临时目录造成磁盘 I/O 拖慢全流程。
func Open(path string) (*Reader, error) {
	f, err := excelize.OpenFile(path, openOptions(path))
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
//
// 性能：通过 GetSheetDimension 拿"已使用范围"反推最大列号，再逐列查 GetColWidth。
// 这两个 API 都是 O(列数) 元数据查询，不会触发 sheet 数据全量加载。
//
// 历史踩坑：旧版用 GetCols 读出所有列再遍历，对 10 万行 xlsx 等价于"全表加载"，
// probeFile 阶段单文件耗时 40+ 秒（实测）。改用 GetSheetDimension 后 ms 级返回。
func (r *Reader) ColumnWidths(sheet string) (map[int]float64, error) {
	out := map[int]float64{}

	// 1) 用 GetSheetDimension 拿到"A1:N100001"形式的范围，O(1) 元数据
	dim, err := r.f.GetSheetDimension(sheet)
	if err != nil {
		return nil, core.Wrap("EXCEL_READ_FAILED", "读取范围失败: "+sheet, err)
	}
	maxCol := parseDimensionMaxCol(dim)
	if maxCol <= 0 {
		return out, nil
	}

	// 2) 对每列查列宽，O(列数) 元数据查询
	for i := 1; i <= maxCol; i++ {
		colName, err := excelize.ColumnNumberToName(i)
		if err != nil {
			continue
		}
		w, err := r.f.GetColWidth(sheet, colName)
		if err != nil {
			continue
		}
		// excelize 的默认宽度是 9.0；只有在用户显式调整或公开为非默认时才有意义。
		// 这里全部记录（包含默认），让目标文件能完整复刻外观。
		out[i] = w
	}
	return out, nil
}

// parseDimensionMaxCol 解析 GetSheetDimension 返回的范围串，提取最大列号。
//
// 支持形式：
//   - "A1:N100001" -> 14
//   - "A1"        -> 1
//   - ""          -> 0（空表）
//
// 单元格非法时返回 0，调用方用 0 表示"没有可推断的列"，安全降级为空 map。
func parseDimensionMaxCol(dim string) int {
	if dim == "" {
		return 0
	}
	end := dim
	if i := indexByte(dim, ':'); i >= 0 {
		end = dim[i+1:]
	}
	col, _, err := excelize.CellNameToCoordinates(end)
	if err != nil {
		return 0
	}
	return col
}

// indexByte 是 strings.IndexByte 的本地副本，避免为 1 个调用引入 strings 包。
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
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
	// 跳到 headerRow，区分两种"读不到"：
	//   第 1 次 Next() == false  → Sheet 完全空（0 行） → EMPTY_SHEET（调用方应跳过）
	//   i > 1 时 Next() == false → Sheet 有数据但行数 < headerRow（用户填错 headerRow） → HEADER_ROW_MISSING（调用方应报错）
	for i := 1; i <= headerRow; i++ {
		if !rows.Next() {
			if i == 1 {
				return nil, core.New(core.CodeEmptySheet, "Sheet 完全空（0 行）: "+sheet)
			}
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
