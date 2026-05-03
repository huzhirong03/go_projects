// Command gen-rich-fixture generates polished Excel samples with formulas and pictures.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	dataSheet    = "商品清单"
	summarySheet = "汇总看板"
	firstDataRow = 2
)

type product struct {
	Name     string
	SKU      string
	Category string
	Unit     string
	Cost     float64
	Price    float64
	Purchase int
	Stock    int
	Sales    int
	Rating   float64
	Launch   time.Time
	Note     string
	Color    color.RGBA
	Shape    int
}

type workbookSpec struct {
	Code       string
	FileName   string
	Supplier   string
	Theme      string
	Categories []string
	Products   []product
}

type rowStyleSet struct {
	Text     int
	Center   int
	Integer  int
	Currency int
	Percent  int
	Rating   int
	Date     int
	Note     int
}

type styleBook struct {
	Header       int
	Even         rowStyleSet
	Odd          rowStyleSet
	SummaryTitle int
	SummaryLabel int
	SummaryValue int
	SummaryMoney int
	SummaryPct   int
	SummaryWarn  int
}

func main() {
	out := flag.String("out", "testdata_samples", "output directory")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		exitErr(err)
	}

	for _, spec := range specs() {
		path := filepath.Join(*out, spec.FileName)
		if err := writeWorkbook(path, spec); err != nil {
			exitErr(err)
		}
		rows, formulas, pictures, err := verifyWorkbook(path, len(spec.Products))
		if err != nil {
			exitErr(err)
		}
		fmt.Printf("生成: %s | 数据 %d 行 | 公式 %d 个 | 图片 %d 张\n", path, rows, formulas, pictures)
	}
}

func writeWorkbook(path string, spec workbookSpec) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", dataSheet); err != nil {
		return err
	}
	if _, err := f.NewSheet(summarySheet); err != nil {
		return err
	}
	idx, err := f.GetSheetIndex(dataSheet)
	if err != nil {
		return err
	}
	f.SetActiveSheet(idx)

	styles, err := createStyles(f)
	if err != nil {
		return err
	}
	if err := buildDataSheet(f, spec, styles); err != nil {
		return err
	}
	if err := buildSummarySheet(f, spec, styles); err != nil {
		return err
	}
	if err := f.UpdateLinkedValue(); err != nil {
		return err
	}
	return f.SaveAs(path)
}

