package excelio

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestExtractToNewFileSurgery_Basic 验证最基础的"行子集 + 图片迁移"路径：
//   - 源 xlsx 含 4 行数据 + 2 张图（A2、A4）
//   - 筛选保留 header + A4 行
//   - 新文件应只有 1 个 sheet 叫"搜索_hit"
//   - 新 sheet 有 2 行（header + A4 内容 → 变成 header + A2）
//   - 新 sheet 有 1 张图锚点在 A2（原 A4 的图迁移到新 row 2）
func TestExtractToNewFileSurgery_Basic(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.xlsx")

	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "名称")
	_ = f.SetCellValue("Sheet1", "A2", "row2")
	_ = f.SetCellValue("Sheet1", "A3", "row3")
	_ = f.SetCellValue("Sheet1", "A4", "row4")
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

	dst := filepath.Join(dir, "dst.xlsx")
	specs := []InplaceSheetSpec{
		{SourceSheet: "Sheet1", NewSheetName: "搜索_hit", KeepRows: []int{1, 4}},
	}
	if err := ExtractToNewFileSurgery(src, dst, specs); err != nil {
		t.Fatalf("ExtractToNewFileSurgery: %v", err)
	}

	// 用 excelize 打开校验（excelize 能打开 = 文件格式合法）
	f2, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatalf("打开新文件失败: %v", err)
	}
	defer f2.Close()

	// 1. 只有 1 个 sheet，名叫"搜索_hit"
	sheets := f2.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "搜索_hit" {
		t.Fatalf("期望单个 sheet=搜索_hit，实际 %v", sheets)
	}

	// 2. 新 sheet 有 2 行：header + row4
	rows, err := f2.GetRows("搜索_hit")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("期望 2 行，实际 %d: %v", len(rows), rows)
	}
	if rows[0][0] != "名称" || rows[1][0] != "row4" {
		t.Fatalf("内容错: %v", rows)
	}

	// 3. 图片：新 sheet 只有 1 张（原 A4 的图迁到新 row 2 = A2）
	cells, err := f2.GetPictureCells("搜索_hit")
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 1 || cells[0] != "A2" {
		t.Fatalf("期望图片锚点 A2（原 A4 的图），实际 %v", cells)
	}
}

// TestExtractToNewFileSurgery_MultipleSheets 验证多个 specs → 新文件多个 sheet。
func TestExtractToNewFileSurgery_MultipleSheets(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.xlsx")

	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "H")
	for i := 2; i <= 10; i++ {
		cell, _ := excelize.CoordinatesToCellName(1, i)
		_ = f.SetCellValue("Sheet1", cell, "r"+string(rune('0'+i-2)))
	}
	if err := f.SaveAs(src); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	dst := filepath.Join(dir, "dst.xlsx")
	specs := []InplaceSheetSpec{
		{SourceSheet: "Sheet1", NewSheetName: "kw_A", KeepRows: []int{1, 2, 4}},
		{SourceSheet: "Sheet1", NewSheetName: "kw_B", KeepRows: []int{1, 3, 5, 7}},
	}
	if err := ExtractToNewFileSurgery(src, dst, specs); err != nil {
		t.Fatalf("ExtractToNewFileSurgery: %v", err)
	}

	f2, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatalf("打开新文件失败: %v", err)
	}
	defer f2.Close()

	sheets := f2.GetSheetList()
	got := map[string]bool{}
	for _, s := range sheets {
		got[s] = true
	}
	if !got["kw_A"] || !got["kw_B"] || len(sheets) != 2 {
		t.Fatalf("期望 sheet = [kw_A, kw_B]，实际 %v", sheets)
	}

	rowsA, _ := f2.GetRows("kw_A")
	if len(rowsA) != 3 {
		t.Fatalf("kw_A 期望 3 行，实际 %d", len(rowsA))
	}
	rowsB, _ := f2.GetRows("kw_B")
	if len(rowsB) != 4 {
		t.Fatalf("kw_B 期望 4 行，实际 %d", len(rowsB))
	}
}

