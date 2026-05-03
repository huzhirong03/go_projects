package excelio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// mkSubDir t.TempDir() 下创建子目录（makeTempXlsx 需要一个已存在的 dir）。
func mkSubDir(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// makeTempXlsx 已在 clone_test.go 定义：A 表 6 行 + B 表 1 行。
// 这里再做一份"基本兼容"的样本（不带图片），验证 merge 的最简路径。

// 验证：primary + 1 个 secondary 都贡献 1 行数据，输出文件的 Sheet A 应有：
//   - 表头（来自 primary）
//   - primary 的 row 3（"香蕉",20）  → 输出 row 2
//   - secondary 的 row 5（"葡萄",40）→ 输出 row 3（追加，inline string + 模板列样式）
func TestCloneAndMergePreserved_BasicTwoSources(t *testing.T) {
	dir := t.TempDir()

	// 两份完全相同的源文件，都有 A 表 6 行
	src1 := makeTempXlsx(t, mkSubDir(t, dir, "src1"))
	src2 := makeTempXlsx(t, mkSubDir(t, dir, "src2"))
	dst := filepath.Join(dir, "merged.xlsx")

	primary := MergeSource{
		SrcPath:   src1,
		SheetName: "A",
		KeepRows:  []int{1, 3}, // 表头 + 第 3 行（"香蕉",20）
	}
	secondary := MergeSource{
		SrcPath:   src2,
		SheetName: "A",
		KeepRows:  []int{5}, // 第 5 行（"葡萄",40），不含表头
	}

	if err := CloneAndMergePreserved(primary, dst, []MergeSource{secondary}); err != nil {
		t.Fatalf("CloneAndMergePreserved 失败: %v", err)
	}

	// 用 excelize 验证输出
	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatalf("打开输出失败: %v", err)
	}
	defer f.Close()

	// 应该只剩 A 表
	sheets := f.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "A" {
		t.Fatalf("应只保留 A 表，实际 %v", sheets)
	}

	rows, err := f.GetRows("A")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	// 期望 3 行：表头、香蕉 20、葡萄 40
	if len(rows) != 3 {
		t.Fatalf("期望 3 行，实际 %d 行: %v", len(rows), rows)
	}
	if rows[0][0] != "名称" || rows[0][1] != "数量" {
		t.Fatalf("表头错: %v", rows[0])
	}
	if rows[1][0] != "香蕉" || rows[1][1] != "20" {
		t.Fatalf("primary 行错: %v", rows[1])
	}
	if rows[2][0] != "葡萄" || rows[2][1] != "40" {
		t.Fatalf("secondary 行错: %v", rows[2])
	}

	// 表头样式应保留（红底白字 → 检查 styleID 不为 0）
	styleID, err := f.GetCellStyle("A", "A1")
	if err != nil || styleID == 0 {
		t.Fatalf("表头样式丢失: styleID=%d err=%v", styleID, err)
	}
}

// 验证：没有 secondaries 时退化为 CloneAndExtractZipMulti 的等价
func TestCloneAndMergePreserved_NoSecondaries(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "only_primary.xlsx")

	err := CloneAndMergePreserved(MergeSource{
		SrcPath: src, SheetName: "A", KeepRows: []int{1, 3, 5},
	}, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, _ := f.GetRows("A")
	if len(rows) != 3 {
		t.Fatalf("应 3 行，实际 %d", len(rows))
	}
}

// 验证：两个 secondary 各贡献多行，输出按追加顺序排列
func TestCloneAndMergePreserved_MultiSecondaries(t *testing.T) {
	dir := t.TempDir()
	src1 := makeTempXlsx(t, mkSubDir(t, dir, "p"))
	src2 := makeTempXlsx(t, mkSubDir(t, dir, "s2"))
	src3 := makeTempXlsx(t, mkSubDir(t, dir, "s3"))
	dst := filepath.Join(dir, "merged_multi.xlsx")

	err := CloneAndMergePreserved(
		MergeSource{SrcPath: src1, SheetName: "A", KeepRows: []int{1, 2}}, // 表头 + 苹果 10
		dst,
		[]MergeSource{
			{SrcPath: src2, SheetName: "A", KeepRows: []int{4}}, // 橙子 30
			{SrcPath: src3, SheetName: "A", KeepRows: []int{6}}, // 梨子 50
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, _ := f.GetRows("A")
	// 表头 + 苹果 + 橙子 + 梨子 = 4 行
	if len(rows) != 4 {
		t.Fatalf("应 4 行，实际 %d: %v", len(rows), rows)
	}
	want := [][2]string{
		{"名称", "数量"},
		{"苹果", "10"},
		{"橙子", "30"},
		{"梨子", "50"},
	}
	for i, w := range want {
		if rows[i][0] != w[0] || rows[i][1] != w[1] {
			t.Errorf("行 %d 错: got %v, want %v", i+1, rows[i], w)
		}
	}
}

func TestCloneAndMergePreserved_SecondaryFormula(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "primary.xlsx")
	src2 := filepath.Join(dir, "secondary.xlsx")
	dst := filepath.Join(dir, "merged_formula.xlsx")

	makeFormulaXlsx := func(path string) {
		f := excelize.NewFile()
		defer f.Close()
		_ = f.SetSheetName("Sheet1", "A")
		_ = f.SetCellValue("A", "A1", "名称")
		_ = f.SetCellValue("A", "B1", "数量")
		_ = f.SetCellValue("A", "C1", "金额")
		_ = f.SetCellValue("A", "A2", "苹果")
		_ = f.SetCellValue("A", "B2", 10)
		_ = f.SetCellFormula("A", "C2", "=B2*10")
		_ = f.SetCellValue("A", "A5", "葡萄")
		_ = f.SetCellValue("A", "B5", 40)
		_ = f.SetCellFormula("A", "C5", "=B5*10")
		if err := f.SaveAs(path); err != nil {
			t.Fatal(err)
		}
	}
	makeFormulaXlsx(src1)
	makeFormulaXlsx(src2)

	if err := CloneAndMergePreserved(
		MergeSource{SrcPath: src1, SheetName: "A", KeepRows: []int{1, 2}},
		dst,
		[]MergeSource{{SrcPath: src2, SheetName: "A", KeepRows: []int{5}}},
	); err != nil {
		t.Fatal(err)
	}

	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	formula, err := f.GetCellFormula("A", "C3")
	if err != nil {
		t.Fatal(err)
	}
	if formula != "=B3*10" {
		t.Fatalf("secondary 公式未正确偏移: got %q", formula)
	}
}

// 错误路径：sheet 名不一致
func TestCloneAndMergePreserved_SheetNameMismatch(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	err := CloneAndMergePreserved(
		MergeSource{SrcPath: src, SheetName: "A", KeepRows: []int{1}},
		filepath.Join(dir, "x.xlsx"),
		[]MergeSource{{SrcPath: src, SheetName: "B", KeepRows: []int{1}}}, // 故意不同
	)
	if err == nil {
		t.Fatal("期望 SheetName 不匹配错误")
	}
}
