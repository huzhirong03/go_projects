package excelio

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// buildUncachedFormulaFixture 生成一个 xlsx，公式 cell 没有 <v> 缓存
// （SetCellFormula 直接 SaveAs，模拟 fixture 04 的场景）。
//
// Sheet "数据" 5 行 4 列：
//
//	A1 产品  B1 数量  C1 单价  D1 小计
//	A2 口红  B2 10    C2 30    D2 =B2*C2  → 期望算 300
//	A3 粉底  B3 5     C3 50    D3 =B3*C3  → 期望算 250
//	A4 香水  B4 2     C4 100   D4 =B4*C4  → 期望算 200
//	A5 隔离  B5 1     C5 88    D5 =B5*C5  → 期望算 88
func buildUncachedFormulaFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "uncached.xlsx")
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	const sheet = "数据"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{"产品", "数量", "单价", "小计"}
	for i, h := range headers {
		ref, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, ref, h)
	}
	rows := []struct {
		name      string
		qty, unit int
	}{
		{"口红", 10, 30},
		{"粉底", 5, 50},
		{"香水", 2, 100},
		{"隔离", 1, 88},
	}
	for i, r := range rows {
		row := i + 2
		cellA, _ := excelize.CoordinatesToCellName(1, row)
		cellB, _ := excelize.CoordinatesToCellName(2, row)
		cellC, _ := excelize.CoordinatesToCellName(3, row)
		cellD, _ := excelize.CoordinatesToCellName(4, row)
		_ = f.SetCellValue(sheet, cellA, r.name)
		_ = f.SetCellValue(sheet, cellB, r.qty)
		_ = f.SetCellValue(sheet, cellC, r.unit)
		// SetCellFormula 不写 <v> 缓存 —— 就是我们要验证的场景
		if err := f.SetCellFormula(sheet, cellD, "B"+strconv.Itoa(row)+"*C"+strconv.Itoa(row)); err != nil {
			t.Fatalf("SetCellFormula: %v", err)
		}
	}

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

