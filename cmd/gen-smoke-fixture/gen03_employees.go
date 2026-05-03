package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// gen03_EmployeesWithPhotos 生成 2 万行员工花名册，**每行一张照片**。
//
// 测试用途：
//   - 大量图片提取性能（excelize.GetPictures O(N²) 已优化为 zip 直读）
//   - 图片迁移到输出文件（merged / per_source / 拆分 by_keyword）
//   - 图片锚点重映射（命中行 -> 输出行的 from.row 偏移）
//   - 内存压力（2 万张 256B 图 = 5 MB 图字节，全表 ~30-40 MB）
//
// 关键词埋点（部门）：
//   - "研发部" 5000 行
//   - "销售部" 5000 行
//   - "市场部" 4000 行
//   - "客服部" 3000 行
//   - "行政部" 3000 行
func gen03_EmployeesWithPhotos(dir string) error {
	const totalRows = 20_000
	path := filepath.Join(dir, "03_员工花名册_2万行带照片.xlsx")

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "员工档案"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}

	headers := []string{"工号", "姓名", "性别", "部门", "岗位", "入职日期", "学历", "联系方式", "照片"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	// 列宽 + 表头行高
	widths := map[string]float64{"A": 12, "B": 10, "C": 6, "D": 10, "E": 14, "F": 13, "G": 8, "H": 14, "I": 12}
	for col, w := range widths {
		_ = f.SetColWidth(sheet, col, col, w)
	}
	_ = f.SetRowHeight(sheet, 1, 28)

	depts := []string{"研发部", "研发部", "销售部", "销售部", "市场部", "市场部", "客服部", "行政部"}
	posByDept := map[string][]string{
		"研发部": {"工程师", "高级工程师", "架构师", "技术经理"},
		"销售部": {"销售代表", "销售主管", "区域经理"},
		"市场部": {"市场专员", "市场经理", "品牌策划"},
		"客服部": {"客服代表", "客服主管"},
		"行政部": {"行政专员", "行政经理", "前台"},
	}
	surnames := []string{"王", "李", "张", "刘", "陈", "杨", "黄", "赵", "吴", "周", "徐", "孙", "胡", "朱", "高"}
	givens := []string{"明", "华", "芳", "敏", "静", "丽", "强", "磊", "军", "洋", "勇", "艳", "杰", "娟", "涛", "超", "鹏"}
	educations := []string{"本科", "硕士", "本科", "本科", "硕士", "博士", "大专", "本科"}

	// 预生成 16 个不同颜色的小头像，循环使用（避免每行都生成 PNG 太慢）
	imgCache := make(map[int][]byte)
	for i := 0; i < 16; i++ {
		hue := uint8((i * 16) % 256)
		imgCache[i] = makeAvatar(hue, uint8(255-hue), 100)
	}

	for i := 0; i < totalRows; i++ {
		row := i + 2
		empID := fmt.Sprintf("E%07d", 20260001+i)
		name := surnames[i%len(surnames)] + givens[(i*7)%len(givens)] + givens[(i*11)%len(givens)]
		gender := []string{"男", "女"}[i%2]
		dept := depts[i%len(depts)]
		pos := posByDept[dept][i%len(posByDept[dept])]
		// 入职日期：2010 年至 2026 年的某天
		year := 2010 + i%17
		month := 1 + i%12
		day := 1 + i%28
		hireDate := fmt.Sprintf("%d-%02d-%02d", year, month, day)
		edu := educations[i%len(educations)]
		phone := fmt.Sprintf("13%09d", 800000000+i*7919%200000000)

		setCell(f, sheet, 1, row, empID)
		setCell(f, sheet, 2, row, name)
		setCell(f, sheet, 3, row, gender)
		setCell(f, sheet, 4, row, dept)
		setCell(f, sheet, 5, row, pos)
		setCell(f, sheet, 6, row, hireDate)
		setCell(f, sheet, 7, row, edu)
		setCell(f, sheet, 8, row, phone)

		// 行高
		_ = f.SetRowHeight(sheet, row, 36)

		// 插入头像到 I 列
		imgBytes := imgCache[i%16]
		cell, _ := excelize.CoordinatesToCellName(9, row)
		err := f.AddPictureFromBytes(sheet, cell, &excelize.Picture{
			Extension: ".png",
			File:      imgBytes,
			Format: &excelize.GraphicOptions{
				AltText:     name,
				AutoFit:     true,
				Positioning: "oneCell",
				OffsetX:     2,
				OffsetY:     2,
			},
		})
		if err != nil {
			return fmt.Errorf("第 %d 行插入图片失败: %w", row, err)
		}
	}

	return f.SaveAs(path)
}

// makeAvatar 生成一张 36x36 的小头像，单色填充 + 中央圆形点缀。
// 体积控制在 ~150-300B，2 万张总共 ~3-6 MB。
func makeAvatar(r, g, b uint8) []byte {
	const size = 36
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{R: r, G: g, B: b, A: 255}
	fg := color.RGBA{R: 255 - r/2, G: 255 - g/2, B: 255 - b/2, A: 255}

	// 背景
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, bg)
		}
	}
	// 中央圆点
	cx, cy, rad := size/2, size/2, size/3
	r2 := rad * rad
	for y := cy - rad; y <= cy+rad; y++ {
		for x := cx - rad; x <= cx+rad; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				img.Set(x, y, fg)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
