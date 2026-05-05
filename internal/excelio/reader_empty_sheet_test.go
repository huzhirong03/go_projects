package excelio

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"

	"excel-master/internal/core"
)

// buildFixtureWithEmptySheet 构造一个带空 Sheet1 + 数据 Sheet 的 xlsx，模拟
// Excel 把 CSV 另存为 xlsx 时遗留的真实场景（Excel 默认有个 Sheet1 不删）。
func buildFixtureWithEmptySheet(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "empty_sheet.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	// 保留默认的空 Sheet1（关键：不删它，这就是用户的真实场景）
	// 加一个有数据的业务 Sheet
	idx, err := f.NewSheet("数据")
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.SetCellValue("数据", "A1", "产品名")
	_ = f.SetCellValue("数据", "B1", "数量")
	_ = f.SetCellValue("数据", "A2", "苹果")
	_ = f.SetCellValue("数据", "B2", 10)

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

// Sheet1 完全空 → Header 应返回 EMPTY_SHEET 错误（而不是 HEADER_ROW_MISSING）。
func TestHeader_EmptySheet_ReturnsEmptySheetCode(t *testing.T) {
	path := buildFixtureWithEmptySheet(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	_, err = r.Header("Sheet1", 1)
	if err == nil {
		t.Fatal("期望 EMPTY_SHEET 错误，实际无错")
	}

	var ae *core.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("期望 *core.AppError，实际 %T: %v", err, err)
	}
	if ae.Code != core.CodeEmptySheet {
		t.Fatalf("期望 Code=%q，实际 %q", core.CodeEmptySheet, ae.Code)
	}
	if !core.IsEmptySheet(err) {
		t.Fatal("core.IsEmptySheet 应返回 true")
	}
}

// 数据 Sheet 的 Header 读取不受影响。
func TestHeader_NormalSheet_StillWorks(t *testing.T) {
	path := buildFixtureWithEmptySheet(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	h, err := r.Header("数据", 1)
	if err != nil {
		t.Fatalf("Header: %v", err)
	}
	if len(h) != 2 || h[0] != "产品名" || h[1] != "数量" {
		t.Errorf("Header=%v", h)
	}
}

// Sheet 有 1 行数据但用户 headerRow 填了 5（超出）→ 仍报 HEADER_ROW_MISSING，
// 不是 EMPTY_SHEET。这是配置错误，不该被当成"空 Sheet"静默跳过。
func TestHeader_HeaderRowOutOfRange_NotEmptySheet(t *testing.T) {
	path := buildFixtureWithEmptySheet(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	_, err = r.Header("数据", 5) // 数据 Sheet 只有 2 行，5 超出
	if err == nil {
		t.Fatal("期望 HEADER_ROW_MISSING 错误")
	}
	if core.IsEmptySheet(err) {
		t.Fatal("headerRow 超出范围不该被判定为 EMPTY_SHEET（用户配置错误应报 fatal）")
	}

	var ae *core.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("期望 *core.AppError: %v", err)
	}
	if ae.Code != "HEADER_ROW_MISSING" {
		t.Fatalf("期望 Code=HEADER_ROW_MISSING，实际 %q", ae.Code)
	}
}

// core.IsEmptySheet 对 nil / 其他错误返回 false（防御性测试）。
func TestIsEmptySheet_NegativeCases(t *testing.T) {
	if core.IsEmptySheet(nil) {
		t.Error("nil 不该被判定为 EmptySheet")
	}
	if core.IsEmptySheet(errors.New("plain error")) {
		t.Error("普通 error 不该被判定为 EmptySheet")
	}
	if core.IsEmptySheet(core.New("OTHER_CODE", "msg")) {
		t.Error("非 EMPTY_SHEET Code 的 AppError 不该被判定为 EmptySheet")
	}
}