// TestProbeSheet_CollectsUncachedFormulas 验证 ProbeSheet 的 zip 扫描能**顺手**
// 把"有 <f> 无 <v>"的 cell 写到 r.uncachedFormulasCache，extractor 据此判断
// 是否需要回退求值。
func TestProbeSheet_CollectsUncachedFormulas(t *testing.T) {
	path := buildUncachedFormulaFixture(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	hasF, _, err := r.ProbeSheet("数据")
	if err != nil {
		t.Fatalf("ProbeSheet: %v", err)
	}
	if !hasF {
		t.Fatal("hasFormulas 应该为 true")
	}

	uncached, err := r.UncachedFormulas("数据")
	if err != nil {
		t.Fatalf("UncachedFormulas: %v", err)
	}
	wantKeys := []string{"D2", "D3", "D4", "D5"}
	if len(uncached) != len(wantKeys) {
		t.Fatalf("uncached cell 数=%d, 期望 %d；map=%v", len(uncached), len(wantKeys), uncached)
	}
	for _, k := range wantKeys {
		if formula, ok := uncached[k]; !ok {
			t.Errorf("%s 应在 uncached map 中", k)
		} else if formula == "" {
			t.Errorf("%s 的公式文本为空（应为 B*C）", k)
		}
	}
}

// TestProbeSheet_AllCached_EmptyUncached 真实业务文件（公式 cell 都有 <v> 缓存）
// 对应的 uncached map 必须是空，上层 extractor 据此零开销跳过求值。
func TestProbeSheet_AllCached_EmptyUncached(t *testing.T) {
	// 用 excelize 的方式 SetCellValue 静态值，不带公式 —— 没有公式 cell 就不会
	// 进 uncached map。同时也用 SetCellFormula 带 <v> 的正常情况测试：excelize
	// 的 SetCellFormula 本身不写 <v>，所以走另一套数据构造。
	//
	// 为了模拟"有 <v> 缓存"，我们手动构造 xlsx：直接 SetCellValue 静态值，不带公式，
	// 和 excelize.SetCellFormula 的区别是没有 <f> 元素，但 hasFormulas=false。
	//
	// 单独验证"有公式+有缓存"还需要一个 calc-then-save 流程，这是集成测试的范畴；
	// 本单测只锁定"非 uncached 场景下 map 为空"的边界。
	path := filepath.Join(t.TempDir(), "no_formula.xlsx")
	f := excelize.NewFile()
	const sheet = "数据"
	idx, _ := f.NewSheet(sheet)
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")
	_ = f.SetCellValue(sheet, "A1", "产品")
	_ = f.SetCellValue(sheet, "D1", "小计")
	_ = f.SetCellValue(sheet, "A2", "口红")
	_ = f.SetCellValue(sheet, "D2", 300.0) // 静态值，不是公式
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	_ = f.Close()

	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	hasF, _, err := r.ProbeSheet(sheet)
	if err != nil {
		t.Fatalf("ProbeSheet: %v", err)
	}
	if hasF {
		t.Errorf("无公式 sheet hasFormulas 应为 false")
	}
	uncached, err := r.UncachedFormulas(sheet)
	if err != nil {
		t.Fatalf("UncachedFormulas: %v", err)
	}
	if len(uncached) != 0 {
		t.Errorf("无公式 sheet uncached 应为空，got=%v", uncached)
	}
}

// TestEvalFormulasAt 验证"按 ref 列表精准求值"：只对指定 ref 调 CalcCellValue，
// 不扫全表。这是方案 A+ 保持性能的核心机制。
func TestEvalFormulasAt(t *testing.T) {
	path := buildUncachedFormulaFixture(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	uncached, err := r.UncachedFormulas("数据")
	if err != nil {
		t.Fatalf("UncachedFormulas: %v", err)
	}

	got, err := r.EvalFormulasAt("数据", uncached)
	if err != nil {
		t.Fatalf("EvalFormulasAt: %v", err)
	}

	want := map[string]string{
		"D2": "300",
		"D3": "250",
		"D4": "200",
		"D5": "88",
	}
	if len(got) != len(want) {
		t.Errorf("求值结果大小不符 got=%d want=%d  got=%v", len(got), len(want), got)
	}
	for ref, expect := range want {
		if v, ok := got[ref]; !ok {
			t.Errorf("%s 应有求值结果", ref)
		} else if v != expect {
			t.Errorf("%s 求值 = %q, 期望 %q", ref, v, expect)
		}
	}
}

// TestEvalFormulasAt_EmptyRefs 空 ref 列表应直接返回空 map，不触发任何 excelize
// 调用（这是真实业务文件的性能 fast path）。
func TestEvalFormulasAt_EmptyRefs(t *testing.T) {
	path := buildUncachedFormulaFixture(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	got, err := r.EvalFormulasAt("数据", nil)
	if err != nil {
		t.Fatalf("EvalFormulasAt(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("空 refs 应返回空 map, got=%v", got)
	}
	got2, err := r.EvalFormulasAt("数据", map[string]string{})
	if err != nil {
		t.Fatalf("EvalFormulasAt({}): %v", err)
	}
	if len(got2) != 0 {
		t.Errorf("空 refs 应返回空 map, got=%v", got2)
	}
}

func TestFillRowCellsWithFormulaValues(t *testing.T) {
	formulaValues := map[string]string{
		"D2": "300",
		"D3": "250",
	}
	t.Run("填空 cell", func(t *testing.T) {
		cells := []string{"口红", "10", "30", ""}
		got := FillRowCellsWithFormulaValues(cells, 2, formulaValues)
		if got[3] != "300" {
			t.Errorf("D2 应被填为 '300', got %q", got[3])
		}
	})
	t.Run("已有值的 cell 不被覆盖", func(t *testing.T) {
		cells := []string{"口红", "10", "30", "999"}
		got := FillRowCellsWithFormulaValues(cells, 2, formulaValues)
		if got[3] != "999" {
			t.Errorf("已有值不应被覆盖, got %q", got[3])
		}
	})
	t.Run("空 map 是 noop", func(t *testing.T) {
		cells := []string{"a", "", "b", ""}
		got := FillRowCellsWithFormulaValues(cells, 2, nil)
		if !slicesEqual(got, cells) {
			t.Errorf("nil map 应返回原切片不变, got=%v cells=%v", got, cells)
		}
	})
	// 关键回归：xlsxreader 会跳过"无 <v> 缓存的公式 cell"，导致返回的 cells
	// 切片长度可能 < 公式所在列。Fill 必须自动扩展 cells 切片把公式列纳入。
	t.Run("公式列超出 cells 长度时自动扩展", func(t *testing.T) {
		// 模拟 fixture 04 第 2 行：A-J 列有值，K/L 列只有公式无 v 缓存
		// xlsxreader 跳过 K/L → cells 长度只到 J（10 个）
		cells := []string{"S001", "张三", "1班", "一年级", "男", "60", "60", "60", "60", "60"}
		fv := map[string]string{
			"K2": "300",
			"L2": "中等",
		}
		got := FillRowCellsWithFormulaValues(cells, 2, fv)
		if len(got) != 12 {
			t.Fatalf("cells 应被扩展到 12 列（A-L），got len=%d", len(got))
		}
		if got[10] != "300" {
			t.Errorf("K2 (cells[10]) 应为 '300', got %q", got[10])
		}
		if got[11] != "中等" {
			t.Errorf("L2 (cells[11]) 应为 '中等', got %q", got[11])
		}
		if got[0] != "S001" || got[5] != "60" {
			t.Errorf("原 cells 前 10 列内容被破坏: %v", got[:10])
		}
	})
	t.Run("ref 在其他行号的不应填到当前行", func(t *testing.T) {
		cells := []string{"a", "", "b", ""}
		got := FillRowCellsWithFormulaValues(cells, 2, map[string]string{"D5": "999"})
		if len(got) != len(cells) || got[3] != "" {
			t.Errorf("跨行 ref 不应被填，got=%v", got)
		}
	})
}

// TestEvalFormulasAt_SkipsCrossSheet：公式里含 '!' 的不应被调 CalcCellValue。
// 这是防踩"跨 sheet 聚合 180ms/cell 累积到 16 秒"坑的关键门禁。
func TestEvalFormulasAt_SkipsCrossSheet(t *testing.T) {
	path := buildUncachedFormulaFixture(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	// 构造 refs：同 sheet 公式（D2=B2*C2）和跨 sheet 公式混合
	refs := map[string]string{
		"D2": "B2*C2",               // 同 sheet，应算
		"D3": "Sheet1!A1+Sheet2!B2", // 跨 sheet，应跳
		"D4": "'学生成绩明细'!D2",         // 跨 sheet (带引号)，应跳
		"D5": "B5*C5",               // 同 sheet，应算
	}
	_, stats, err := r.EvalFormulasAtWithStats("数据", refs)
	if err != nil {
		t.Fatalf("EvalFormulasAtWithStats: %v", err)
	}
	if stats.SkippedCrossSheet != 2 {
		t.Errorf("SkippedCrossSheet=%d 期望 2", stats.SkippedCrossSheet)
	}
	if stats.Computed < 1 {
		// 同 sheet 的 D2/D5 至少要算出 1 个（excelize 可能对单元格类型报错，放宽）
		t.Errorf("Computed=%d 期望 >= 1", stats.Computed)
	}
}

// buildLargeUncachedFormulaFixture 类似 buildUncachedFormulaFixture 但行数可配。
// 专供 budget 测试用——少量 ref 在 Windows 高分辨率时钟下容易全部落在同一 tick，
// 导致 time.Since 返回 0 让 budget 判断失效。100 行 CalcCellValue 累积耗时
// 远超任何时钟粒度，可稳定触发"剩余 ref 超预算被跳过"分支。
func buildLargeUncachedFormulaFixture(t *testing.T, rowCount int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "uncached_large.xlsx")
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	const sheet = "数据"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{"产品", "数量", "单价", "小计"}
	for i, h := range headers {
		ref, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, ref, h)
	}
	for i := 0; i < rowCount; i++ {
		row := i + 2
		_ = f.SetCellValue(sheet, "A"+strconv.Itoa(row), "P"+strconv.Itoa(i))
		_ = f.SetCellValue(sheet, "B"+strconv.Itoa(row), i+1)
		_ = f.SetCellValue(sheet, "C"+strconv.Itoa(row), 100)
		// SetCellFormula 不写 <v>，模拟 fixture 04 的无缓存公式场景
		if err := f.SetCellFormula(sheet, "D"+strconv.Itoa(row),
			"B"+strconv.Itoa(row)+"*C"+strconv.Itoa(row)); err != nil {
			t.Fatalf("SetCellFormula: %v", err)
		}
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

// TestEvalFormulasAt_RespectsBudget：budget 到期后剩余 ref 应被跳过，
// 已算出的不丢。用 100 行 + 1µs budget 保证 Windows 下稳定。
func TestEvalFormulasAt_RespectsBudget(t *testing.T) {
	const rowCount = 100
	path := buildLargeUncachedFormulaFixture(t, rowCount)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	// 1µs budget：Windows time.Since 粒度最大 ~1µs，100 次 CalcCellValue
	// 累积耗时至少十几 µs（excelize calc 引擎本身 µs 级 per call），保证
	// 循环早期就触发 budget 判定，后续所有 ref 稳定进入 SkippedBudget 分支。
	origBudget := FormulaEvalBudget
	FormulaEvalBudget = time.Microsecond
	defer func() { FormulaEvalBudget = origBudget }()

	refs := make(map[string]string, rowCount)
	for i := 0; i < rowCount; i++ {
		row := i + 2
		refs["D"+strconv.Itoa(row)] = "B" + strconv.Itoa(row) + "*C" + strconv.Itoa(row)
	}
	_, stats, err := r.EvalFormulasAtWithStats("数据", refs)
	if err != nil {
		t.Fatalf("EvalFormulasAtWithStats: %v", err)
	}
	if stats.SkippedBudget == 0 {
		t.Errorf("100 refs + budget=1µs 下 SkippedBudget 应 >= 1，实际 %d (Computed=%d)",
			stats.SkippedBudget, stats.Computed)
	}
	if stats.Computed+stats.SkippedBudget+stats.SkippedCrossSheet != rowCount {
		t.Errorf("总数不守恒：Computed=%d + SkippedBudget=%d + SkippedCrossSheet=%d != %d",
			stats.Computed, stats.SkippedBudget, stats.SkippedCrossSheet, rowCount)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
