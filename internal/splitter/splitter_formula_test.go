package splitter

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/xuri/excelize/v2"

	"excel-master/internal/core"
)

// buildFormulaSplitFixture 生成一个按公式列拆分的测试文件：
//
//	Sheet "数据"：
//	  A (产品)  B (分类码)  C (分类名)
//	  口红      1           =IF(B2=1,"美妆",IF(B2=2,"服饰","其他"))   ← 公式无 <v> 缓存
//	  T 恤     2           同上公式（引用本行 B 列）
//	  水杯     3           同上公式
//	  唇膏     1           同上公式（应归到"美妆"）
//
// 按"分类名"列拆分，预期：3 个文件（美妆 2 行、服饰 1 行、其他 1 行）
//
// 若公式求值未接入 splitter：cells[2] 全是空串 → 全进 "__空值__" → 只 1 个文件 ❌
func buildFormulaSplitFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "formula_split.xlsx")
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	const sheet = "数据"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	_ = f.SetCellValue(sheet, "A1", "产品")
	_ = f.SetCellValue(sheet, "B1", "分类码")
	_ = f.SetCellValue(sheet, "C1", "分类名")

	rows := []struct {
		name string
		code int
	}{
		{"口红", 1},
		{"T恤", 2},
		{"水杯", 3},
		{"唇膏", 1},
	}
	for i, r := range rows {
		row := i + 2
		rs := strconv.Itoa(row)
		_ = f.SetCellValue(sheet, "A"+rs, r.name)
		_ = f.SetCellValue(sheet, "B"+rs, r.code)
		// SetCellFormula 不写 <v>，就是模拟 fixture 04 的场景
		if err := f.SetCellFormula(sheet, "C"+rs,
			"IF(B"+rs+"=1,\"美妆\",IF(B"+rs+"=2,\"服饰\",\"其他\"))"); err != nil {
			t.Fatalf("SetCellFormula: %v", err)
		}
	}

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

// TestSplitByColumn_UncachedFormulaColumn 锁定方案 A+ 在 splitter 路径
// 的正确接入：按公式列拆分能基于公式求值结果正确分桶。
func TestSplitByColumn_UncachedFormulaColumn(t *testing.T) {
	srcPath := buildFormulaSplitFixture(t)
	outDir := t.TempDir()

	task := core.SplitTask{
		SourcePath:   srcPath,
		Mode:         core.SplitByColumn,
		HeaderRow:    1,
		SplitColumn:  "分类名", // 公式列
		OutputDir:    outDir,
		OutputTarget: core.OutputTargetNewFiles,
	}

	result, err := SplitByColumn(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByColumn: %v", err)
	}

	// 应产出 3 个文件（美妆 / 服饰 / 其他），而不是 1 个（__空值__）
	if result.PartsCreated != 3 {
		names := []string{}
		for _, p := range result.OutputFiles {
			names = append(names, filepath.Base(p))
		}
		t.Fatalf("PartsCreated=%d 期望 3；输出文件=%v", result.PartsCreated, names)
	}

	// 输出文件名应包含分类名——检查三个桶都有
	wantKeys := map[string]bool{"美妆": false, "服饰": false, "其他": false}
	entries, _ := os.ReadDir(outDir)
	for _, e := range entries {
		for k := range wantKeys {
			if contains(e.Name(), k) {
				wantKeys[k] = true
			}
		}
	}
	for k, ok := range wantKeys {
		if !ok {
			t.Errorf("输出文件缺少分类 %q", k)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	n, m := len(s), len(sub)
	if m == 0 || m > n {
		return -1
	}
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == sub {
			return i
		}
	}
	return -1
}
