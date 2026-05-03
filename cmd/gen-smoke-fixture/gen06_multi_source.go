package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// gen06_MultiSourceMerged 生成 5 个 "供应商" 文件，放在 06_多源/ 子目录。
// 每个文件 1 万行，相同表头（订单号 / 客户 / 产品 / 数量 / 金额 / 备注）。
//
// 测试用途：批量提取 → "合成一个文件" 模式
//   - 5 源 × 1 万行 = 5 万行总数据
//   - 关键词"VIP" 在每个源里都有（每文件约 200 行）
//   - 验证 merged 模式跨源合并不丢数据
//   - 验证 zip surgery 多源场景（primary + secondaries）
func gen06_MultiSourceMerged(dir string) error {
	subDir := filepath.Join(dir, "06_多源")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return err
	}

	suppliers := []struct {
		Code     string
		Name     string
		Products []string
	}{
		{"A", "供应商A_华东电子", []string{"笔记本", "鼠标", "键盘", "显示器"}},
		{"B", "供应商B_华南数码", []string{"平板", "手机壳", "耳机", "充电器"}},
		{"C", "供应商C_华北办公", []string{"打印机", "墨盒", "纸张", "胶水"}},
		{"D", "供应商D_西北物流", []string{"包装盒", "胶带", "标签", "封口袋"}},
		{"E", "供应商E_西南文具", []string{"笔", "本子", "尺子", "橡皮"}},
	}

	customers := []string{"普通客户", "VIP", "金牌客户", "新客户", "回头客"}

	for _, sup := range suppliers {
		const rowsPerFile = 10_000
		fname := fmt.Sprintf("%s.xlsx", sup.Name)
		fpath := filepath.Join(subDir, fname)

		f := excelize.NewFile()
		sheet := "订单明细"
		if err := f.SetSheetName("Sheet1", sheet); err != nil {
			_ = f.Close()
			return err
		}
		sw, err := f.NewStreamWriter(sheet)
		if err != nil {
			_ = f.Close()
			return err
		}

		headers := []any{"订单号", "客户", "产品", "数量", "金额", "备注"}
		if err := sw.SetRow("A1", headers); err != nil {
			_ = f.Close()
			return err
		}

		for i := 0; i < rowsPerFile; i++ {
			row := i + 2
			orderNo := fmt.Sprintf("%s-%06d", sup.Code, 10001+i)
			// 客户：每 50 行有一次 VIP
			cust := customers[i%len(customers)]
			if i%50 == 0 {
				cust = "VIP"
			}
			product := sup.Products[i%len(sup.Products)]
			qty := 1 + (i*3)%50
			amount := qty * (50 + (i*11)%150)
			note := []string{"正常", "加急", "促销", "代发"}[i%4]

			cells := []any{orderNo, cust, product, qty, amount, note}
			cell, _ := excelize.CoordinatesToCellName(1, row)
			if err := sw.SetRow(cell, cells); err != nil {
				_ = f.Close()
				return err
			}
		}
		if err := sw.Flush(); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.SaveAs(fpath); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
		fmt.Printf("    生成: %s\n", fpath)
	}
	return nil
}