func buildDataSheet(f *excelize.File, spec workbookSpec, styles styleBook) error {
	lastRow := firstDataRow + len(spec.Products) - 1
	headers := []string{
		"产品图", "产品名称", "SKU", "品类", "单位", "供货价", "建议零售价", "采购量", "当前库存",
		"近7天销量", "预计销售额", "预计毛利", "毛利率", "安全库存", "补货建议", "评级", "上架日期", "备注",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(dataSheet, cell, h); err != nil {
			return err
		}
	}
	if err := f.SetCellStyle(dataSheet, "A1", "R1", styles.Header); err != nil {
		return err
	}

	widths := map[string]float64{
		"A": 15, "B": 22, "C": 15, "D": 13, "E": 8, "F": 11, "G": 12, "H": 10, "I": 10,
		"J": 11, "K": 13, "L": 13, "M": 10, "N": 10, "O": 11, "P": 8, "Q": 12, "R": 24,
	}
	for col, width := range widths {
		if err := f.SetColWidth(dataSheet, col, col, width); err != nil {
			return err
		}
	}
	if err := f.SetRowHeight(dataSheet, 1, 28); err != nil {
		return err
	}
	if err := f.SetSheetView(dataSheet, 0, &excelize.ViewOptions{ShowGridLines: boolPtr(false)}); err != nil {
		return err
	}
	if err := f.SetPanes(dataSheet, &excelize.Panes{
		Freeze:      true,
		XSplit:      2,
		YSplit:      1,
		TopLeftCell: "C2",
		ActivePane:  "bottomRight",
		Selection:   []excelize.Selection{{SQRef: "C2", ActiveCell: "C2", Pane: "bottomRight"}},
	}); err != nil {
		return err
	}

	for i, p := range spec.Products {
		row := firstDataRow + i
		set := styles.Even
		if row%2 == 1 {
			set = styles.Odd
		}
		if err := f.SetRowHeight(dataSheet, row, 58); err != nil {
			return err
		}
		values := map[string]any{
			"B": p.Name, "C": p.SKU, "D": p.Category, "E": p.Unit, "F": p.Cost, "G": p.Price,
			"H": p.Purchase, "I": p.Stock, "J": p.Sales, "P": p.Rating, "Q": p.Launch, "R": p.Note,
		}
		for col, value := range values {
			if err := f.SetCellValue(dataSheet, fmt.Sprintf("%s%d", col, row), value); err != nil {
				return err
			}
		}
		formulas := map[string]string{
			"K": fmt.Sprintf("=G%d*J%d", row, row),
			"L": fmt.Sprintf("=(G%d-F%d)*J%d", row, row, row),
			"M": fmt.Sprintf("=IF(K%d=0,0,L%d/K%d)", row, row, row),
			"N": fmt.Sprintf("=ROUNDUP(J%d*1.8,0)", row),
			"O": fmt.Sprintf("=IF(I%d<N%d,\"需要补货\",\"正常\")", row, row),
		}
		for col, formula := range formulas {
			if err := f.SetCellFormula(dataSheet, fmt.Sprintf("%s%d", col, row), formula); err != nil {
				return err
			}
		}

		if err := styleProductRow(f, row, set); err != nil {
			return err
		}
		if err := addProductImage(f, row, p); err != nil {
			return err
		}
	}

	if err := f.AddTable(dataSheet, &excelize.Table{
		Range:             fmt.Sprintf("A1:R%d", lastRow),
		Name:              "tbl_" + spec.Code,
		StyleName:         "TableStyleMedium4",
		ShowFirstColumn:   true,
		ShowLastColumn:    true,
		ShowColumnStripes: false,
	}); err != nil {
		return err
	}
	if err := addConditionalFormats(f, lastRow); err != nil {
		return err
	}
	return nil
}

func styleProductRow(f *excelize.File, row int, set rowStyleSet) error {
	r := fmt.Sprint(row)
	ranges := map[string]int{
		"A" + r: set.Center, "B" + r: set.Text, "C" + r: set.Center, "D" + r: set.Center, "E" + r: set.Center,
		"F" + r: set.Currency, "G" + r: set.Currency, "H" + r: set.Integer, "I" + r: set.Integer,
		"J" + r: set.Integer, "K" + r: set.Currency, "L" + r: set.Currency, "M" + r: set.Percent,
		"N" + r: set.Integer, "O" + r: set.Center, "P" + r: set.Rating, "Q" + r: set.Date, "R" + r: set.Note,
	}
	for cell, style := range ranges {
		if err := f.SetCellStyle(dataSheet, cell, cell, style); err != nil {
			return err
		}
	}
	return nil
}

