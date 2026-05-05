package excelio

// formula_eval.go：公式回退求值。
//
// 背景：xlsx 里的公式 cell 一般长这样：
//   <c r="K2" t="str"><f>F2+G2+H2+I2+J2</f><v>300</v></c>
//                                            ^^^^^^^^^^^
//                                            cached value
// Excel/WPS 保存时会把公式的计算结果写到 <v> 标签里。但有些工具（比如 excelize 的
// SetCellFormula）只写 <f> 不写 <v>。这种文件用 xlsxreader / excelize.Rows 读出来
// cell.Value 就是空字符串 —— 业务层搜"300"自然搜不到。
//
// 此模块提供一个回退路径：扫描 sheet，对所有"无 <v> 缓存但有 <f> 公式"的 cell，
// 调用 excelize 的 CalcCellValue 现场求值，返回 ref → value 的 map。
// 调用方在扫描每行时，对空 cell 查这个 map 兜底，让搜索能命中公式计算结果。
//
// 性能：CalcCellValue 单次约 0.1-2ms，fixture 04 的 3000 行 × 2 公式列共 6000 次
// 调用大约 1-3 秒。只在 hasFormulas=true 的 sheet 跑一次，扫描完缓存到内存。

import (
	"github.com/xuri/excelize/v2"

	"excel-master/internal/core"
)

// EvaluateFormulas 扫描整个 sheet，找出所有"cell.Value 为空但有公式"的单元格，
// 调用 CalcCellValue 求值，返回 ref（如 "K2"）→ value 的 map。
//
// 已经有 cache value 的 cell 完全跳过 —— 真实业务文件几乎都有缓存，所以本函数
// 在多数情况下做的是 fast-skip。
//
// 返回 map 可能为空（无公式 / 无空缓存），不返回 nil。
// 整体失败（无法 Iterate）才返回 error；单 cell 求值失败只是跳过，不影响其他 cell。
func (r *Reader) EvaluateFormulas(sheet string) (map[string]string, error) {
	out := make(map[string]string)
	if r == nil || r.f == nil {
		return out, nil
	}

	rows, err := r.f.Rows(sheet)
	if err != nil {
		return nil, core.Wrap("EXCEL_READ_FAILED", "公式预求值打开行迭代失败: "+sheet, err)
	}
	defer func() { _ = rows.Close() }()

	rowIdx := 0
	for rows.Next() {
		rowIdx++
		cells, err := rows.Columns()
		if err != nil {
			// 单行读失败不影响整 sheet，继续
			continue
		}
		for c, val := range cells {
			if val != "" {
				continue // 已有缓存，跳过
			}
			ref, err := excelize.CoordinatesToCellName(c+1, rowIdx)
			if err != nil {
				continue
			}
			formula, err := r.f.GetCellFormula(sheet, ref)
			if err != nil || formula == "" {
				continue // 不是公式，是真的空值
			}
			calc, err := r.f.CalcCellValue(sheet, ref)
			if err != nil || calc == "" {
				continue // 求值失败（如跨 sheet 不支持）就保留空，不污染 map
			}
			out[ref] = calc
		}
	}
	return out, nil
}

// FillRowCellsWithFormulaValues 按公式预求值结果填充某行 cells 数组的空 cell。
//
// cells: 当前行的单元格切片（按列索引 0-based）
// rowNum: 1-based 行号
// formulaValues: EvaluateFormulas 返回的 ref → value map
//
// 行为：
//   - 对 cells[i] == "" 且 formulaValues 有对应 ref 的位置回填
//   - 如果 formulaValues 包含的列索引超出当前 cells 长度（xlsxreader 会**跳过**
//     无 <v> 缓存的公式 cell，导致 K/L 列在 cells 数组里直接不存在），自动扩展
//     cells 切片到能容纳所有公式列的长度，缺位置用空字符串填充
//   - 已有非空值的 cell 保持原值不变
//
// 返回值：可能与入参 cells 同一切片（无需扩展时）或新分配切片（需扩展时）。
// 调用方必须用返回值更新自己的 cells 引用。
//
// 性能：遍历 formulaValues 一次，O(len(formulaValues))。对于真实业务文件
// （>99% 公式都有缓存）此 map 为空，函数直接返回原切片。
func FillRowCellsWithFormulaValues(cells []string, rowNum int, formulaValues map[string]string) []string {
	if len(formulaValues) == 0 {
		return cells
	}
	// 第一遍：找出本行所有公式的最大列索引，决定是否需要扩展 cells
	type fillItem struct {
		col int // 0-based
		val string
	}
	var fills []fillItem
	maxCol0 := len(cells) - 1
	for ref, val := range formulaValues {
		col, row, err := excelize.CellNameToCoordinates(ref)
		if err != nil || row != rowNum {
			continue
		}
		fills = append(fills, fillItem{col: col - 1, val: val})
		if col-1 > maxCol0 {
			maxCol0 = col - 1
		}
	}
	if len(fills) == 0 {
		return cells
	}
	// 必要时扩展 cells
	if maxCol0 >= len(cells) {
		newCells := make([]string, maxCol0+1)
		copy(newCells, cells)
		cells = newCells
	}
	// 回填（已有非空值的 cell 保持不变）
	for _, f := range fills {
		if cells[f.col] == "" {
			cells[f.col] = f.val
		}
	}
	return cells
}
