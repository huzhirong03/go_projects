package excelio

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildCombinedFixture 构造一个既含公式又有若干自定义行高的 sheet，
// 用来验证 ProbeSheet 一次扫描能同时拿到两者。
func buildCombinedFixture(t *testing.T, withFormula bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "combined.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "数据"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	_ = f.SetCellValue(sheet, "A1", "key")
	_ = f.SetCellValue(sheet, "B1", "val")
	for row := 2; row <= 6; row++ {
		_ = f.SetCellValue(sheet, mustCell(1, row), row)
		_ = f.SetCellValue(sheet, mustCell(2, row), row*10)
	}
	// 自定义行高在第 2/4 行
	_ = f.SetRowHeight(sheet, 2, 30.0)
	_ = f.SetRowHeight(sheet, 4, 45.5)
	if withFormula {
		_ = f.SetCellFormula(sheet, "C5", "A5+B5")
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func TestProbeSheet_NoFormula(t *testing.T) {
	path := buildCombinedFixture(t, false)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	has, hmap, err := r.ProbeSheet("数据")
	if err != nil {
		t.Fatalf("ProbeSheet: %v", err)
	}
	if has {
		t.Errorf("无公式 fixture 应返回 hasFormulas=false, got true")
	}
	if len(hmap) != 2 {
		t.Errorf("应 2 个自定义行高 got=%v", hmap)
	}
	if hmap[2] != 30.0 || hmap[4] != 45.5 {
		t.Errorf("行高值错: %v", hmap)
	}
}

func TestProbeSheet_WithFormula(t *testing.T) {
	path := buildCombinedFixture(t, true)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	has, hmap, err := r.ProbeSheet("数据")
	if err != nil {
		t.Fatalf("ProbeSheet: %v", err)
	}
	if !has {
		t.Errorf("含公式 fixture 应返回 hasFormulas=true, got false")
	}
	if len(hmap) != 2 {
		t.Errorf("行高 map 应 2 项，got=%v", hmap)
	}
}

// TestProbeSheet_FillsBothCaches 确认一次 ProbeSheet 后，
// 后续的 SheetHasFormulas 和 RowHeights 必须零 I/O 命中 cache。
func TestProbeSheet_FillsBothCaches(t *testing.T) {
	path := buildCombinedFixture(t, true)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	_, _, err = r.ProbeSheet("数据")
	if err != nil {
		t.Fatalf("ProbeSheet: %v", err)
	}

	// 篡改 path 让后续方法若走磁盘必然报错，从而证明它们走了 cache
	orig := r.path
	r.path = "nonexistent.xlsx"

	if has, err := r.SheetHasFormulas("数据"); err != nil || !has {
		t.Errorf("SheetHasFormulas 应命中 cache 返回 true，got has=%v err=%v", has, err)
	}
	m, err := r.RowHeights("数据")
	if err != nil {
		t.Errorf("RowHeights 应命中 cache: %v", err)
	}
	if len(m) != 2 {
		t.Errorf("cache 行高应 2 项 got=%v", m)
	}

	r.path = orig
}

// TestProbeSheet_ConsistentWithSeparateAPIs 同一 fixture 上
// ProbeSheet 的返回结果必须和 SheetHasFormulas / RowHeights 各自独立调用时一致。
func TestProbeSheet_ConsistentWithSeparateAPIs(t *testing.T) {
	path := buildCombinedFixture(t, true)

	// 两个独立 Reader 各自跑独立 API，避免 cache 串扰
	r1, err := Open(path)
	if err != nil {
		t.Fatalf("Open r1: %v", err)
	}
	defer r1.Close()
	wantHas, err := r1.SheetHasFormulas("数据")
	if err != nil {
		t.Fatalf("SheetHasFormulas: %v", err)
	}
	wantMap, err := r1.RowHeights("数据")
	if err != nil {
		t.Fatalf("RowHeights: %v", err)
	}

	r2, err := Open(path)
	if err != nil {
		t.Fatalf("Open r2: %v", err)
	}
	defer r2.Close()
	gotHas, gotMap, err := r2.ProbeSheet("数据")
	if err != nil {
		t.Fatalf("ProbeSheet: %v", err)
	}

	if gotHas != wantHas {
		t.Errorf("hasFormulas 不一致: ProbeSheet=%v, separate=%v", gotHas, wantHas)
	}
	if len(gotMap) != len(wantMap) {
		t.Errorf("行高 map 大小不一致: ProbeSheet=%d separate=%d", len(gotMap), len(wantMap))
	}
	for k, v := range wantMap {
		if gotMap[k] != v {
			t.Errorf("row %d 行高不一致: ProbeSheet=%v separate=%v", k, gotMap[k], v)
		}
	}
}
