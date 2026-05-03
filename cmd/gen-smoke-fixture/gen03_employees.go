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

// makeAvatar 生成一张 64x64 的"默认头像"风格图：
//   - 柔和单色圆形背景（参数 r/g/b 控制色调）
//   - 中央白色头部剪影（圆形头 + 梯形肩膀）
//
// 看着像 GitHub / Slack / 钉钉的默认头像，比单色块自然得多。
//
// 体积控制在 ~300-500B，2 万张总共约 6-10 MB。
func makeAvatar(r, g, b uint8) []byte {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 1) 圆形背景（柔和单色，外圈淡边）
	bg := softenColor(color.RGBA{R: r, G: g, B: b, A: 255})
	bgEdge := darkenColor(bg, 0.1)
	transparent := color.RGBA{R: 245, G: 247, B: 250, A: 255} // 圆外用浅灰，看着像卡片背景
	cx, cy := size/2, size/2
	bgRad := size / 2 // 几乎填满
	bgEdgeRad := bgRad - 1
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-cx, y-cy
			d2 := dx*dx + dy*dy
			switch {
			case d2 < bgEdgeRad*bgEdgeRad:
				img.Set(x, y, bg)
			case d2 < bgRad*bgRad:
				img.Set(x, y, bgEdge)
			default:
				img.Set(x, y, transparent)
			}
		}
	}

	// 2) 头部（白色实心圆，居中偏上）
	fg := color.RGBA{R: 255, G: 255, B: 255, A: 235}
	headCX, headCY, headR := cx, cy-size/8, size/5
	headR2 := headR * headR
	for y := headCY - headR; y <= headCY+headR; y++ {
		for x := headCX - headR; x <= headCX+headR; x++ {
			dx, dy := x-headCX, y-headCY
			if dx*dx+dy*dy <= headR2 {
				img.Set(x, y, fg)
			}
		}
	}

	// 3) 肩膀（白色梯形：下宽上窄，半圆收口在底部）
	shoulderTopY := headCY + headR + 2
	shoulderBotY := size - 4
	for y := shoulderTopY; y < shoulderBotY; y++ {
		// 梯形左右宽度按 y 线性插值
		t := float64(y-shoulderTopY) / float64(shoulderBotY-shoulderTopY)
		halfW := int(float64(size/8) + t*float64(size/3))
		// 圆角处理：在最底部把宽度收一点
		if y > shoulderBotY-3 {
			halfW = halfW - (y - (shoulderBotY - 3))
		}
		x0 := cx - halfW
		x1 := cx + halfW
		if x0 < 1 {
			x0 = 1
		}
		if x1 > size-1 {
			x1 = size - 1
		}
		for x := x0; x < x1; x++ {
			// 还要在背景圆内
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy < bgEdgeRad*bgEdgeRad {
				img.Set(x, y, fg)
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// softenColor 把过于鲜艳的颜色调和向白色靠拢，让头像背景更柔和不刺眼。
func softenColor(c color.RGBA) color.RGBA {
	const t = 0.25 // 25% 向白色混
	return color.RGBA{
		R: uint8(float64(c.R)*(1-t) + 255*t),
		G: uint8(float64(c.G)*(1-t) + 255*t),
		B: uint8(float64(c.B)*(1-t) + 255*t),
		A: 255,
	}
}

// darkenColor 把颜色向黑色靠拢一点，用于圆形外圈。
func darkenColor(c color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(c.R) * (1 - t)),
		G: uint8(float64(c.G) * (1 - t)),
		B: uint8(float64(c.B) * (1 - t)),
		A: 255,
	}
}
