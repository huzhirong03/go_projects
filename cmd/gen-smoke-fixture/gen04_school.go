package main

import (
	"fmt"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// gen04_SchoolMultiSheet 复刻你那个出 bug 的学校 case：
//   - 4 个 Sheet：学生明细 / 班级统计 / 年级统计 / 教师列表
//   - 学生明细 3000 行，每行带照片
//   - 班级统计、年级统计 用跨 sheet 公式（=SUM(学生明细!K2:K3001) 这种）
//   - 关键词埋点："六年级" 在学生明细中 500 行
//
// 测试用途：**回归 commit 38214d9 + a0bc3a7**
//   - merged 模式提取"六年级"后：
//     · 输出文件应保留所有 4 个 sheet（不丢数据）
//     · 跨 sheet 公式不报错（共享公式被展开）
//     · calcChain 不残留导致 Excel "部分内容有问题"
func gen04_SchoolMultiSheet(dir string) error {
	const detailRows = 3_000
	path := filepath.Join(dir, "04_学校成绩册_多Sheet交叉公式.xlsx")

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// === Sheet 1: 学生成绩明细 ===
	detailSheet := "学生成绩明细"
	if err := f.SetSheetName("Sheet1", detailSheet); err != nil {
		return err
	}
	headersA := []string{"学号", "姓名", "班级", "年级", "性别", "语文", "数学", "英语", "物理", "化学", "总分", "评级", "照片"}
	for i, h := range headersA {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(detailSheet, cell, h)
	}
	widths := map[string]float64{"A": 10, "B": 10, "C": 8, "D": 10, "E": 6, "F": 6, "G": 6, "H": 6, "I": 6, "J": 6, "K": 8, "L": 8, "M": 12}
	for col, w := range widths {
		_ = f.SetColWidth(detailSheet, col, col, w)
	}
	_ = f.SetRowHeight(detailSheet, 1, 28)

	grades := []string{"一年级", "二年级", "三年级", "四年级", "五年级", "六年级"}
	classes := []string{"1班", "2班", "3班", "4班", "5班"}
	surnames := []string{"王", "李", "张", "刘", "陈", "杨", "黄", "赵", "吴", "周"}
	givens := []string{"明", "华", "芳", "敏", "静", "丽", "强", "磊", "军", "洋"}

	// 预生成 8 张照片
	photos := make([][]byte, 8)
	for i := 0; i < 8; i++ {
		photos[i] = makeAvatar(uint8(i*32), uint8(255-i*32), 150)
	}

	for i := 0; i < detailRows; i++ {
		row := i + 2
		stuID := fmt.Sprintf("S%05d", 10001+i)
		name := surnames[i%len(surnames)] + givens[(i*3)%len(givens)] + givens[(i*5)%len(givens)]
		// 班级：每 100 行轮换；年级每 500 行变化（六年级 500 行：i=2500-2999）
		grade := grades[(i/500)%len(grades)]
		class := classes[(i/100)%len(classes)]
		gender := []string{"男", "女"}[i%2]
		yu := 60 + (i*17)%41
		shu := 60 + (i*23)%41
		yin := 60 + (i*31)%41
		wu := 60 + (i*37)%41
		hua := 60 + (i*41)%41

		setCell(f, detailSheet, 1, row, stuID)
		setCell(f, detailSheet, 2, row, name)
		setCell(f, detailSheet, 3, row, class)
		setCell(f, detailSheet, 4, row, grade)
		setCell(f, detailSheet, 5, row, gender)
		setCell(f, detailSheet, 6, row, yu)
		setCell(f, detailSheet, 7, row, shu)
		setCell(f, detailSheet, 8, row, yin)
		setCell(f, detailSheet, 9, row, wu)
		setCell(f, detailSheet, 10, row, hua)
		// 总分：用普通公式
		_ = f.SetCellFormula(detailSheet, fmt.Sprintf("K%d", row),
			fmt.Sprintf("F%d+G%d+H%d+I%d+J%d", row, row, row, row, row))
		// 评级：用 IF 公式
		_ = f.SetCellFormula(detailSheet, fmt.Sprintf("L%d", row),
			fmt.Sprintf(`IF(K%d>=400,"优秀",IF(K%d>=350,"良好",IF(K%d>=300,"中等","待加强")))`, row, row, row))

		_ = f.SetRowHeight(detailSheet, row, 36)
		// 照片
		photoCell, _ := excelize.CoordinatesToCellName(13, row)
		_ = f.AddPictureFromBytes(detailSheet, photoCell, &excelize.Picture{
			Extension: ".png",
			File:      photos[i%8],
			Format: &excelize.GraphicOptions{
				AltText:     name,
				AutoFit:     true,
				Positioning: "oneCell",
			},
		})
	}

	// === Sheet 2: 班级统计（跨 sheet 公式） ===
	classSheet := "班级统计"
	if _, err := f.NewSheet(classSheet); err != nil {
		return err
	}
	classHeaders := []string{"年级", "班级", "学生数", "平均总分", "优秀人数"}
	for i, h := range classHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(classSheet, cell, h)
	}
	rowIdx := 2
	for _, g := range grades {
		for _, c := range classes {
			setCell(f, classSheet, 1, rowIdx, g)
			setCell(f, classSheet, 2, rowIdx, c)
			// 跨 sheet COUNTIFS / AVERAGEIFS
			_ = f.SetCellFormula(classSheet, fmt.Sprintf("C%d", rowIdx),
				fmt.Sprintf(`COUNTIFS(学生成绩明细!D2:D%d,A%d,学生成绩明细!C2:C%d,B%d)`,
					detailRows+1, rowIdx, detailRows+1, rowIdx))
			_ = f.SetCellFormula(classSheet, fmt.Sprintf("D%d", rowIdx),
				fmt.Sprintf(`IFERROR(AVERAGEIFS(学生成绩明细!K2:K%d,学生成绩明细!D2:D%d,A%d,学生成绩明细!C2:C%d,B%d),0)`,
					detailRows+1, detailRows+1, rowIdx, detailRows+1, rowIdx))
			_ = f.SetCellFormula(classSheet, fmt.Sprintf("E%d", rowIdx),
				fmt.Sprintf(`COUNTIFS(学生成绩明细!D2:D%d,A%d,学生成绩明细!C2:C%d,B%d,学生成绩明细!L2:L%d,"优秀")`,
					detailRows+1, rowIdx, detailRows+1, rowIdx, detailRows+1))
			rowIdx++
		}
	}

	// === Sheet 3: 年级统计 ===
	gradeSheet := "年级统计"
	if _, err := f.NewSheet(gradeSheet); err != nil {
		return err
	}
	gradeHeaders := []string{"年级", "学生总数", "平均分", "最高分", "最低分"}
	for i, h := range gradeHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(gradeSheet, cell, h)
	}
	for i, g := range grades {
		row := i + 2
		setCell(f, gradeSheet, 1, row, g)
		_ = f.SetCellFormula(gradeSheet, fmt.Sprintf("B%d", row),
			fmt.Sprintf(`COUNTIF(学生成绩明细!D2:D%d,A%d)`, detailRows+1, row))
		_ = f.SetCellFormula(gradeSheet, fmt.Sprintf("C%d", row),
			fmt.Sprintf(`IFERROR(AVERAGEIF(学生成绩明细!D2:D%d,A%d,学生成绩明细!K2:K%d),0)`,
				detailRows+1, row, detailRows+1))
		_ = f.SetCellFormula(gradeSheet, fmt.Sprintf("D%d", row),
			fmt.Sprintf(`MAXIFS(学生成绩明细!K2:K%d,学生成绩明细!D2:D%d,A%d)`,
				detailRows+1, detailRows+1, row))
		_ = f.SetCellFormula(gradeSheet, fmt.Sprintf("E%d", row),
			fmt.Sprintf(`MINIFS(学生成绩明细!K2:K%d,学生成绩明细!D2:D%d,A%d)`,
				detailRows+1, detailRows+1, row))
	}

	// === Sheet 4: 教师列表（少量数据，验证小 sheet 也能保留） ===
	teacherSheet := "教师列表"
	if _, err := f.NewSheet(teacherSheet); err != nil {
		return err
	}
	teacherHeaders := []string{"工号", "姓名", "年级", "科目", "联系方式"}
	for i, h := range teacherHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(teacherSheet, cell, h)
	}
	subjects := []string{"语文", "数学", "英语", "物理", "化学"}
	for i := 0; i < 30; i++ {
		row := i + 2
		setCell(f, teacherSheet, 1, row, fmt.Sprintf("T%03d", i+1))
		setCell(f, teacherSheet, 2, row, surnames[i%len(surnames)]+"老师")
		setCell(f, teacherSheet, 3, row, grades[i%len(grades)])
		setCell(f, teacherSheet, 4, row, subjects[i%len(subjects)])
		setCell(f, teacherSheet, 5, row, fmt.Sprintf("139%08d", 10000000+i))
	}

	// 默认显示在学生明细
	idx, _ := f.GetSheetIndex(detailSheet)
	f.SetActiveSheet(idx)

	// 触发公式重算（保存时 Excel 会立即看到值）
	if err := f.UpdateLinkedValue(); err != nil {
		return err
	}

	return f.SaveAs(path)
}
