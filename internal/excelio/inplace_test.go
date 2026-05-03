package excelio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestCopySheetWithin_Basic 验证 CopySheetWithin 能：
//  1. 把源 Sheet 的数据复制到新 Sheet
//  2. 图片也复制过去，锚点相同
//  3. 原 Sheet 不受影响
func TestCopySheetWithin_Basic(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.xlsx")

	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "名称")
	_ = f.SetCellValue("Sheet1", "A2", "row2")
	_ = f.SetCellValue("Sheet1", "A3", "row3")
	_ = f.SetCellValue("Sheet1", "A4", "row4")
	// 在 A2 和 A4 各加一张 1x1 PNG
	for _, cell := range []string{"A2", "A4"} {
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

	// 打开 -> 复制 Sheet1 到 Sheet1_copy
	f2, err := excelize.OpenFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := CopySheetWithin(f2, "Sheet1", "Sheet1_copy"); err != nil {
		_ = f2.Close()
		t.Fatalf("CopySheetWithin: %v", err)
	}
	dst := filepath.Join(dir, "dst.xlsx")
	if err := f2.SaveAs(dst); err != nil {
		t.Fatal(err)
	}
	_ = f2.Close()

	// 重新打开校验
	f3, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f3.Close()

	// 1. 两个 Sheet 都在
	sheets := f3.GetSheetList()
	got := map[string]bool{}
	for _, s := range sheets {
		got[s] = true
	}
	if !got["Sheet1"] || !got["Sheet1_copy"] {
		t.Fatalf("期望 Sheet1 + Sheet1_copy 都存在，实际 %v", sheets)
	}

	// 2. 新 Sheet 的数据和原 Sheet 一致
	for _, sh := range []string{"Sheet1", "Sheet1_copy"} {
		rows, _ := f3.GetRows(sh)
		if len(rows) != 4 {
			t.Fatalf("%s 期望 4 行，实际 %d: %v", sh, len(rows), rows)
		}
	}

	// 3. 新 Sheet 的图片数量和原 Sheet 一致（各 2 张）
	for _, sh := range []string{"Sheet1", "Sheet1_copy"} {
		cells, err := f3.GetPictureCells(sh)
		if err != nil {
			t.Fatalf("%s GetPictureCells: %v", sh, err)
		}
		if len(cells) != 2 {
			t.Fatalf("%s 期望 2 张图，实际 %d: %v", sh, len(cells), cells)
		}
		wantCells := map[string]bool{"A2": false, "A4": false}
		for _, c := range cells {
			if _, ok := wantCells[c]; !ok {
				t.Fatalf("%s 意外图片锚点: %s", sh, c)
			}
			wantCells[c] = true
		}
		for c, ok := range wantCells {
			if !ok {
				t.Fatalf("%s 期望 %s 有图片，但没找到", sh, c)
			}
		}
	}
}