func buildSummarySheet(f *excelize.File, spec workbookSpec, styles styleBook) error {
	lastRow := firstDataRow + len(spec.Products) - 1
	if err := f.SetSheetView(summarySheet, 0, &excelize.ViewOptions{ShowGridLines: boolPtr(false)}); err != nil {
		return err
	}
	if err := f.SetColWidth(summarySheet, "A", "A", 18); err != nil {
		return err
	}
	if err := f.SetColWidth(summarySheet, "B", "D", 16); err != nil {
		return err
	}
	if err := f.MergeCell(summarySheet, "A1", "D1"); err != nil {
		return err
	}
	if err := f.SetCellValue(summarySheet, "A1", spec.Supplier+" · "+spec.Theme+" 汇总看板"); err != nil {
		return err
	}
	if err := f.SetCellStyle(summarySheet, "A1", "D1", styles.SummaryTitle); err != nil {
		return err
	}
	kpis := []struct {
		Label   string
		Formula string
		Style   int
	}{
		{"商品数量", fmt.Sprintf("=COUNTA(商品清单!B2:B%d)", lastRow), styles.SummaryValue},
		{"总销售额", fmt.Sprintf("=SUM(商品清单!K2:K%d)", lastRow), styles.SummaryMoney},
		{"总毛利", fmt.Sprintf("=SUM(商品清单!L2:L%d)", lastRow), styles.SummaryMoney},
		{"平均毛利率", fmt.Sprintf("=AVERAGE(商品清单!M2:M%d)", lastRow), styles.SummaryPct},
		{"需补货 SKU", fmt.Sprintf("=COUNTIF(商品清单!O2:O%d,\"需要补货\")", lastRow), styles.SummaryWarn},
		{"平均评级", fmt.Sprintf("=AVERAGE(商品清单!P2:P%d)", lastRow), styles.SummaryValue},
	}
	for i, kpi := range kpis {
		row := i + 3
		if err := f.SetCellValue(summarySheet, fmt.Sprintf("A%d", row), kpi.Label); err != nil {
			return err
		}
		if err := f.SetCellFormula(summarySheet, fmt.Sprintf("B%d", row), kpi.Formula); err != nil {
			return err
		}
		if err := f.SetCellStyle(summarySheet, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), styles.SummaryLabel); err != nil {
			return err
		}
		if err := f.SetCellStyle(summarySheet, fmt.Sprintf("B%d", row), fmt.Sprintf("B%d", row), kpi.Style); err != nil {
			return err
		}
	}

	if err := f.SetCellValue(summarySheet, "A11", "品类"); err != nil {
		return err
	}
	if err := f.SetCellValue(summarySheet, "B11", "销售额"); err != nil {
		return err
	}
	if err := f.SetCellValue(summarySheet, "C11", "毛利"); err != nil {
		return err
	}
	if err := f.SetCellValue(summarySheet, "D11", "平均评级"); err != nil {
		return err
	}
	if err := f.SetCellStyle(summarySheet, "A11", "D11", styles.Header); err != nil {
		return err
	}
	for i, category := range spec.Categories {
		row := i + 12
		if err := f.SetCellValue(summarySheet, fmt.Sprintf("A%d", row), category); err != nil {
			return err
		}
		if err := f.SetCellFormula(summarySheet, fmt.Sprintf("B%d", row), fmt.Sprintf("=SUMIF(商品清单!D2:D%d,A%d,商品清单!K2:K%d)", lastRow, row, lastRow)); err != nil {
			return err
		}
		if err := f.SetCellFormula(summarySheet, fmt.Sprintf("C%d", row), fmt.Sprintf("=SUMIF(商品清单!D2:D%d,A%d,商品清单!L2:L%d)", lastRow, row, lastRow)); err != nil {
			return err
		}
		if err := f.SetCellFormula(summarySheet, fmt.Sprintf("D%d", row), fmt.Sprintf("=AVERAGEIF(商品清单!D2:D%d,A%d,商品清单!P2:P%d)", lastRow, row, lastRow)); err != nil {
			return err
		}
		if err := f.SetCellStyle(summarySheet, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), styles.SummaryLabel); err != nil {
			return err
		}
		if err := f.SetCellStyle(summarySheet, fmt.Sprintf("B%d", row), fmt.Sprintf("C%d", row), styles.SummaryMoney); err != nil {
			return err
		}
		if err := f.SetCellStyle(summarySheet, fmt.Sprintf("D%d", row), fmt.Sprintf("D%d", row), styles.SummaryValue); err != nil {
			return err
		}
	}
	return nil
}