// TestExtractToNewFileSurgery_NoOldEntries 验证新文件里**确实没有**旧 sheet / drawing。
// 这是"new file"语义的核心保证。
func TestExtractToNewFileSurgery_NoOldEntries(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.xlsx")

	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "H")
	_ = f.SetCellValue("Sheet1", "A2", "kept")
	_ = f.SetCellValue("Sheet1", "A3", "dropped")
	// 新建一个额外 sheet（Sheet2）纯为了测试：新文件不应保留 Sheet2
	_, _ = f.NewSheet("Sheet2")
	_ = f.SetCellValue("Sheet2", "A1", "should be dropped")
	if err := f.AddPictureFromBytes("Sheet1", "A2", &excelize.Picture{
		Extension: ".png", File: onePixelPNG(),
		Format: &excelize.GraphicOptions{AutoFit: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveAs(src); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	dst := filepath.Join(dir, "dst.xlsx")
	specs := []InplaceSheetSpec{
		{SourceSheet: "Sheet1", NewSheetName: "only_hit", KeepRows: []int{1, 2}},
	}
	if err := ExtractToNewFileSurgery(src, dst, specs); err != nil {
		t.Fatalf("ExtractToNewFileSurgery: %v", err)
	}

	// 直接看 zip 内部条目
	r, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	entries := map[string]bool{}
	for _, e := range r.File {
		entries[e.Name] = true
	}

	// 必须有：styles / theme / workbook
	mustHave := []string{"xl/workbook.xml", "[Content_Types].xml", "xl/_rels/workbook.xml.rels"}
	for _, n := range mustHave {
		if !entries[n] {
			t.Fatalf("缺必需条目: %s", n)
		}
	}

	// 必须没有任何旧 sheet xml（源 Sheet1 / Sheet2 被丢弃，新 sheet 会有自己的 sheetN.xml）
	// 但新 sheet 也叫 xl/worksheets/sheetN.xml，所以要检查是不是同名覆盖
	// 简化：至少**数量**上，worksheets 应该恰好 1 个（只有新 sheet）
	wsCount := 0
	for n := range entries {
		if strings.HasPrefix(n, "xl/worksheets/") && strings.HasSuffix(n, ".xml") {
			wsCount++
		}
	}
	if wsCount != 1 {
		t.Fatalf("期望 worksheets 下只有 1 个 xml（新 sheet），实际 %d: %v", wsCount, entries)
	}

	// calcChain 必须被删除
	if entries["xl/calcChain.xml"] {
		t.Fatalf("calcChain.xml 应该被删除（公式链指向旧 sheet 已失效）")
	}

	// media 必须保留（新 sheet 的 drawing rels 指向它）
	hasMedia := false
	for n := range entries {
		if strings.HasPrefix(n, "xl/media/") {
			hasMedia = true
			break
		}
	}
	if !hasMedia {
		t.Fatalf("期望保留 xl/media/*，实际未找到")
	}

	// 打开验证读取正常
	f2, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatalf("打开新文件失败: %v", err)
	}
	defer f2.Close()
	if got := f2.GetSheetList(); len(got) != 1 || got[0] != "only_hit" {
		t.Fatalf("期望单 sheet=only_hit，实际 %v", got)
	}
}

// TestExtractToNewFileSurgery_NilKeepRowsFull 当 KeepRows=nil 时应全行保留，
// 等价于"原封不动克隆 sheet 到新文件"。
func TestExtractToNewFileSurgery_NilKeepRowsFull(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.xlsx")

	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "H")
	for i := 2; i <= 5; i++ {
		cell, _ := excelize.CoordinatesToCellName(1, i)
		_ = f.SetCellValue("Sheet1", cell, "row"+string(rune('0'+i-1)))
	}
	if err := f.SaveAs(src); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	dst := filepath.Join(dir, "dst.xlsx")
	specs := []InplaceSheetSpec{
		{SourceSheet: "Sheet1", NewSheetName: "clone", KeepRows: nil},
	}
	if err := ExtractToNewFileSurgery(src, dst, specs); err != nil {
		t.Fatalf("ExtractToNewFileSurgery: %v", err)
	}

	f2, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatalf("打开新文件失败: %v", err)
	}
	defer f2.Close()

	rows, _ := f2.GetRows("clone")
	if len(rows) != 5 {
		t.Fatalf("期望 5 行，实际 %d: %v", len(rows), rows)
	}
}

// TestExtractToNewFileSurgery_Errors 验证错误情况：
//   - empty specs
//   - dst 已存在
//   - 源 sheet 不存在
func TestExtractToNewFileSurgery_Errors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.xlsx")
	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "H")
	if err := f.SaveAs(src); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	t.Run("empty specs", func(t *testing.T) {
		dst := filepath.Join(dir, "dst_empty.xlsx")
		err := ExtractToNewFileSurgery(src, dst, nil)
		if err == nil {
			t.Fatal("期望报错 empty specs")
		}
	})

	t.Run("dst already exists", func(t *testing.T) {
		dst := filepath.Join(dir, "dst_exist.xlsx")
		_ = os.WriteFile(dst, []byte("existing"), 0o644)
		specs := []InplaceSheetSpec{
			{SourceSheet: "Sheet1", NewSheetName: "n", KeepRows: []int{1}},
		}
		err := ExtractToNewFileSurgery(src, dst, specs)
		if err == nil {
			t.Fatal("期望报 OUTPUT_CONFLICT")
		}
	})

	t.Run("source sheet not found", func(t *testing.T) {
		dst := filepath.Join(dir, "dst_nf.xlsx")
		specs := []InplaceSheetSpec{
			{SourceSheet: "NonExistent", NewSheetName: "n", KeepRows: []int{1}},
		}
		err := ExtractToNewFileSurgery(src, dst, specs)
		if err == nil {
			t.Fatal("期望报 SHEET_NOT_FOUND")
		}
	})
}