// TestCopySheetWithin_ThenFilterRows 验证核心业务链：
// CopySheetWithin 之后，在新 Sheet 上调 FilterRowsInSheet，
// 结果应该是：原 Sheet 不变；新 Sheet 只保留命中行，图片随行上移。
func TestCopySheetWithin_ThenFilterRows(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.xlsx")

	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "名称")
	for i := 2; i <= 6; i++ {
		cell, _ := excelize.CoordinatesToCellName(1, i)
		_ = f.SetCellValue("Sheet1", cell, "row"+string(rune('0'+i)))
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

	// 打开 -> 复制到"搜索_口红" -> 在新 Sheet 上保留 header+第3行+第5行
	f2, err := excelize.OpenFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := CopySheetWithin(f2, "Sheet1", "搜索_口红"); err != nil {
		_ = f2.Close()
		t.Fatalf("CopySheetWithin: %v", err)
	}
	if err := FilterRowsInSheet(f2, "搜索_口红", []int{1, 3, 5}); err != nil {
		_ = f2.Close()
		t.Fatalf("FilterRowsInSheet: %v", err)
	}
	dst := filepath.Join(dir, "dst.xlsx")
	if err := f2.SaveAs(dst); err != nil {
		t.Fatal(err)
	}
	_ = f2.Close()

	f3, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f3.Close()

	// 原 Sheet 不变：6 行 5 图
	rows1, _ := f3.GetRows("Sheet1")
	if len(rows1) != 6 {
		t.Fatalf("原 Sheet1 应有 6 行，实际 %d", len(rows1))
	}
	cells1, _ := f3.GetPictureCells("Sheet1")
	if len(cells1) != 5 {
		t.Fatalf("原 Sheet1 应有 5 张图，实际 %d @ %v", len(cells1), cells1)
	}

	// 新 Sheet：3 行 (header+第3行+第5行) 2 图，图锚应在 A2 A3
	rows2, _ := f3.GetRows("搜索_口红")
	if len(rows2) != 3 {
		t.Fatalf("新 Sheet 应有 3 行，实际 %d: %v", len(rows2), rows2)
	}
	cells2, _ := f3.GetPictureCells("搜索_口红")
	if len(cells2) != 2 {
		t.Fatalf("新 Sheet 应有 2 张图，实际 %d @ %v", len(cells2), cells2)
	}
	want := map[string]bool{"A2": false, "A3": false}
	for _, c := range cells2 {
		if _, ok := want[c]; !ok {
			t.Fatalf("新 Sheet 意外图片锚点: %s", c)
		}
		want[c] = true
	}
	for c, ok := range want {
		if !ok {
			t.Fatalf("新 Sheet 期望 %s 有图片，但没有", c)
		}
	}
}

func TestUniqueSheetName_NoConflict(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	got := UniqueSheetName(f, "Sheet2")
	if got != "Sheet2" {
		t.Fatalf("期望 Sheet2，实际 %q", got)
	}
}

func TestUniqueSheetName_Conflict(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	_, _ = f.NewSheet("搜索_口红")
	_, _ = f.NewSheet("搜索_口红_2")
	got := UniqueSheetName(f, "搜索_口红")
	if got != "搜索_口红_3" {
		t.Fatalf("期望 搜索_口红_3，实际 %q", got)
	}
}

func TestSanitizeSheetName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"搜索_口红", "搜索_口红"},
		{"a/b", "a_b"},
		{`a\b?c*d[e]f:g`, "a_b_c_d_e_f_g"},
		{"'quoted'", "quoted"},
		{"", "Sheet"},
	}
	for _, c := range cases {
		got := SanitizeSheetName(c.in)
		if got != c.want {
			t.Errorf("SanitizeSheetName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 超长：31 字符截断
	long := "搜索_口红搜索_口红搜索_口红搜索_口红搜索_口红搜索_口红搜索_口红搜索_口红"
	got := SanitizeSheetName(long)
	if len([]rune(got)) > 31 {
		t.Errorf("期望 ≤31 字符，实际 %d: %q", len([]rune(got)), got)
	}
}

func TestAtomicReplace_Success(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	tmp := filepath.Join(dir, "tmp.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicReplace(target, tmp); err != nil {
		t.Fatalf("AtomicReplace: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("target 应为 new，实际 %q", got)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("tmp 应被移走")
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf("target.old 应被清理")
	}
}

func TestAtomicReplace_TmpMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	_ = os.WriteFile(target, []byte("old"), 0o644)
	err := AtomicReplace(target, filepath.Join(dir, "nope.txt"))
	if err == nil {
		t.Fatal("期望 tmp 不存在时报错")
	}
	// target 应保持原样
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("target 应仍为 old，实际 %q", got)
	}
}

func TestBackupCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	bak, err := BackupCopy(src)
	if err != nil {
		t.Fatal(err)
	}
	if bak != src+".bak" {
		t.Fatalf("期望 %s.bak，实际 %s", src, bak)
	}
	got, _ := os.ReadFile(bak)
	if string(got) != "hello" {
		t.Fatalf("备份内容错误: %q", got)
	}
	// 覆盖：第二次调用成功
	if _, err := BackupCopy(src); err != nil {
		t.Fatalf("第二次 BackupCopy 应覆盖: %v", err)
	}
}