func addConditionalFormats(f *excelize.File, lastRow int) error {
	warn, err := f.NewConditionalStyle(&excelize.Style{
		Font: &excelize.Font{Color: "9A0511", Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F9D6D5"}},
	})
	if err != nil {
		return err
	}
	ok, err := f.NewConditionalStyle(&excelize.Style{
		Font: &excelize.Font{Color: "14532D", Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DDF7E8"}},
	})
	if err != nil {
		return err
	}
	if err := f.SetConditionalFormat(dataSheet, fmt.Sprintf("K2:K%d", lastRow), []excelize.ConditionalFormatOptions{{
		Type: "data_bar", Criteria: "=", MinType: "min", MaxType: "max", BarColor: "#5B8DEF", BarSolid: true,
	}}); err != nil {
		return err
	}
	if err := f.SetConditionalFormat(dataSheet, fmt.Sprintf("M2:M%d", lastRow), []excelize.ConditionalFormatOptions{{
		Type: "3_color_scale", Criteria: "=", MinType: "min", MidType: "percentile", MaxType: "max",
		MinColor: "#F8696B", MidColor: "#FFEB84", MaxColor: "#63BE7B",
	}}); err != nil {
		return err
	}
	return f.SetConditionalFormat(dataSheet, fmt.Sprintf("O2:O%d", lastRow), []excelize.ConditionalFormatOptions{
		{Type: "text", Criteria: "containing", Value: "需要补货", Format: &warn},
		{Type: "text", Criteria: "containing", Value: "正常", Format: &ok},
	})
}

func createStyles(f *excelize.File) (styleBook, error) {
	header, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 10.5},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F4E5F"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    thinBorder("B7C9D3"),
	})
	if err != nil {
		return styleBook{}, err
	}
	even, err := createRowStyles(f, "FFFFFF")
	if err != nil {
		return styleBook{}, err
	}
	odd, err := createRowStyles(f, "F7FBFC")
	if err != nil {
		return styleBook{}, err
	}
	title, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "12313F", Size: 16},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DCEEF4"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border:    thinBorder("B7C9D3"),
	})
	if err != nil {
		return styleBook{}, err
	}
	label, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "244653", Size: 10.5},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EEF5F7"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border:    thinBorder("D5E0E6"),
	})
	if err != nil {
		return styleBook{}, err
	}
	value, err := summaryValueStyle(f, "0.0")
	if err != nil {
		return styleBook{}, err
	}
	money, err := summaryValueStyle(f, "¥#,##0.00")
	if err != nil {
		return styleBook{}, err
	}
	pct, err := summaryValueStyle(f, "0.00%")
	if err != nil {
		return styleBook{}, err
	}
	warn, err := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Color: "9A3412", Size: 12},
		Fill:         excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFF4D6"}},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:       thinBorder("EBCB8B"),
		CustomNumFmt: strPtr("0"),
	})
	if err != nil {
		return styleBook{}, err
	}
	return styleBook{
		Header:       header,
		Even:         even,
		Odd:          odd,
		SummaryTitle: title,
		SummaryLabel: label,
		SummaryValue: value,
		SummaryMoney: money,
		SummaryPct:   pct,
		SummaryWarn:  warn,
	}, nil
}

func createRowStyles(f *excelize.File, fill string) (rowStyleSet, error) {
	text, err := cellStyle(f, fill, "", "left", true)
	if err != nil {
		return rowStyleSet{}, err
	}
	center, err := cellStyle(f, fill, "", "center", true)
	if err != nil {
		return rowStyleSet{}, err
	}
	integer, err := cellStyle(f, fill, "0", "center", false)
	if err != nil {
		return rowStyleSet{}, err
	}
	currency, err := cellStyle(f, fill, "¥#,##0.00", "right", false)
	if err != nil {
		return rowStyleSet{}, err
	}
	percent, err := cellStyle(f, fill, "0.00%", "right", false)
	if err != nil {
		return rowStyleSet{}, err
	}
	rating, err := cellStyle(f, fill, "0.0", "center", false)
	if err != nil {
		return rowStyleSet{}, err
	}
	date, err := cellStyle(f, fill, "yyyy-mm-dd", "center", false)
	if err != nil {
		return rowStyleSet{}, err
	}
	note, err := cellStyle(f, fill, "", "left", true)
	if err != nil {
		return rowStyleSet{}, err
	}
	return rowStyleSet{Text: text, Center: center, Integer: integer, Currency: currency, Percent: percent, Rating: rating, Date: date, Note: note}, nil
}

