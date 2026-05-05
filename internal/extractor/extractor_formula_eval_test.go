package extractor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// TestExtract_FormulaCacheFallback 端到端验证方案 A+：
//
//	1) fixture 04 公式 cell 没有 <v> 缓存（SetCellFormula 直接 SaveAs 生成）
//	2) 搜 "300" 应能命中 K 列总分公式行
//	3) 输出文件 K 列必须仍然是公式（不是被静态化）
//
// 这是用户实测场景的回归锁。fixture 04 不在 testdata 而是 testdata_smoke，
// CI 环境若没有该 fixture 会 t.Skip 自动跳过。
func TestExtract_FormulaCacheFallback(t *testing.T) {
	fixturePath := findSmokeFixture(t, "04_学校成绩册_多Sheet交叉公式.xlsx")
	if fixturePath == "" {
		t.Skip("smoke fixture 04 不存在，跳过（请先跑 go run ./cmd/gen-smoke-fixture/）")
	}

	srcDir := t.TempDir()
	srcCopy := filepath.Join(srcDir, "src.xlsx")
	if err := copyFile(fixturePath, srcCopy); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	outDir := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    srcDir,
		Keywords:      []string{"300"},
		MatchMode:     core.MatchExact,
		SearchAllCols: true,
		Output:        core.OutputMerged,
		OutputDir:     outDir,
		HeaderRow:     1,
		// 学生成绩明细只有同 sheet 公式，是测试关注的核心 sheet
		SheetNames: []string{"学生成绩明细"},
	}

	t0 := time.Now()
	result, err := Extract(context.Background(), task, nil)
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if result.RowsMatched == 0 {
		t.Fatalf("没有命中行 — 公式回退求值失效，搜不到公式计算结果。"+
			"RowsMatched=0, elapsed=%v", elapsed)
	}
	t.Logf("命中 %d 行，输出 %d 个文件，总耗时 %v",
		result.RowsMatched, len(result.OutputFiles), elapsed.Round(time.Millisecond))

	// 性能门禁：方案 A+ 对 fixture 04 应该在 10 秒内（含图片迁移、zip 手术等）。
	// 旧版 v1.4.0 全表扫一上来 20 秒，方案 A+ 必须明显快于此阈值。
	if elapsed > 10*time.Second {
		t.Errorf("耗时 %v 超过 10 秒阈值，方案 A+ 性能优化未达预期", elapsed)
	}

	if len(result.OutputFiles) == 0 {
		t.Fatal("无输出文件")
	}
	verifyOutputKColumnIsFormula(t, result.OutputFiles[0])
}

// verifyOutputKColumnIsFormula 打开输出文件，检查"总分"列至少有一个 cell 是公式。
// 这是关键断言：公式回退求值不应"污染"输出（输出仍写公式，不是静态值）。
func verifyOutputKColumnIsFormula(t *testing.T, path string) {
	t.Helper()
	r, err := excelio.Open(path)
	if err != nil {
		t.Fatalf("打开输出文件: %v", err)
	}
	defer r.Close()

	sheets := r.SheetNames()
	if len(sheets) == 0 {
		t.Fatal("输出文件没有 Sheet")
	}

	header, err := r.Header(sheets[0], 1)
	if err != nil {
		t.Fatalf("读表头: %v", err)
	}
	totalColIdx := -1
	for i, h := range header {
		if strings.TrimSpace(h) == "总分" {
			totalColIdx = i
			break
		}
	}
	if totalColIdx < 0 {
		t.Fatalf("输出表头未找到\"总分\"列: %v", header)
	}

	foundFormula := false
	for row := 2; row <= 5; row++ {
		ref := smallExcelRef(totalColIdx+1, row)
		formula, err := r.CellFormula(sheets[0], ref)
		if err == nil && formula != "" {
			foundFormula = true
			t.Logf("输出文件 %s = 公式 %q ✅", ref, formula)
			break
		}
	}
	if !foundFormula {
		t.Errorf("输出文件\"总分\"列前 4 行都不是公式 — 公式被错误静态化了")
	}
}

// smallExcelRef 简化版 col→A,B,C... + row 拼接（仅支持 col<=26）。
func smallExcelRef(col, row int) string {
	if col < 1 || col > 26 {
		return ""
	}
	return string(rune('A'+col-1)) + itoaSmall(row)
}

func itoaSmall(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// findSmokeFixture 从测试运行目录向上找 testdata_smoke/<name>，最多 6 层。
func findSmokeFixture(t *testing.T, name string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := cwd
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "testdata_smoke", name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
