package excelio

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildHeightFixture 构造一个 sheet，部分行设置自定义 ht，其它保持默认高度。
// 用于验证 RowHeights 与 RowHeight 在同样数据上返回一致的结果。
func buildHeightFixture(t *testing.T) (path string, expected map[int]float64) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "height_fixture.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "数据"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	// 6 行数据：1、3、5 行设自定义高度；2、4 行保持默认；6 行设 15.0（被视为默认不记）
	for row := 1; row <= 6; row++ {
		_ = f.SetCellValue(sheet, mustCell(1, row), row)
	}
	_ = f.SetRowHeight(sheet, 1, 30.0)
	_ = f.SetRowHeight(sheet, 3, 40.5)
	_ = f.SetRowHeight(sheet, 5, 50.0)
	_ = f.SetRowHeight(sheet, 6, 15.0) // 默认值，按现有 RowHeight 语义视为未自定义

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	expected = map[int]float64{
		1: 30.0,
		3: 40.5,
		5: 50.0,
	}
	return path, expected
}

func TestRowHeights_Basic(t *testing.T) {
	path, expected := buildHeightFixture(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	m, err := r.RowHeights("数据")
	if err != nil {
		t.Fatalf("RowHeights: %v", err)
	}
	if len(m) != len(expected) {
		t.Fatalf("RowHeights 返回 %d 项，预期 %d: got=%v", len(m), len(expected), m)
	}
	for row, want := range expected {
		got, ok := m[row]
		if !ok {
			t.Errorf("row %d 应在 map 里，但缺失", row)
			continue
		}
		if got != want {
			t.Errorf("row %d: got=%v want=%v", row, got, want)
		}
	}
	// 没自定义的行不应在 map 里
	for _, row := range []int{2, 4, 6} {
		if _, ok := m[row]; ok {
			t.Errorf("row %d 本应未自定义，却在 map 里", row)
		}
	}
}

// TestRowHeights_ConsistentWithRowHeight 断言：对每一行，
// RowHeights map 的值必须与 r.RowHeight 单点查询的返回一致。
// 这是 extractor 切换到 map 路径的前置条件——语义零回归。
func TestRowHeights_ConsistentWithRowHeight(t *testing.T) {
	path, _ := buildHeightFixture(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	m, err := r.RowHeights("数据")
	if err != nil {
		t.Fatalf("RowHeights: %v", err)
	}

	for row := 1; row <= 6; row++ {
		wantH, wantCustom, err := r.RowHeight("数据", row)
		if err != nil {
			t.Fatalf("RowHeight(%d): %v", row, err)
		}
		got, ok := m[row]
		if wantCustom {
			if !ok || got != wantH {
				t.Errorf("row %d: RowHeight=(%v,custom=%v) 但 map=(%v, ok=%v)",
					row, wantH, wantCustom, got, ok)
			}
		} else {
			if ok {
				t.Errorf("row %d: RowHeight 未自定义，但 map 里有 %v", row, got)
			}
		}
	}
}

func TestRowHeights_Cache(t *testing.T) {
	path, _ := buildHeightFixture(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	m1, err := r.RowHeights("数据")
	if err != nil {
		t.Fatalf("首次: %v", err)
	}

	// 篡改 path 验证二次调用命中 cache 不读 zip
	orig := r.path
	r.path = "nonexistent.xlsx"
	m2, err := r.RowHeights("数据")
	r.path = orig
	if err != nil {
		t.Fatalf("二次（应命中 cache）: %v", err)
	}
	if len(m2) != len(m1) {
		t.Fatalf("cache 命中后 map 项数不一致: first=%d second=%d", len(m1), len(m2))
	}
}
