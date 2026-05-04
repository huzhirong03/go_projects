package excelio

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildFormulaFixture 生成一个 xlsx，Sheet "数据" 前两行数据，根据 withFormula 决定
// D2 是否带公式 "=B2*C2"。用于验证 SheetHasFormulas 的两种分支。
func buildFormulaFixture(t *testing.T, withFormula bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "formula_fixture.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "数据"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	// 表头
	_ = f.SetCellValue(sheet, "A1", "产品")
	_ = f.SetCellValue(sheet, "B1", "数量")
	_ = f.SetCellValue(sheet, "C1", "单价")
	_ = f.SetCellValue(sheet, "D1", "小计")
	// 数据
	_ = f.SetCellValue(sheet, "A2", "口红")
	_ = f.SetCellValue(sheet, "B2", 10)
	_ = f.SetCellValue(sheet, "C2", 99.9)
	if withFormula {
		if err := f.SetCellFormula(sheet, "D2", "B2*C2"); err != nil {
			t.Fatalf("SetCellFormula: %v", err)
		}
	} else {
		_ = f.SetCellValue(sheet, "D2", 999.0)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func TestSheetHasFormulas_NoFormula(t *testing.T) {
	path := buildFormulaFixture(t, false)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	has, err := r.SheetHasFormulas("数据")
	if err != nil {
		t.Fatalf("SheetHasFormulas: %v", err)
	}
	if has {
		t.Fatalf("无公式 sheet 应返回 false，但返回 true")
	}
}

func TestSheetHasFormulas_WithFormula(t *testing.T) {
	path := buildFormulaFixture(t, true)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	has, err := r.SheetHasFormulas("数据")
	if err != nil {
		t.Fatalf("SheetHasFormulas: %v", err)
	}
	if !has {
		t.Fatalf("含公式 sheet 应返回 true，但返回 false")
	}
}

// TestSheetHasFormulas_Cache 验证同一 Reader 上多次调用同一 sheet 只扫一次 zip。
// 通过调用前后检查 formulaProbeCache 状态来间接证明。
func TestSheetHasFormulas_Cache(t *testing.T) {
	path := buildFormulaFixture(t, false)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	// 第一次：触发 zip 扫描并缓存结果
	has1, err := r.SheetHasFormulas("数据")
	if err != nil {
		t.Fatalf("首次 SheetHasFormulas: %v", err)
	}
	if len(r.formulaProbeCache) != 1 {
		t.Fatalf("首次调用后 cache 应有 1 条，got %d", len(r.formulaProbeCache))
	}
	if v, ok := r.formulaProbeCache["数据"]; !ok || v != has1 {
		t.Fatalf("cache 值不匹配首次结果: ok=%v v=%v has1=%v", ok, v, has1)
	}

	// 第二次：必须直接命中 cache，不读 zip。用"路径篡改"验证：
	// 把 r.path 改成不存在的路径，如果代码没走 cache 必然报错。
	orig := r.path
	r.path = "this-file-must-not-exist.xlsx"
	has2, err := r.SheetHasFormulas("数据")
	r.path = orig
	if err != nil {
		t.Fatalf("第二次（应命中 cache）不该报错: %v", err)
	}
	if has2 != has1 {
		t.Fatalf("二次调用结果应与首次一致: has1=%v has2=%v", has1, has2)
	}
}

func TestSheetHasFormulas_UnknownSheet(t *testing.T) {
	path := buildFormulaFixture(t, false)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	_, err = r.SheetHasFormulas("根本不存在的sheet")
	if err == nil {
		t.Fatalf("未知 sheet 应返回错误")
	}
}
