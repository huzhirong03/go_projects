package main

import (
	"fmt"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// gen02_OrdersWithFormulas 生成 3 万行订单表，**故意制造 shared formula** 让 Excel
// 把"金额 = 数量 × 单价"压缩成共享公式（主公式只在 H2，其他行写 si=0 引用）。
// 同时也生成 calcChain.xml。
//
// 这是上一轮 zip-surgery 修复的回归 fixture：
//   - 单文件 merged 提取后，应不再弹"部分内容有问题"
//   - 共享公式应被展开成独立公式
//   - calcChain.xml 应被自动重建（不复制到输出）
//
// 关键词埋点：
//   - "VIP 客户" 每 50 行一个 = 600 行
//   - "促销" 每 30 行一个 = 1000 行
//   - "退货" 偶尔出现（每 500 行一个 = 60 行）
//
// 注：excelize 的 SetCellFormula 默认写 normal formula，要触发 shared formula
// 需要在 SetCellFormula 时显式指定 ref + type=shared。但 excelize 的高层 API
// 不直接暴露这个，所以我们直接用 StreamWriter + raw cell 注入 shared 标记。
func gen02_OrdersWithFormulas(dir string) error {
	const totalRows = 30_000
	path := filepath.Join(dir, "02_订单含公式_3万行.xlsx")

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "订单明细"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}

	headers := []string{"订单号", "客户类型", "产品", "数量", "单价", "促销标记", "备注", "金额", "折扣", "实收"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return err
		}
	}

	customers := []string{"普通客户", "VIP 客户", "金牌客户", "新客户"}
	products := []string{
		"标准款笔记本", "无线鼠标", "机械键盘", "USB-C 集线器",
		"4K 显示器", "降噪耳机", "便携硬盘", "扩展坞 Pro",
	}
	notes := []string{"正常订单", "加急", "促销", "退货", "首次", "续费", "代发"}

	// ---- 写数据行 + 公式 ----
	// 关键：B 列（客户类型）要保证"VIP 客户" 出现 ~600 次
	// G 列（备注）要保证"促销" / "退货" 命中
	for i := 0; i < totalRows; i++ {
		row := i + 2
		orderNo := fmt.Sprintf("ORD-%08d", 20260000+i)

		// 客户：每 50 行第 1 行设为 VIP；其他循环
		var customer string
		if i%50 == 0 {
			customer = "VIP 客户"
		} else {
			customer = customers[i%len(customers)]
		}

		product := products[i%len(products)]
		qty := 1 + (i*3)%50    // 1-50
		price := 80 + (i*7)%420 // 80-500
		// 促销标记
		promo := ""
		if i%30 == 0 {
			promo = "促销"
		}
		// 备注
		note := notes[i%len(notes)]
		if i%500 == 0 {
			note = "退货"
		}

		setCell(f, sheet, 1, row, orderNo)
		setCell(f, sheet, 2, row, customer)
		setCell(f, sheet, 3, row, product)
		setCell(f, sheet, 4, row, qty)
		setCell(f, sheet, 5, row, price)
		setCell(f, sheet, 6, row, promo)
		setCell(f, sheet, 7, row, note)

		// H 列：金额 = 数量 × 单价
		if i == 0 {
			// 主公式（带 shared 标记，覆盖 H2:H30001）
			ref := fmt.Sprintf("H%d:H%d", 2, totalRows+1)
			_ = f.SetCellFormula(sheet, fmt.Sprintf("H%d", row),
				fmt.Sprintf("D%d*E%d", row, row),
				excelize.FormulaOpts{
					Type: stringPtr("shared"),
					Ref:  &ref,
				})
		} else {
			// follower：写 si=0 引用主
			_ = f.SetCellFormula(sheet, fmt.Sprintf("H%d", row),
				"",
				excelize.FormulaOpts{
					Type: stringPtr("shared"),
				})
		}

		// I 列：折扣（VIP 客户 0.9，其他 1.0）—— 普通公式
		_ = f.SetCellFormula(sheet, fmt.Sprintf("I%d", row),
			fmt.Sprintf("IF(B%d=\"VIP 客户\",0.9,1)", row))

		// J 列：实收 = 金额 × 折扣
		_ = f.SetCellFormula(sheet, fmt.Sprintf("J%d", row),
			fmt.Sprintf("H%d*I%d", row, row))
	}

	// 列宽
	widths := map[string]float64{"A": 16, "B": 12, "C": 18, "D": 6, "E": 8, "F": 10, "G": 12, "H": 10, "I": 8, "J": 10}
	for col, w := range widths {
		_ = f.SetColWidth(sheet, col, col, w)
	}
	return f.SaveAs(path)
}

func stringPtr(s string) *string { return &s }

func setCell(f *excelize.File, sheet string, col, row int, v any) {
	cell, _ := excelize.CoordinatesToCellName(col, row)
	_ = f.SetCellValue(sheet, cell, v)
}
