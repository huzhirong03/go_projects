// Command verify-pics 打印一个 xlsx 所有 Sheet 里图片的锚点单元格。
// 用来肉眼验证"原汁原味"路径后图片是否跟着行被 RemoveRow 自动移到正确位置。
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
		fmt.Fprintln(os.Stderr, "用法: verify-pics -file <output.xlsx>")
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
		rows, err := f.GetRows(sheet)
		if err != nil {
			fmt.Println("读 rows 失败:", err)
			continue
		}
		total := 0
		// 遍历每个 cell（只扫前 20 列 * 实际行数）查图片
		for r := 1; r <= len(rows); r++ {
			for c := 1; c <= 20; c++ {
				cell, _ := excelize.CoordinatesToCellName(c, r)
				pics, err := f.GetPictures(sheet, cell)
				if err != nil || len(pics) == 0 {
					continue
				}
				for _, pic := range pics {
					total++
					fmt.Printf("  图片 #%d 锚点=%s 扩展名=%s 字节数=%d\n",
						total, cell, pic.Extension, len(pic.File))
				}
			}
		}
		fmt.Printf("[汇总] %s: %d 张图片\n\n", sheet, total)
	}
}
