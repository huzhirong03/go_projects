package excelio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// makeTempXlsx 生成一个简单 xlsx，包含：
//   - Sheet "A"：表头 + 5 行数据 + 2 列
//   - Sheet "B"：只有表头
//   - A1 设红色填充（验证样式是否保留）
//   - A2 一个公式 =B2*10
//
// 返回 xlsx 路径，测试结束后调用方需自行清理 tmpDir（t.TempDir() 会处理）。
func makeTempXlsx(t *testing.T, dir string) string {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()

	if _, err := f.NewSheet("A"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.NewSheet("B"); err != nil {
		t.Fatal(err)
	}
	f.DeleteSheet("Sheet1")

	// 样式：红底白字
	styleID, err := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"FF0000"}, Pattern: 1},
		Font: &excelize.Font{Color: "FFFFFF", Bold: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sheet A 数据
	rows := [][]any{
		{"名称", "数量"},
		{"苹果", 10},
		{"香蕉", 20},
		{"橙子", 30},
		{"葡萄", 40},
		{"梨子", 50},
	}
	for i, r := range rows {
		for j, v := range r {
			cell, _ := excelize.CoordinatesToCellName(j+1, i+1)
			_ = f.SetCellValue("A", cell, v)
		}
	}
	_ = f.SetCellStyle("A", "A1", "B1", styleID)
	_ = f.SetCellFormula("A", "C2", "=B2*10")

	// Sheet B 只有表头
	_ = f.SetCellValue("B", "A1", "单列")

	path := filepath.Join(dir, "source.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCloneFile_Basic(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "copy.xlsx")

	if err := CloneFile(src, dst); err != nil {
		t.Fatal(err)
	}
	si, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if si.Size() != di.Size() {
		t.Fatalf("文件大小不一致: src=%d dst=%d", si.Size(), di.Size())
	}
}

func TestCloneFile_RejectsExisting(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "copy.xlsx")
	// 先创建 dst
	if err := os.WriteFile(dst, []byte("exists"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CloneFile(src, dst); err == nil {
		t.Fatal("期望返回 OUTPUT_CONFLICT，实际为 nil")
	}
}

func TestCloneFileAndFilterRows_KeepHeaderAndSomeData(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "filtered.xlsx")

	// 保留第 1 行（表头）+ 第 3 行（香蕉）+ 第 5 行（葡萄）
	if err := CloneFileAndFilterRows(src, dst, "A", []int{1, 3, 5}); err != nil {
		t.Fatal(err)
	}

	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	rows, err := f.GetRows("A")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("期望 3 行，实际 %d 行: %v", len(rows), rows)
	}
	// 表头
	if rows[0][0] != "名称" || rows[0][1] != "数量" {
		t.Fatalf("表头被破坏: %v", rows[0])
	}
	// 原第 3 行
	if rows[1][0] != "香蕉" || rows[1][1] != "20" {
		t.Fatalf("第 2 行数据错: %v", rows[1])
	}
	// 原第 5 行
	if rows[2][0] != "葡萄" || rows[2][1] != "40" {
		t.Fatalf("第 3 行数据错: %v", rows[2])
	}

	// 验证样式没丢（表头 A1 仍有红底）
	styleID, err := f.GetCellStyle("A", "A1")
	if err != nil {
		t.Fatal(err)
	}
	if styleID == 0 {
		t.Fatalf("A1 样式丢失")
	}
}

func TestCloneFileAndFilterRows_KeepAllRows(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "all.xlsx")

	if err := CloneFileAndFilterRows(src, dst, "A", []int{1, 2, 3, 4, 5, 6}); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, _ := f.GetRows("A")
	if len(rows) != 6 {
		t.Fatalf("期望 6 行全保留，实际 %d 行", len(rows))
	}
}

func TestCloneFileAndFilterRows_KeepOnlyHeader(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "head.xlsx")

	if err := CloneFileAndFilterRows(src, dst, "A", []int{1}); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, _ := f.GetRows("A")
	if len(rows) != 1 {
		t.Fatalf("期望 1 行，实际 %d 行: %v", len(rows), rows)
	}
}

