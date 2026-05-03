// Command verify-formulas 检查一个 xlsx 文件是否保留了公式 + 列宽 + 行高。
// 用于 V1.1 端到端验证，不属于 GUI 流程。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/xuri/excelize/v2"
)

func main() {
	path := flag.String("file", "", "要检查的 .xlsx 路径")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "用法: verify-formulas -file <output.xlsx>")
		os.Exit(2)
	}

	f, err := excelize.OpenFile(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开失败:", err)
		os.Exit(1)
	}
	defer f.Close()

	for _, sheet := range f.GetSheetList() {
		fmt.Printf("=== Sheet: %s ===\n", sheet)

		// 列宽
		fmt.Println("[列宽]")
		for col := 1; col <= 10; col++ {
			name, _ := excelize.ColumnNumberToName(col)
			w, err := f.GetColWidth(sheet, name)
			if err == nil {
				fmt.Printf("  %s: %.2f\n", name, w)
			}
		}

		// 行高 + 公式
		rows, err := f.GetRows(sheet)
		if err != nil {
			fmt.Println("读 rows 失败:", err)
			continue
		}
		formulaRows := 0
		customHeightRows := 0
		for ri := range rows {
			rowNum := ri + 1
			if h, _ := f.GetRowHeight(sheet, rowNum); h != 15 && h != 0 {
				customHeightRows++
				if customHeightRows <= 5 {
					fmt.Printf("[行高] row %d = %.2f\n", rowNum, h)
				}
			}
			for ci := range rows[ri] {
				cellName, _ := excelize.CoordinatesToCellName(ci+1, rowNum)
				formula, _ := f.GetCellFormula(sheet, cellName)
				if formula != "" {
					formulaRows++
					if formulaRows <= 8 {
						val, _ := f.GetCellValue(sheet, cellName)
						fmt.Printf("[公式] %s: %s  (缓存值=%q)\n", cellName, formula, val)
					}
				}
			}
		}
		fmt.Printf("[汇总] 行数=%d 自定义行高=%d 公式 cell=%d\n",
			len(rows), customHeightRows, formulaRows)
		fmt.Println()
	}
}