func cellStyle(f *excelize.File, fill, numFmt, horizontal string, wrap bool) (int, error) {
	style := &excelize.Style{
		Font:      &excelize.Font{Color: "1F2933", Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}},
		Alignment: &excelize.Alignment{Horizontal: horizontal, Vertical: "center", WrapText: wrap},
		Border:    thinBorder("DDE7EC"),
	}
	if numFmt != "" {
		style.CustomNumFmt = strPtr(numFmt)
	}
	return f.NewStyle(style)
}

func summaryValueStyle(f *excelize.File, numFmt string) (int, error) {
	return f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Color: "173B4A", Size: 12},
		Fill:         excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFFFFF"}},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:       thinBorder("D5E0E6"),
		CustomNumFmt: strPtr(numFmt),
	})
}

func thinBorder(hex string) []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: hex, Style: 1},
		{Type: "right", Color: hex, Style: 1},
		{Type: "top", Color: hex, Style: 1},
		{Type: "bottom", Color: hex, Style: 1},
	}
}

func addProductImage(f *excelize.File, row int, p product) error {
	img, err := productPNG(p)
	if err != nil {
		return err
	}
	return f.AddPictureFromBytes(dataSheet, fmt.Sprintf("A%d", row), &excelize.Picture{
		Extension: ".png",
		File:      img,
		Format: &excelize.GraphicOptions{
			AltText:             p.Name,
			AutoFit:             true,
			AutoFitIgnoreAspect: false,
			OffsetX:             3,
			OffsetY:             3,
			Positioning:         "oneCell",
		},
	})
}

func productPNG(p product) ([]byte, error) {
	const w, h = 120, 74
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg1 := lighten(p.Color, 0.82)
	bg2 := lighten(p.Color, 0.58)
	for y := 0; y < h; y++ {
		ratio := float64(y) / float64(h-1)
		line := mix(bg1, bg2, ratio)
		for x := 0; x < w; x++ {
			img.Set(x, y, line)
		}
	}
	fillCircle(img, 96, 14, 20, withAlpha(lighten(p.Color, 0.35), 120))
	fillCircle(img, 21, 57, 16, withAlpha(lighten(p.Color, 0.20), 140))
	fillRect(img, 8, 58, 112, 66, withAlpha(color.RGBA{255, 255, 255, 255}, 130))

	main := p.Color
	dark := darken(p.Color, 0.25)
	light := lighten(p.Color, 0.45)
	switch p.Shape % 4 {
	case 0:
		fillRect(img, 51, 18, 74, 58, main)
		fillRect(img, 56, 11, 69, 20, dark)
		fillRect(img, 55, 25, 70, 42, light)
		fillCircle(img, 51, 21, 4, dark)
		fillCircle(img, 74, 21, 4, dark)
	case 1:
		fillRect(img, 39, 23, 82, 58, main)
		fillRect(img, 45, 17, 76, 24, dark)
		fillRect(img, 47, 31, 74, 42, light)
		fillRect(img, 84, 29, 92, 57, dark)
	case 2:
		fillRect(img, 34, 21, 86, 52, color.RGBA{247, 250, 252, 255})
		fillRect(img, 39, 26, 81, 47, lighten(main, 0.72))
		fillRect(img, 54, 53, 66, 60, dark)
		fillRect(img, 46, 61, 74, 65, main)
	case 3:
		fillCircle(img, 60, 39, 25, main)
		fillCircle(img, 60, 39, 15, light)
		fillRect(img, 44, 21, 76, 29, dark)
		fillRect(img, 42, 51, 78, 57, dark)
	}
	fillRect(img, 16, 12, 29, 18, dark)
	fillRect(img, 16, 21, 37, 25, withAlpha(dark, 180))
	fillRect(img, 16, 28, 32, 32, withAlpha(dark, 140))

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	draw.Draw(img, image.Rect(x0, y0, x1, y1), image.NewUniform(c), image.Point{}, draw.Over)
}

func fillCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				blendPixel(img, x, y, c)
			}
		}
	}
}

func blendPixel(img *image.RGBA, x, y int, c color.RGBA) {
	if c.A == 255 {
		img.SetRGBA(x, y, c)
		return
	}
	dst := img.RGBAAt(x, y)
	a := float64(c.A) / 255
	img.SetRGBA(x, y, color.RGBA{
		R: uint8(float64(c.R)*a + float64(dst.R)*(1-a)),
		G: uint8(float64(c.G)*a + float64(dst.G)*(1-a)),
		B: uint8(float64(c.B)*a + float64(dst.B)*(1-a)),
		A: 255,
	})
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: 255,
	}
}

func lighten(c color.RGBA, amount float64) color.RGBA {
	return mix(c, color.RGBA{255, 255, 255, 255}, amount)
}

func darken(c color.RGBA, amount float64) color.RGBA {
	return mix(c, color.RGBA{20, 31, 42, 255}, amount)
}

func withAlpha(c color.RGBA, alpha uint8) color.RGBA {
	c.A = alpha
	return c
}

func verifyWorkbook(path string, expectedRows int) (int, int, int, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = f.Close() }()

	rows, err := f.GetRows(dataSheet)
	if err != nil {
		return 0, 0, 0, err
	}
	dataRows := len(rows) - 1
	lastRow := firstDataRow + expectedRows - 1
	formulas := 0
	for _, col := range []string{"K", "L", "M", "N", "O"} {
		for row := firstDataRow; row <= lastRow; row++ {
			formula, err := f.GetCellFormula(dataSheet, fmt.Sprintf("%s%d", col, row))
			if err != nil {
				return 0, 0, 0, err
			}
			if formula != "" {
				formulas++
			}
		}
	}
	for _, cell := range []string{"B3", "B4", "B5", "B6", "B7", "B8"} {
		formula, err := f.GetCellFormula(summarySheet, cell)
		if err != nil {
			return 0, 0, 0, err
		}
		if formula != "" {
			formulas++
		}
	}
	cells, err := f.GetPictureCells(dataSheet)
	if err != nil {
		return 0, 0, 0, err
	}
	pictures := 0
	for _, cell := range cells {
		pics, err := f.GetPictures(dataSheet, cell)
		if err != nil {
			return 0, 0, 0, err
		}
		pictures += len(pics)
	}
	if dataRows != expectedRows || formulas < expectedRows*5 || pictures != expectedRows {
		return dataRows, formulas, pictures, fmt.Errorf("verification failed for %s", path)
	}
	return dataRows, formulas, pictures, nil
}

