package main

// gen07_CSVSamples 生成 5 个 CSV 样例文件，覆盖项目支持的编码 × 分隔符组合，
// 所有文件共享同一套表头和业务数据（同一套 10000 行订单）：
//
//	07_CSV_UTF8_逗号_1万行.csv         UTF-8 无 BOM + 逗号（Linux/Web 导出最常见）
//	07_CSV_UTF8BOM_逗号_1万行.csv      UTF-8 带 BOM + 逗号（Windows Excel 另存默认）
//	07_CSV_GBK_逗号_1万行.csv          GBK + 逗号（老版 WPS / 国内老系统）
//	07_CSV_UTF8_分号_1万行.csv         UTF-8 + 分号（欧洲地区法德习惯）
//	07_CSV_UTF8_Tab_1万行.csv          UTF-8 + Tab（TSV 风格 / 数据库导出）
//
// 关键词埋点（与 fixture 02 风格一致，方便测试对齐）：
//   - "VIP 客户" 约 2700 行 / 10000 (customers[1] 循环 + i%50==0 强制埋点)
//   - "退货"     约 1446 行 / 10000 (notes[3] 循环 + i%500==0 强制埋点)
//
// 作用场景：
//   - 冒烟测试 CSV 编码自动嗅探（OpenCSV 的 DetectCSVEncoding 路径）
//   - 冒烟测试分隔符自动推断（OpenCSV 的 pickDelimiter 路径）
//   - per_keyword / per_source / merged 三种输出策略在 CSV 源下的行为
//   - 混源场景：xlsx 与 CSV 同文件夹批量提取（提醒用 fixture 06 + 07 组合）
//
// 为什么不用 StreamWriter：CSV 是纯文本 + 外层可能 transform.NewWriter，
// excelize StreamWriter 无对应 API，直接用 encoding/csv.Writer 更干净。

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func gen07_CSVSamples(dir string) error {
	const totalRows = 10_000
	headers := []string{"订单号", "客户类型", "产品", "数量", "单价", "备注"}
	customers := []string{"普通客户", "VIP 客户", "金牌客户", "新客户"}
	products := []string{
		"标准款笔记本", "无线鼠标", "机械键盘", "USB-C 集线器",
		"4K 显示器", "降噪耳机", "便携硬盘", "扩展坞 Pro",
	}
	notes := []string{"正常订单", "加急", "促销", "退货", "首次", "续费", "代发"}

	buildRows := func() [][]string {
		rows := make([][]string, 0, totalRows+1)
		rows = append(rows, headers)
		for i := 0; i < totalRows; i++ {
			orderNo := fmt.Sprintf("ORD-%08d", 20260000+i)

			// 客户：每 50 行一个 VIP；其他循环
			var customer string
			if i%50 == 0 {
				customer = "VIP 客户"
			} else {
				customer = customers[i%len(customers)]
			}

			product := products[i%len(products)]
			qty := 1 + (i*3)%50     // 1-50
			price := 80 + (i*7)%420 // 80-500

			note := notes[i%len(notes)]
			if i%500 == 0 {
				note = "退货"
			}

			rows = append(rows, []string{
				orderNo, customer, product,
				fmt.Sprintf("%d", qty),
				fmt.Sprintf("%d", price),
				note,
			})
		}
		return rows
	}

	rows := buildRows()

	type variant struct {
		name      string
		delimiter rune
		writeBOM  bool
		gbk       bool
	}
	variants := []variant{
		{"07_CSV_UTF8_逗号_1万行.csv", ',', false, false},
		{"07_CSV_UTF8BOM_逗号_1万行.csv", ',', true, false},
		{"07_CSV_GBK_逗号_1万行.csv", ',', false, true},
		{"07_CSV_UTF8_分号_1万行.csv", ';', false, false},
		{"07_CSV_UTF8_Tab_1万行.csv", '\t', false, false},
	}

	for _, v := range variants {
		outPath := filepath.Join(dir, v.name)
		if err := writeCSVVariant(outPath, rows, v.delimiter, v.writeBOM, v.gbk); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", v.name, err)
		}
	}

	return nil
}

// writeCSVVariant 写一个 CSV 文件，指定分隔符、是否加 BOM、是否 GBK 编码。
//
// 流程：
//
//	os.Create -> 可选 UTF-8 BOM -> 可选 GBK transform.NewWriter ->
//	csv.Writer + delimiter -> WriteAll
func writeCSVVariant(path string, rows [][]string, delimiter rune, writeBOM, gbk bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// UTF-8 BOM（仅当 !gbk 且 writeBOM 时）：先写 EF BB BF，再写内容。
	// GBK 不用 BOM（GBK 没有标准 BOM，Excel 也靠嗅探区分不靠 BOM）。
	var w io.Writer = f
	if !gbk && writeBOM {
		if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return err
		}
	}
	if gbk {
		w = transform.NewWriter(f, simplifiedchinese.GBK.NewEncoder())
	}

	cw := csv.NewWriter(w)
	cw.Comma = delimiter
	if err := cw.WriteAll(rows); err != nil {
		return err
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}

	// GBK transform.Writer 没有 Close；cw.Flush 已经吐完内容；最终 f.Close 由 defer 处理。
	// 但 transform.NewWriter 在 UTF-8 片段越过内部 buffer 边界时可能需要显式 Close
	// 刷掉残余字节，所以显式关一下（如果是 *transform.Writer）。
	if tw, ok := w.(*transform.Writer); ok {
		if err := tw.Close(); err != nil {
			return err
		}
	}
	return nil
}
