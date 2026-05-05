package splitter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"

	"excel-master/internal/core"
)

// 拆分按列值：源文件有空 Sheet1 + 数据 Sheet（复刻用户真实问题——Excel 把 CSV
// 转成 xlsx 时留下的空 Sheet1）。期望拆分不因为空 Sheet 失败，正常按数据 Sheet 拆。
func TestSplitByColumn_SkipsEmptySheet(t *testing.T) {
	src := buildEmptySheetFixture(t)
	outDir := t.TempDir()

	task := core.SplitTask{
		SourcePath:  src,
		Mode:        core.SplitByColumn,
		SplitColumn: "类别",
		OutputDir:   outDir,
		HeaderRow:   1,
	}
	result, err := SplitByColumn(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByColumn 不该因空 Sheet 失败: %v", err)
	}
	// "类别" 列有 3 个不同值 → 3 个输出文件
	if result.PartsCreated != 3 {
		t.Errorf("PartsCreated=%d, want 3 (美妆/百货/文具)", result.PartsCreated)
	}
}

// inplace 路径同样：空 Sheet 跳过，只给数据 Sheet 写回新 Sheet。
func TestSplitByColumnInplace_SkipsEmptySheet(t *testing.T) {
	src := buildEmptySheetFixture(t)
	outDir := t.TempDir()

	task := core.SplitTask{
		SourcePath:   src,
		Mode:         core.SplitByColumn,
		SplitColumn:  "类别",
		OutputDir:    outDir,
		HeaderRow:    1,
		OutputTarget: core.OutputTargetInplaceSheets,
	}
	result, err := SplitByColumn(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("inplace 拆分不该因空 Sheet 失败: %v", err)
	}
	if result.PartsCreated != 3 {
		t.Errorf("PartsCreated=%d, want 3", result.PartsCreated)
	}
}

// 边界：源文件只有 1 个 Sheet 且它是空的 → 报错但不该是"找不到列"的误导信息，
// 而是上层的 COLUMN_NOT_FOUND（空 Sheet 被跳过后无 sheet 可处理）。
// 这是可接受的行为：用户知道 "没有可处理的数据"。
func TestSplitByColumn_OnlyEmptySheet_ReportsNoData(t *testing.T) {
	// 构造只有一个空 Sheet 的文件
	src := filepath.Join(t.TempDir(), "only_empty.xlsx")
	f := excelize.NewFile()
	// 不删 Sheet1，它就是空的；不加任何数据 Sheet
	if err := f.SaveAs(src); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	_ = f.Close()

	task := core.SplitTask{
		SourcePath:  src,
		Mode:        core.SplitByColumn,
		SplitColumn: "类别",
		OutputDir:   t.TempDir(),
		HeaderRow:   1,
	}
	_, err := SplitByColumn(context.Background(), task, nil)
	if err == nil {
		t.Fatal("只有空 Sheet 的文件应报错（无数据可拆），实际无错")
	}
	// 允许是 COLUMN_NOT_FOUND 或其他合理错误，但绝不该是 HEADER_ROW_MISSING
	var ae *core.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("期望 AppError: %v", err)
	}
	if ae.Code == "HEADER_ROW_MISSING" {
		t.Errorf("空 Sheet 不应产生 HEADER_ROW_MISSING 错误，应被视为无数据（%q）", ae.Code)
	}
}

// buildEmptySheetFixture 造一个带空 Sheet1 + "数据" Sheet 的 xlsx，
// 数据 Sheet 有"产品名/类别/价格"3 列和 5 行数据（3 种类别）。
func buildEmptySheetFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mixed.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	// 保留默认的空 Sheet1
	idx, err := f.NewSheet("数据")
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)

	headers := []string{"产品名", "类别", "价格"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue("数据", cell, h)
	}
	rows := [][]any{
		{"口红 A", "美妆", 99},
		{"眼影 B", "美妆", 50},
		{"水杯 C", "百货", 30},
		{"笔 D", "文具", 5},
		{"笔 E", "文具", 6},
	}
	for ri, r := range rows {
		for ci, v := range r {
			cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			_ = f.SetCellValue("数据", cell, v)
		}
	}

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}
