// Command zipsurgery-cli 临时工具：用 CloneAndExtractZip 对单文件做一次原汁原味提取，
// 方便用 Excel 亲自验证"样式是否真的不丢了"。
//
// 用法：
//
//	zipsurgery-cli -src <src.xlsx> -dst <dst.xlsx> -sheet <sheet名> -rows 1,3,5,7
//
// 其中 rows 列表是要保留的 1-based 行号（表头要一起列进去）。
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"excel-master/internal/excelio"
)

func main() {
	src := flag.String("src", "", "源 xlsx")
	dst := flag.String("dst", "", "目标 xlsx")
	sheet := flag.String("sheet", "", "要保留的 Sheet 名")
	rows := flag.String("rows", "", "要保留的 1-based 行号（逗号分隔），例: 1,3,5")
	flag.Parse()
	if *src == "" || *dst == "" || *sheet == "" || *rows == "" {
		flag.Usage()
		os.Exit(2)
	}
	keep := []int{}
	for _, s := range strings.Split(*rows, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "行号解析失败:", s)
			os.Exit(2)
		}
		keep = append(keep, n)
	}
	if err := excelio.CloneAndExtractZip(*src, *dst, *sheet, keep); err != nil {
		fmt.Fprintln(os.Stderr, "[失败]", err)
		os.Exit(1)
	}
	fmt.Println("[完成] 输出:", *dst, "  保留行:", keep)
}