func TestKeepSheetsOnly_KeepsSpecified(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "keep_a.xlsx")

	if err := CloneFile(src, dst); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if err := KeepSheetsOnly(f, []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	f2, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	sheets := f2.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "A" {
		t.Fatalf("期望只剩 Sheet A，实际: %v", sheets)
	}
}

func TestKeepSheetsOnly_RejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	f, err := excelize.OpenFile(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := KeepSheetsOnly(f, nil); err == nil {
		t.Fatal("期望报错，实际 nil")
	}
	if err := KeepSheetsOnly(f, []string{"不存在"}); err == nil {
		t.Fatal("期望报错，实际 nil")
	}
}

// TestCloneFileAndFilterRows_PicturesFollowRows 回归测试：
// 用户 2026-05 反馈"补货建议图标不在正确的行"—— 根因是 excelize RemoveRow 不删除被删行的图片，
// 图片会全部堆积到上一保留行。FilterRowsInSheet 现在会在 RemoveRow 前显式 DeletePicture。
func TestCloneFileAndFilterRows_PicturesFollowRows(t *testing.T) {
	dir := t.TempDir()
	// 造一个每行一张图的源文件：A1=header, A2..A6 各锚一张 1x1 PNG
	src := filepath.Join(dir, "src_pics.xlsx")
	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "名称")
	for i := 2; i <= 6; i++ {
		cell, _ := excelize.CoordinatesToCellName(1, i)
		_ = f.SetCellValue("Sheet1", cell, "row"+string(rune('0'+i)))
		// 生成一张 1x1 的 PNG 图片字节
		if err := f.AddPictureFromBytes("Sheet1", cell, &excelize.Picture{
			Extension: ".png",
			File:      onePixelPNG(),
			Format:    &excelize.GraphicOptions{AutoFit: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SaveAs(src); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// 只保留 header + 第 3 行和第 5 行（丢掉 2,4,6 行 = 3 张图要删除）
	dst := filepath.Join(dir, "dst_pics.xlsx")
	if err := CloneFileAndFilterRows(src, dst, "Sheet1", []int{1, 3, 5}); err != nil {
		t.Fatal(err)
	}

	f2, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	// 期望：3 行（A1 header + A2 原第3行 + A3 原第5行），2 张图（原第3行 + 原第5行的）
	rows, _ := f2.GetRows("Sheet1")
	if len(rows) != 3 {
		t.Fatalf("期望 3 行，实际 %d 行: %v", len(rows), rows)
	}
	cells, err := f2.GetPictureCells("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 2 {
		t.Fatalf("期望 2 张图（每行一张，无堆叠），实际 %d 张 @ cells=%v", len(cells), cells)
	}
	// 图片应该锚在 A2 和 A3（原第3/5行被上移）
	expectCells := map[string]bool{"A2": false, "A3": false}
	for _, c := range cells {
		if _, ok := expectCells[c]; !ok {
			t.Fatalf("意外的图片锚点: %s（期望 A2/A3）", c)
		}
		expectCells[c] = true
	}
	for c, seen := range expectCells {
		if !seen {
			t.Fatalf("期望 %s 有图片，但没有", c)
		}
	}
}

// onePixelPNG 返回一个合法的 1x1 透明 PNG 字节串（硬编码，避免依赖 image 编码）。
func onePixelPNG() []byte {
	// 89504e47 0d0a1a0a | IHDR 1x1 8bit RGBA | IDAT deflate{1 px} | IEND
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
}

func TestSortedUnique(t *testing.T) {
	cases := []struct {
		in   []int
		want []int
	}{
		{nil, nil},
		{[]int{3, 1, 2}, []int{1, 2, 3}},
		{[]int{1, 1, 2, 2, 3}, []int{1, 2, 3}},
		{[]int{5, 5, 5}, []int{5}},
	}
	for _, c := range cases {
		got := SortedUnique(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("SortedUnique(%v) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("SortedUnique(%v) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}
