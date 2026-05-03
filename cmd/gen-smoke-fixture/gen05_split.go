package main

import (
	"fmt"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// gen05_SplitByColumn30Classes 生成 1.5 万行学生数据，**故意分布到 30 个班级**，
// 用于"按列值拆分" 模式：选 班级 列 → 拆分成 30 个文件。
//
// 测试用途：
//   - 拆分模式 by_column 性能（30 个分片）
//   - 文件名 sanitize（班级名含中文 + 数字）
//   - 输出文件夹一次性生成 30 个 xlsx
func gen05_SplitByColumn30Classes(dir string) error {
	const totalRows = 15_000
	const totalClasses = 30
	path := filepath.Join(dir, "05_按列值拆分_30个班.xlsx")

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "全校学生"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return err
	}

	headers := []any{"学号", "姓名", "班级", "年级", "性别", "总分"}
	if err := sw.SetRow("A1", headers); err != nil {
		return err
	}
	_ = sw.SetColWidth(1, 1, 12)
	_ = sw.SetColWidth(2, 2, 10)
	_ = sw.SetColWidth(3, 3, 14)
	_ = sw.SetColWidth(4, 4, 10)

	surnames := []string{"王", "李", "张", "刘", "陈", "杨"}
	givens := []string{"明", "华", "芳", "敏", "静", "丽", "强", "磊"}
	grades := []string{"一年级", "二年级", "三年级", "四年级", "五年级", "六年级"}

	for i := 0; i < totalRows; i++ {
		row := i + 2
		stuID := fmt.Sprintf("S%05d", 10001+i)
		name := surnames[i%len(surnames)] + givens[(i*3)%len(givens)]
		// 班级：1班-30班 平均分布（每 500 行换一班）
		classNum := (i / 500) % totalClasses // 0-29
		class := fmt.Sprintf("%d班", classNum+1)
		grade := grades[classNum/5]
		gender := []string{"男", "女"}[i%2]
		total := 200 + (i*7)%301

		cells := []any{stuID, name, class, grade, gender, total}
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := sw.SetRow(cell, cells); err != nil {
			return err
		}
	}
	if err := sw.Flush(); err != nil {
		return err
	}
	return f.SaveAs(path)
}
