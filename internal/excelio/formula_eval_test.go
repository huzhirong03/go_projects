package excelio

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildFormulaEvalFixture 生成一个有"无 v 缓存的公式 cell"的 xlsx，
// 模拟 fixture 04 那种 SetCellFormula 写完直接 SaveAs 不走 calc 的场景。
//
// Sheet "数据" 5 行 4 列：
//
//	A1 产品  B1 数量  C1 单价  D1 小计
//	A2 口红  B2 10    C2 30    D2 =B2*C2  → 期望算出 300
//	A3 粉底  B3 5     C3 50    D3 =B3*C3  → 期望算出 250
//	A4 香水  B4 2     C4 100   D4 =B4*C4  → 期望算出 200
//	A5 隔离  B5 1     C5 88    D5 =B5*C5  → 期望算出 88
func buildFormulaEvalFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "formula_eval.xlsx")
	f := excelize.NewFile()
	defer f.Close()

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
		_ = f.SetCellValue(sheet, mustCoord(t, 1, row), r.name)
		_ = f.SetCellValue(sheet, mustCoord(t, 2, row), r.qty)
		_ = f.SetCellValue(sheet, mustCoord(t, 3, row), r.unit)
		// SetCellFormula 不会写 <v> 缓存 —— 这正是我们要测试的场景
		if err := f.SetCellFormula(sheet, mustCoord(t, 4, row), "B"+strconv.Itoa(row)+"*C"+strconv.Itoa(row)); err != nil {
			t.Fatalf("SetCellFormula: %v", err)
		}
	}

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func mustCoord(t *testing.T, col, row int) string {
	t.Helper()
	r, err := excelize.CoordinatesToCellName(col, row)
	if err != nil {
		t.Fatalf("CoordinatesToCellName(%d,%d): %v", col, row, err)
	}
	return r
}

func TestEvaluateFormulas_FillsMissingCacheValues(t *testing.T) {
	path := buildFormulaEvalFixture(t)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	got, err := r.EvaluateFormulas("数据")
	if err != nil {
		t.Fatalf("EvaluateFormulas: %v", err)
	}

	// 4 个公式 cell 都应被求出
	want := map[string]string{
		"D2": "300",
		"D3": "250",
		"D4": "200",
		"D5": "88",
	}
	if len(got) != len(want) {
		t.Errorf("公式 map 大小不符 got=%d want=%d  got=%v", len(got), len(want), got)
	}
	for ref, expect := range want {
		if v, ok := got[ref]; !ok {
			t.Errorf("缺少 %s 的求值结果", ref)
		} else if v != expect {
			t.Errorf("%s 求值 = %q, 期望 %q", ref, v, expect)
		}
	}
}

func TestEvaluateFormulas_SkipsCellsWithCachedValues(t *testing.T) {
	// 跟 buildFormulaEvalFixture 同样数据，但用 excelize 把 D 列直接设成静态值（不是公式）
	// → EvaluateFormulas 应该返回空 map（没有公式可补）
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

	got, err := r.EvaluateFormulas(sheet)
	if err != nil {
		t.Fatalf("EvaluateFormulas: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("无公式 sheet 应返回空 map，got=%v", got)
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
	t.Run("formulaValues 无对应 ref 时跳过", func(t *testing.T) {
		cells := []string{"口红", "", "30", ""}
		got := FillRowCellsWithFormulaValues(cells, 2, formulaValues)
		if got[1] != "" {
			t.Errorf("无 B2 在 map 中, cells[1] 应保持空, got %q", got[1])
		}
		if got[3] != "300" {
			t.Errorf("D2 应被填为 '300', got %q", got[3])
		}
	})
	t.Run("空 map 是 noop", func(t *testing.T) {
		cells := []string{"a", "", "b", ""}
		got := FillRowCellsWithFormulaValues(cells, 2, nil)
		if !slicesEqual(got, cells) {
			t.Errorf("nil map 应返回原切片不变, got=%v cells=%v", got, cells)
		}
		got2 := FillRowCellsWithFormulaValues(cells, 2, map[string]string{})
		if !slicesEqual(got2, cells) {
			t.Errorf("空 map 应返回原切片不变, got=%v cells=%v", got2, cells)
		}
	})
	// 关键回归：xlsxreader 会跳过"无 <v> 缓存的公式 cell"，导致返回的 cells 切片
	// 长度可能 < 公式所在列。Fill 必须自动扩展 cells 切片把公式列纳入。
	t.Run("公式列超出 cells 长度时自动扩展（xlsxreader 跳无 v 公式 cell 场景）", func(t *testing.T) {
		// 模拟 fixture 04 学生成绩明细第 2 行：A-J 列有值，K/L 列只有公式无 v 缓存
		// xlsxreader 跳过 K/L → cells 长度只到 J（10 个）
		cells := []string{"S001", "张三", "1班", "一年级", "男", "60", "60", "60", "60", "60"}
		fv := map[string]string{
			"K2": "300", // 总分
			"L2": "中等",  // 评级
		}
		got := FillRowCellsWithFormulaValues(cells, 2, fv)
		if len(got) != 12 {
			t.Fatalf("cells 应被扩展到 12 列（A-L），got len=%d", len(got))
		}
		if got[10] != "300" {
			t.Errorf("K2 (cells[10]) 应被填为 '300', got %q", got[10])
		}
		if got[11] != "中等" {
			t.Errorf("L2 (cells[11]) 应被填为 '中等', got %q", got[11])
		}
		// 原前 10 列应该保持
		if got[0] != "S001" || got[5] != "60" {
			t.Errorf("原 cells 内容被破坏: %v", got[:10])
		}
	})
	t.Run("公式列在其他行号 → 不应填到当前行", func(t *testing.T) {
		cells := []string{"a", "", "b", ""}
		// 当前行号是 2，但 map 里 ref 是 D5（行号 5），应该跳过
		got := FillRowCellsWithFormulaValues(cells, 2, map[string]string{"D5": "999"})
		if len(got) != len(cells) || got[3] != "" {
			t.Errorf("跨行 ref 不应被填，got=%v", got)
		}
	})
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
