package main

import (
	"fmt"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// gen01_StudentBigTable 生成 10 万行学生信息表（无图，纯文本+数字）。
//
// 关键词埋点（方便测试搜索）：
//   - "六年级" 共 5000 行（每 20 行一个，可用于"按关键词提取"）
//   - "高三"   共 5000 行
//   - "重点班" 共 1000 行（每 100 行一个）
//   - "示范学校" 共 100 行（每 1000 行一个）
//
// 测试用途：
//   - 流式读性能（10 万行不应 OOM）
//   - 关键词匹配（精准/包含/拼音）
//   - 大文件预警 banner（>50MB 触发）
//   - 输出 merged xlsx 不报错
func gen01_StudentBigTable(dir string) error {
	const totalRows = 100_000
	path := filepath.Join(dir, "01_学生信息_10万行.xlsx")

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "学生信息"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return err
	}

	// 列宽
	for col, w := range map[int]float64{1: 12, 2: 10, 3: 10, 4: 8, 5: 8, 6: 8, 7: 16, 8: 8, 9: 8, 10: 8, 11: 8, 12: 8, 13: 8, 14: 18} {
		_ = sw.SetColWidth(col, col, w)
	}

	// 表头
	headers := []any{
		"学号", "姓名", "班级", "年级", "性别", "民族", "联系方式",
		"语文", "数学", "英语", "物理", "化学", "总分", "学校类型",
	}
	if err := sw.SetRow("A1", headers); err != nil {
		return err
	}

	// 数据行
	surnames := []string{"王", "李", "张", "刘", "陈", "杨", "黄", "赵", "吴", "周", "徐", "孙", "胡", "朱", "高", "林", "何", "郭", "马", "罗"}
	givens := []string{"明", "华", "芳", "敏", "静", "丽", "强", "磊", "军", "洋", "勇", "艳", "杰", "娟", "涛", "超", "鹏", "雪", "玉", "刚"}
	classes := []string{"1班", "2班", "3班", "4班", "5班", "6班", "7班", "8班", "重点班", "实验班"}
	grades := []string{"一年级", "二年级", "三年级", "四年级", "五年级", "六年级", "初一", "初二", "初三", "高一", "高二", "高三"}
	ethnics := []string{"汉族", "回族", "满族", "蒙古族", "维吾尔族", "壮族", "藏族"}
	schoolTypes := []string{"普通学校", "示范学校", "重点学校", "实验学校"}

	for i := 0; i < totalRows; i++ {
		row := i + 2
		// 学号：S 开头 8 位数字
		stuID := fmt.Sprintf("S%07d", 20260000+i)
		// 姓名：随机姓 + 名（2 字）
		name := surnames[i%len(surnames)] + givens[(i*7+3)%len(givens)] + givens[(i*13+5)%len(givens)]
		// 班级：每 20 行一个班级循环；"重点班" 出现频率定为每 100 行一次
		var class string
		if i%100 == 0 {
			class = "重点班"
		} else {
			class = classes[(i/20)%len(classes)]
		}
		// 年级：每 20 行一个年级，"六年级" 命中频率约 1/12
		grade := grades[(i/20)%len(grades)]
		gender := []string{"男", "女"}[i%2]
		ethnic := ethnics[(i*3)%len(ethnics)]
		// 联系方式：13 开头 11 位
		phone := fmt.Sprintf("13%09d", 800000000+i*7919%200000000)
		// 各科成绩（60-100，伪随机分布）
		yu := 60 + (i*17)%41
		shu := 60 + (i*23)%41
		yin := 60 + (i*31)%41
		wu := 60 + (i*37)%41
		hua := 60 + (i*41)%41
		total := yu + shu + yin + wu + hua
		// 学校类型：每 1000 行才出现一次"示范学校"
		var st string
		if i%1000 == 0 {
			st = "示范学校"
		} else {
			st = schoolTypes[i%2] // 普通 / 重点 交替
		}

		cells := []any{stuID, name, class, grade, gender, ethnic, phone, yu, shu, yin, wu, hua, total, st}
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