func specs() []workbookSpec {
	return []workbookSpec{
		{
			Code:       "smart_home",
			FileName:   "供应商D_智能家居目录_60行_重复商品_公式图片.xlsx",
			Supplier:   "供应商D",
			Theme:      "智能家居",
			Categories: []string{"照明", "安防", "环境", "控制"},
			Products: products("SH", []string{
				"智能香薰机", "空气质量仪", "无线门铃套装", "桌面加湿器", "场景控制钮",
			}, []string{
				"智能温控面板", "睡眠感应灯", "迷你空气盒", "多功能网关", "门窗传感器",
				"智能插座", "节能暖风机", "厨房计时器", "床头阅读灯", "红外遥控器",
				"智能窗帘电机", "漏水报警器", "人体感应开关", "智能灯带套装", "家用能源表",
				"玄关感应灯", "浴室防雾镜", "智能晾衣架", "阳台光照仪", "水浸监测器",
				"智能门锁模块", "儿童房夜灯", "客厅情景灯", "桌面无线按钮", "家用红外探头",
				"智能插线板", "温湿度记录仪", "车库开门器", "智能喷香补充液", "可视门铃屏",
				"壁挂网关增强版", "声控床头灯", "蓝牙温湿度计", "节能插座面板", "烟雾报警器",
				"智能墙壁开关", "门厅小夜灯", "家居联动遥控器", "室内环境屏", "自动浇花器",
			}, []string{"照明", "安防", "环境", "控制"}, []color.RGBA{
				{42, 157, 143, 255}, {38, 70, 83, 255}, {233, 196, 106, 255}, {76, 110, 245, 255},
			}),
		},
		{
			Code:       "office_digital",
			FileName:   "供应商E_办公数码目录_60行_重复商品_公式图片.xlsx",
			Supplier:   "供应商E",
			Theme:      "办公数码",
			Categories: []string{"输入设备", "会议设备", "扩展配件", "电源"},
			Products: products("OD", []string{
				"智能香薰机", "无线门铃套装", "USB-C扩展坞", "多口快充插排",
			}, []string{
				"无线静音键盘", "蓝牙数字键盘", "便携显示器", "桌面麦克风", "高清会议摄像头",
				"降噪办公耳机", "磁吸充电底座", "固态移动硬盘", "折叠笔记本支架", "激光演示笔",
				"屏幕挂灯", "双模办公鼠标", "网络会议扬声器", "读卡器套装", "桌面理线器",
				"迷你扫码器", "防窥显示膜", "氮化镓充电器", "人体工学腕托", "会议白板贴",
				"桌面文件扫描仪", "迷你投影仪", "办公计时器", "电子标签机", "无线充电鼠标垫",
				"会议拾音器", "蓝牙翻页笔", "移动热点盒", "桌面补光灯", "可视化扩展屏",
				"机械键盘轴体盒", "会议室门牌屏", "笔记本散热架", "多屏切换器", "USB-C网卡",
				"办公电源管理器", "高速采集卡", "数字签名板", "智能笔筒", "会议预约灯",
			}, []string{"输入设备", "会议设备", "扩展配件", "电源"}, []color.RGBA{
				{87, 117, 144, 255}, {67, 97, 238, 255}, {247, 127, 0, 255}, {46, 196, 182, 255},
			}),
		},
		{
			Code:       "life_travel",
			FileName:   "供应商F_生活旅行目录_60行_重复商品_公式图片.xlsx",
			Supplier:   "供应商F",
			Theme:      "生活旅行",
			Categories: []string{"收纳", "清洁", "出行", "厨房"},
			Products: products("LT", []string{
				"智能香薰机", "空气质量仪", "桌面加湿器", "折叠烧水杯",
			}, []string{
				"压缩收纳袋", "旅行洗漱包", "便携挂烫机", "分类药品盒", "防水证件夹",
				"快干毛巾套装", "迷你清洁刷", "便携餐具盒", "真空保鲜罐", "行李箱绑带",
				"多功能转换插头", "桌面收纳抽屉", "厨房密封夹", "便携电子秤", "旅行分装瓶",
				"鞋袋收纳组", "可折叠衣架", "防滑杯套", "手持封口机", "衣物除味片",
				"便携小风扇", "旅行收纳杯", "车载垃圾袋", "可折叠水盆", "速干洗衣袋",
				"便携餐垫", "收纳标识贴", "户外防潮盒", "厨房计量勺", "冰箱分隔盒",
				"行李箱内胆包", "口袋清洁喷雾", "旅行晾晒绳", "伸缩数据线盒", "厨房沥水架",
				"便携药盒套装", "洗护分装漏斗", "旅行锁扣绳", "家用除尘刷", "迷你熨烫垫",
			}, []string{"收纳", "清洁", "出行", "厨房"}, []color.RGBA{
				{224, 122, 95, 255}, {61, 64, 91, 255}, {129, 178, 154, 255}, {242, 204, 143, 255},
			}),
		},
		{
			Code:       "mixed_channel",
			FileName:   "供应商G_渠道混合目录_60行_重复商品_公式图片.xlsx",
			Supplier:   "供应商G",
			Theme:      "渠道混合",
			Categories: []string{"家居", "办公", "旅行", "补给"},
			Products: products("MX", []string{
				"空气质量仪", "无线门铃套装", "USB-C扩展坞", "多口快充插排", "折叠烧水杯",
			}, []string{
				"渠道组合礼盒", "节能感应灯", "会议收纳包", "旅行快充套装", "桌面环境套件",
				"家用补充液", "便携显示支架", "防水整理袋", "智能插座套装", "办公清洁盒",
				"厨房收纳篮", "蓝牙标签贴", "车载收纳桶", "智能温湿度卡", "文件防潮箱",
				"家居遥控贴", "会议备用线", "户外电源包", "旅行证件包", "屏幕清洁器",
				"床头灯套装", "便携小音箱", "家用能源插头", "桌面读写灯", "分装瓶礼盒",
				"空气滤芯包", "电源整理夹", "键盘清洁刷", "衣物防尘罩", "厨房封口夹",
				"网络会议套装", "门窗贴片传感器", "行李绑带组", "USB-C数据线盒", "家居安全铃",
				"可折叠收纳箱", "桌面充电塔", "户外水杯套", "迷你投屏器", "智能夜灯组",
			}, []string{"家居", "办公", "旅行", "补给"}, []color.RGBA{
				{120, 81, 169, 255}, {34, 139, 230, 255}, {255, 183, 3, 255}, {89, 178, 112, 255},
			}),
		},
	}
}

