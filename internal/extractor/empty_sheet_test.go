package extractor

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// probeFile 遇到带空 Sheet 的 xlsx（典型：Excel 把 CSV 转 xlsx 后残留的 Sheet1），
// 应该静默跳过空 Sheet，只返回数据 Sheet 的 FileInfo。不该因为空 Sheet 抛 HEADER_ROW_MISSING。
func TestProbeFile_SkipsEmptySheet(t *testing.T) {
	path := buildEmptySheetXLSX(t)

	units, err := probeFile(path, 1, nil)
	if err != nil {
		t.Fatalf("probeFile 不该因空 Sheet 失败: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("期望 1 个单元（数据 Sheet），实际 %d: %+v", len(units), units)
	}
	if units[0].SheetName != "数据" {
		t.Errorf("期望跳过 Sheet1 只保留 '数据'，实际 SheetName=%q", units[0].SheetName)
	}
	if len(units[0].Headers) != 2 || units[0].Headers[0] != "产品名" {
		t.Errorf("数据 Sheet 表头错: %v", units[0].Headers)
	}
}

// 用户主动在 allowSheets 里点名了空 Sheet1（很少见但要合理处理）：
// probeFile 仍跳过它，不报错。
func TestProbeFile_EmptySheetExplicitlySelected(t *testing.T) {
	path := buildEmptySheetXLSX(t)

	allow := newSheetFilter([]string{"Sheet1", "数据"})
	units, err := probeFile(path, 1, allow)
	if err != nil {
		t.Fatalf("probeFile: %v", err)
	}
	// 空 Sheet1 跳过 → 只剩"数据"
	if len(units) != 1 || units[0].SheetName != "数据" {
		t.Errorf("期望只剩 '数据'，实际 %+v", units)
	}
}

// ScanFile 是 public API，也应该正确跳过空 Sheet。
func TestScanFile_SkipsEmptySheet(t *testing.T) {
	path := buildEmptySheetXLSX(t)

	units, err := ScanFile(path, 1, nil)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if len(units) != 1 || units[0].SheetName != "数据" {
		t.Errorf("期望只剩 '数据'，实际 %+v", units)
	}
}

// 构造带空 Sheet1 + "数据" Sheet 的 xlsx，复刻用户真实问题。
func buildEmptySheetXLSX(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mixed.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	// 保留默认 Sheet1（空的）+ 加一个有数据的 Sheet
	idx, err := f.NewSheet("数据")
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.SetCellValue("数据", "A1", "产品名")
	_ = f.SetCellValue("数据", "B1", "数量")
	_ = f.SetCellValue("数据", "A2", "苹果")
	_ = f.SetCellValue("数据", "B2", 10)
	_ = f.SetCellValue("数据", "A3", "橙子")
	_ = f.SetCellValue("数据", "B3", 20)

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}
