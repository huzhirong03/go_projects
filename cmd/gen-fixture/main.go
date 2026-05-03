// Command gen-fixture 生成一组带图片的 Excel 测试样本，
// 模拟外贸/电商"产品资料"场景，用于 wails dev 下验证批量提取 + 拆分。
//
// 用法：
//
//	gen-fixture.exe -out .\testdata_samples
//
// 产出：
//
//	testdata_samples/
//	├── 供应商A_美妆目录.xlsx   （5 种产品，表头[产品名,型号,价格,库存,产品图]）
//	├── 供应商B_护肤目录.xlsx   （4 种产品，表头[型号,产品名,价格,产品图] 顺序不同）
//	└── 供应商C_杂货目录.xlsx   （3 种产品，表头[产品名,价格,产品图] 缺"型号"列）
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

type product struct {
	Name  string
	SKU   string
	Price float64
	Stock int
	Color color.RGBA
}

func main() {
	out := flag.String("out", "testdata_samples", "输出目录")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		exitErr(err)
	}

	// 供应商A: 5 款美妆，完整列
	genFile(filepath.Join(*out, "供应商A_美妆目录.xlsx"),
		[]string{"产品名", "型号", "价格", "库存", "产品图"},
		[]product{
			{"哑光口红 A01", "LIP-A01", 99.00, 120, color.RGBA{220, 20, 60, 255}},
			{"丝绒口红 B02", "LIP-B02", 129.00, 80, color.RGBA{178, 34, 52, 255}},
			{"大地色眼影 E03", "EYE-E03", 189.00, 50, color.RGBA{139, 90, 43, 255}},
			{"粉底液 F04", "FDN-F04", 258.00, 30, color.RGBA{245, 222, 179, 255}},
			{"气垫粉饼 Q05", "CUS-Q05", 320.00, 20, color.RGBA{255, 228, 196, 255}},
		},
		[]int{0, 1, 2, 3, 4}, // 所有行都带图
	)

	// 供应商B: 4 款护肤，表头顺序不同（型号在前）
	genFile(filepath.Join(*out, "供应商B_护肤目录.xlsx"),
		[]string{"型号", "产品名", "价格", "产品图"},
		[]product{
			{"水润精华 S01", "SKN-S01", 398.00, 0, color.RGBA{135, 206, 235, 255}},
			{"保湿面霜 M02", "SKN-M02", 288.00, 0, color.RGBA{224, 255, 255, 255}},
			{"防晒霜 SPF50", "SKN-SPF50", 168.00, 0, color.RGBA{255, 250, 205, 255}},
			{"口红蜡 C04", "LIP-C04", 88.00, 0, color.RGBA{255, 105, 180, 255}},
		},
		[]int{0, 2, 3}, // 只有部分行带图
	)

	// 供应商C: 3 款杂货，缺"型号"列（表头只有 产品名/价格/产品图）
	genFileNoSKU(filepath.Join(*out, "供应商C_杂货目录.xlsx"),
		[]string{"产品名", "价格", "产品图"},
		[]product{
			{"保温水杯 H01", "", 59.00, 0, color.RGBA{30, 144, 255, 255}},
			{"文具盒 P02", "", 15.00, 0, color.RGBA{152, 251, 152, 255}},
			{"眼影收纳盒 G03", "", 75.00, 0, color.RGBA{221, 160, 221, 255}},
		},
		[]int{0, 2},
	)

	// 撒一个临时锁文件 + 一个 txt 用来验证扫描器过滤
	_ = os.WriteFile(filepath.Join(*out, "~$临时锁.xlsx"), []byte("lock"), 0o644)
	_ = os.WriteFile(filepath.Join(*out, "说明.txt"),
		[]byte("这是一个无关文件，批量提取不应处理它。\n"), 0o644)

	fmt.Println("✓ 测试样本已生成到:", *out)
	fmt.Println()
	fmt.Println("建议的验证动作：")
	fmt.Println("  1. 批量提取：关键词 \"口红, yy\" （yy=眼影拼音首字母），全列搜索")
	fmt.Println("     预期命中 6 行（A:2 口红 + 1 眼影 + B:1 口红 + C:1 眼影 = 5）")
	fmt.Println("     —— 实际: A 文件 \"哑光口红 A01/丝绒口红 B02/大地色眼影 E03\"")
	fmt.Println("            + B 文件 \"口红蜡 C04\" + C 文件 \"眼影收纳盒 G03\" = 5")
	fmt.Println("  2. 图片应该跟着行进入新文件（数量≥4）")
	fmt.Println("  3. 多文件表头不一致应能正确合并（C 缺型号列 → 输出此列为空）")
}

// genFile 生成一个带完整列（含型号）的 xlsx。
// picRows 是要加图的"产品行"索引（0-based，即第几款产品），图片放"产品图"列。
func genFile(path string, headers []string, products []product, picRows []int) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "产品表"
	idx, _ := f.NewSheet(sheet)
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	// 表头
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	// 找列位置（按名字定位）
	colIdx := map[string]int{}
	for i, h := range headers {
		colIdx[h] = i + 1
	}

	for ri, p := range products {
		row := ri + 2
		if c, ok := colIdx["产品名"]; ok {
			setStr(f, sheet, c, row, p.Name)
		}
		if c, ok := colIdx["型号"]; ok {
			setStr(f, sheet, c, row, p.SKU)
		}
		if c, ok := colIdx["价格"]; ok {
			setCell(f, sheet, c, row, p.Price)
		}
		if c, ok := colIdx["库存"]; ok {
			setCell(f, sheet, c, row, p.Stock)
		}
	}

	// 设一些列宽，让视觉更好
	_ = f.SetColWidth(sheet, "A", "A", 22)
	_ = f.SetColWidth(sheet, "E", "E", 14) // 图片列
	for _, r := range picRows {
		_ = f.SetRowHeight(sheet, r+2, 48)
	}

	// 加图片到产品图列
	picCol, hasPic := colIdx["产品图"]
	if hasPic {
		for _, r := range picRows {
			cell, _ := excelize.CoordinatesToCellName(picCol, r+2)
			addSolidPic(f, sheet, cell, products[r].Color)
		}
	}

	if err := f.SaveAs(path); err != nil {
		exitErr(err)
	}
	fmt.Printf("  · %s (%d 款产品, %d 张图)\n", filepath.Base(path), len(products), len(picRows))
}

// genFileNoSKU 专门处理"缺型号列"的供应商 C（逻辑和 genFile 基本一样，列映射容忍缺列）。
func genFileNoSKU(path string, headers []string, products []product, picRows []int) {
	genFile(path, headers, products, picRows)
}

func setStr(f *excelize.File, sheet string, col, row int, v string) {
	cell, _ := excelize.CoordinatesToCellName(col, row)
	_ = f.SetCellValue(sheet, cell, v)
}

func setCell(f *excelize.File, sheet string, col, row int, v any) {
	cell, _ := excelize.CoordinatesToCellName(col, row)
	_ = f.SetCellValue(sheet, cell, v)
}

// addSolidPic 生成 40x40 纯色 PNG 并插入到指定单元格，模拟"产品图"。
func addSolidPic(f *excelize.File, sheet, cell string, c color.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		exitErr(err)
	}
	pic := &excelize.Picture{
		Extension: ".png",
		File:      buf.Bytes(),
		Format:    &excelize.GraphicOptions{},
	}
	if err := f.AddPictureFromBytes(sheet, cell, pic); err != nil {
		exitErr(err)
	}
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, "[错误]", err)
	os.Exit(1)
}
