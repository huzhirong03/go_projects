package excelio

// formula_eval.go：公式回退求值（方案 A+，v1.4.1）。
//
// 背景：
// xlsx 里的公式 cell 一般长这样：
//   <c r="K2" t="str"><f>F2+G2+H2+I2+J2</f><v>300</v></c>
//                                          ^^^^^^^^^^^
//                                          cached value
// Excel / WPS 保存时会把公式计算结果写到 <v>。但有些工具（比如 excelize 的
// SetCellFormula 未经 Excel 打开保存）只写 <f> 不写 <v>。这类 cell：
//   - xlsxreader（fast path）直接跳过整个 cell（cells 数组里不出现 K 列）
//   - excelize.Rows 给出空字符串 ""
// 结果：用户搜 "300" 一行都命中不了。
//
// 本模块与 ProbeSheet 配合提供**精准按需求值**，避免上一版 v1.4.0 全表扫描
// 的 20 秒性能灾难：
//   - ProbeSheet 在 zip 流式扫描时顺手收集"有 <f> 无 <v>" cell 的 (ref, formula)
//   - 真实业务文件（>99% 公式都有 <v>）该 map 为空 → 本模块完全不被调用 → 零开销
//   - fixture 04 等无缓存文件 → 只对具体的 ref 调 CalcCellValue，不扫全表
//
// 性能对比（fixture 04: 3000 行 × 2 公式列 = 6000 cell）：
//   - v1.4.0 全表扫：~20 秒（excelize.Rows 遍历 + 每 cell 查 CellFormula）
//   - v1.4.1 精准算：~2-3 秒（只对 6000 个已知 ref 调 CalcCellValue）
//   - 真实业务文件（全有缓存）：~0 ms（跳过调用）

import (
	"github.com/xuri/excelize/v2"

	"excel-master/internal/core"
)

// UncachedFormulas 返回 sheet 内所有"有 <f> 无 <v>" cell 的 ref → 公式文本映射。
//
// 数据来源：由 ProbeSheet 的 zip 流式扫描顺手收集，写入 r.uncachedFormulasCache。
// 如果调用方没先跑过 ProbeSheet，本方法内部会触发一次扫描（通过 ProbeSheet）。
//
// 返回值：
//   - 空 map：sheet 内所有公式 cell 都有 <v> 缓存，**不需要**任何回退求值（性能 fast path）
//   - 非空 map：这些 cell 需要外层调用 EvalFormulasAt 批量求值，结果喂给搜索层
//
// 失败时返回 (nil, err)；调用方通常选择按"空 map"保守处理（跟原 v1.3.1 行为一致）。
func (r *Reader) UncachedFormulas(sheet string) (map[string]string, error) {
	if r == nil {
		return nil, nil
	}
	if cached, ok := r.uncachedFormulasCache[sheet]; ok {
		return cached, nil
	}
	// 没扫过就触发 ProbeSheet（它会顺手填 uncachedFormulasCache）
	if _, _, err := r.ProbeSheet(sheet); err != nil {
		return nil, err
	}
	if cached, ok := r.uncachedFormulasCache[sheet]; ok {
		return cached, nil
	}
	return map[string]string{}, nil
}

// EvalFormulasAt 只对指定 ref 列表（通常是 UncachedFormulas 返回的 key 集合）
// 调用 excelize.CalcCellValue 求值。返回 ref → 计算后文本的 map。
//
// 相比上一版 EvaluateFormulas 的全表扫描，本函数有两点本质差异：
//  1. 不用 excelize.Rows 遍历整个 sheet（省 10 万行 × 14 列的 140 万次 cell 访问）
//  2. 不用 GetCellFormula 判断是否公式（ref 都是已经确定的公式 cell）
//
// 对于真实业务文件（UncachedFormulas 返回空 map），本函数完全不会被调用。
//
// 单 cell 求值失败不会影响其他 cell；最终 map 里缺的 ref 会让 Fill 阶段跳过回填，
// 退化到"搜不到那一行"的老行为（可接受：跨 sheet 复杂公式 excelize calc 引擎
// 有限，算不出就不算，不产生错误结果）。
func (r *Reader) EvalFormulasAt(sheet string, refs map[string]string) (map[string]string, error) {
	if r == nil || r.f == nil {
		return nil, core.New("EXCEL_READ_FAILED", "Reader 未初始化")
	}
	out := make(map[string]string, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	for ref := range refs {
		v, err := r.f.CalcCellValue(sheet, ref)
		if err != nil || v == "" {
			// 单 cell 求值失败就跳过；不污染 map，保持调用方"搜不到"的退化行为
			continue
		}
		out[ref] = v
	}
	return out, nil
}

// FillRowCellsWithFormulaValues 按公式求值结果填充某行 cells 数组的空 cell。
//
// cells: 当前行的单元格切片（按列索引 0-based）
// rowNum: 1-based 行号
// formulaValues: EvalFormulasAt 返回的 ref → value map
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
// 性能：遍历 formulaValues 一次，O(len(formulaValues))。仅在 map 非空时被调用，
// 真实业务文件扫描循环完全不会触发此函数。
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