func products(prefix string, shared, unique, categories []string, colors []color.RGBA) []product {
	const rowsPerWorkbook = 60
	base := time.Date(2026, 5, 3, 0, 0, 0, 0, time.Local)
	names := make([]string, 0, rowsPerWorkbook)
	names = append(names, shared...)
	suffixes := []string{"标准款", "升级款", "Pro", "组合装", "渠道装", "节能款", "轻巧款", "旗舰款"}
	for i := 0; len(names) < rowsPerWorkbook; i++ {
		name := unique[i%len(unique)]
		if round := i / len(unique); round > 0 {
			name = fmt.Sprintf("%s %s", name, suffixes[(round+i)%len(suffixes)])
		}
		names = append(names, name)
	}

	items := make([]product, 0, len(names))
	for i, name := range names {
		cost := 36 + float64((i*17)%130) + float64(i%3)*8.5
		price := cost*1.58 + float64((i%4)*12)
		purchase := 48 + (i*9)%90
		stock := 10 + (i*13)%95
		sales := 8 + (i*7)%54
		note := []string{"新品首批", "热卖款", "适合组合销售", "需关注库存"}[i%4]
		if i < len(shared) {
			note = "跨表重复测试，核对图片/公式/格式"
		}
		items = append(items, product{
			Name:     name,
			SKU:      fmt.Sprintf("%s-%03d", prefix, i+1),
			Category: categories[i%len(categories)],
			Unit:     []string{"件", "套", "盒", "台"}[i%4],
			Cost:     round2(cost),
			Price:    round2(price),
			Purchase: purchase,
			Stock:    stock,
			Sales:    sales,
			Rating:   3.8 + float64((i*3)%12)/10,
			Launch:   base.AddDate(0, 0, -i*5),
			Note:     note,
			Color:    colors[i%len(colors)],
			Shape:    i,
		})
	}
	return items
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func boolPtr(v bool) *bool { return &v }

func strPtr(v string) *string { return &v }

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, "[错误]", err)
	os.Exit(1)
}
