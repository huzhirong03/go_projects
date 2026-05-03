package splitter

import (
	"context"
	"os"
	"testing"

	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"
)

// TestSplitByRowsInplace: 单 Sheet 5 行 + RowsPerFile=2 → 3 个新 Sheet (part001..003)。
//   - 原 Sheet1 不变
//   - 库存 Sheet 2 行 + RowsPerFile=2 → 1 个新 Sheet
func TestSplitByRowsInplace(t *testing.T) {
	src := buildFixture(t)
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByRows, RowsPerFile: 2,
		HeaderRow:    1,
		OutputTarget: core.OutputTargetInplaceSheets,
	}
	res, err := SplitByRows(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByRows inplace: %v", err)
	}
	// Sheet1 5 行 → 3 part；库存 2 行 → 1 part；合计 4 个新 Sheet
	if res.PartsCreated != 4 {
		t.Fatalf("PartsCreated 应为 4，实际 %d", res.PartsCreated)
	}
	if len(res.OutputFiles) != 1 || res.OutputFiles[0] != src {
		t.Fatalf("OutputFiles 应为 [源文件]，实际 %v", res.OutputFiles)
	}

	f, err := excelize.OpenFile(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := map[string]bool{}
	for _, s := range f.GetSheetList() {
		got[s] = true
	}
	// 多 Sheet 时新 Sheet 名带源 Sheet 后缀
	for _, name := range []string{"Sheet1", "库存", "拆_part001_Sheet1", "拆_part002_Sheet1", "拆_part003_Sheet1", "拆_part001_库存"} {
		if !got[name] {
			t.Errorf("期望存在 Sheet %q，实际 sheets=%v", name, f.GetSheetList())
		}
	}

	// 原 Sheet1 仍 6 行（含表头）
	rows1, _ := f.GetRows("Sheet1")
	if len(rows1) != 6 {
		t.Errorf("原 Sheet1 应 6 行，实际 %d", len(rows1))
	}
	// 拆_part001_Sheet1 应 3 行（表头 + 2 数据行）
	rowsP1, _ := f.GetRows("拆_part001_Sheet1")
	if len(rowsP1) != 3 {
		t.Errorf("拆_part001_Sheet1 应 3 行，实际 %d: %v", len(rowsP1), rowsP1)
	}
}

// TestSplitByColumnInplace: Sheet1 类别 = 美妆/百货/文具 → 3 个新 Sheet。
func TestSplitByColumnInplace(t *testing.T) {
	src := buildFixture(t)
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByColumn,
		SplitColumn:  "类别",
		HeaderRow:    1,
		OutputTarget: core.OutputTargetInplaceSheets,
		SheetNames:   []string{"Sheet1"}, // 库存 Sheet 没有"类别"列，避开警告
	}
	res, err := SplitByColumn(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByColumn inplace: %v", err)
	}
	if res.PartsCreated != 3 {
		t.Fatalf("PartsCreated 应为 3，实际 %d", res.PartsCreated)
	}
	f, err := excelize.OpenFile(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := map[string]bool{}
	for _, s := range f.GetSheetList() {
		got[s] = true
	}
	for _, name := range []string{"Sheet1", "拆_美妆", "拆_百货", "拆_文具"} {
		if !got[name] {
			t.Errorf("期望存在 Sheet %q，实际 %v", name, f.GetSheetList())
		}
	}
	rowsMz, _ := f.GetRows("拆_美妆")
	if len(rowsMz) != 3 { // 表头 + 口红A + 眼影B
		t.Errorf("拆_美妆 应 3 行，实际 %d", len(rowsMz))
	}
}

// TestSplitBySheetInplaceRejected: by_sheet 在 inplace 下友好拒绝。
func TestSplitBySheetInplaceRejected(t *testing.T) {
	src := buildFixture(t)
	task := core.SplitTask{
		SourcePath:   src,
		Mode:         core.SplitBySheet,
		HeaderRow:    1,
		OutputTarget: core.OutputTargetInplaceSheets,
	}
	_, err := SplitBySheet(context.Background(), task, nil)
	if err == nil {
		t.Fatal("期望 by_sheet inplace 报错")
	}
}

// TestSplitByRowsInplaceBackup: BackupSource=true 时生成 .bak。
func TestSplitByRowsInplaceBackup(t *testing.T) {
	src := buildFixture(t)
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByRows, RowsPerFile: 2,
		HeaderRow:    1,
		OutputTarget: core.OutputTargetInplaceSheets,
		BackupSource: true,
		SheetNames:   []string{"Sheet1"},
	}
	if _, err := SplitByRows(context.Background(), task, nil); err != nil {
		t.Fatalf("SplitByRows inplace: %v", err)
	}
	if _, err := os.Stat(src + ".bak"); err != nil {
		t.Fatalf("期望生成 %s.bak: %v", src, err)
	}
}
