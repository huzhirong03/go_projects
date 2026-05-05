package main

import (
	"fmt"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// gen02_OrdersWithFormulas 生成 3 万行订单表，每行带 H/I/J 三列公式 + 预计算 <v> 缓存。
//
// 设计取舍：
//   - 早期版本用 shared formula（主公式在 H2，H3-H30001 写 follower 引用）尝试
//     复刻 calcChain 回归测试，但 excelize 对 shared follower 的 Si 绑定支持
//     不稳 → 文件在 Excel/WPS 打开时除 H2 外其他行全空，误导读者以为 fixture
//     本身坏了。现已放弃 shared 手工拼装。
//   - shared formula 路径的正确性已由 internal/excelio（含 <f t="shared"/> 的
//     小 fixture 单测）和 internal/extractor 的单测全面覆盖，不再依赖 smoke
//     fixture 承担此职责。
//   - 本 fixture 的当前职责：① 3 万行公式性能冒烟 ② 公式 + <v> 缓存共存场景
//     （真实业务文件最典型的状态）③ 关键词"VIP 客户 / 促销 / 退货"埋点。
//
// 写入顺序陷阱（实测踩坑）：
//   - excelize v2 的 SetCellInt / SetCellFloat 内部会显式 `c.F = nil` 清掉公式；
//   - SetCellFormula 只改 c.F，不动 c.V。
//     所以**必须先写缓存值再写公式**，顺序颠倒会导致所有 cell 只有 <v> 没有 <f>。
//     最终每个 cell 是 <c><f>D2*E2</f><v>80</v></c>。
//
// 关键词埋点：
//   - "VIP 客户" 每 50 行一个 = 600 行
//   - "促销" F 列每 30 行一个 = 1000 行
//   - "退货" G 列每 500 行一个 = 60 行
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

	// ---- 写数据行 + 公式 + 预计算缓存 ----
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
		qty := 1 + (i*3)%50     // 1-50
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

		// --- H 列：金额 = 数量 × 单价（先写值，再写公式，保留二者）---
		hRef := fmt.Sprintf("H%d", row)
		amount := qty * price
		_ = f.SetCellInt(sheet, hRef, amount)
		_ = f.SetCellFormula(sheet, hRef, fmt.Sprintf("D%d*E%d", row, row))

		// --- I 列：折扣（VIP 客户 0.9，其他 1.0）---
		iRef := fmt.Sprintf("I%d", row)
		discount := 1.0
		if customer == "VIP 客户" {
			discount = 0.9
		}
		_ = f.SetCellFloat(sheet, iRef, discount, 2, 64)
		_ = f.SetCellFormula(sheet, iRef, fmt.Sprintf("IF(B%d=\"VIP 客户\",0.9,1)", row))

		// --- J 列：实收 = 金额 × 折扣 ---
		jRef := fmt.Sprintf("J%d", row)
		actual := float64(amount) * discount
		_ = f.SetCellFloat(sheet, jRef, actual, 2, 64)
		_ = f.SetCellFormula(sheet, jRef, fmt.Sprintf("H%d*I%d", row, row))
	}

	// 列宽
	widths := map[string]float64{"A": 16, "B": 12, "C": 18, "D": 6, "E": 8, "F": 10, "G": 12, "H": 10, "I": 8, "J": 10}
	for col, w := range widths {
		_ = f.SetColWidth(sheet, col, col, w)
	}
	return f.SaveAs(path)
}

func setCell(f *excelize.File, sheet string, col, row int, v any) {
	cell, _ := excelize.CoordinatesToCellName(col, row)
	_ = f.SetCellValue(sheet, cell, v)
}
