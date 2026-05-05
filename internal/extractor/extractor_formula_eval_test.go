package extractor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// TestExtract_FormulaCacheFallback 验证：当源文件公式 cell 没有 <v> 缓存值时
// （SetCellFormula 写完直接保存的 fixture 04 / 用户手编未保存就发的文件），
// 扫描阶段能自动调 CalcCellValue 求值，使搜索能命中"公式计算结果"。
//
// 同时验证：输出文件 K 列仍然是公式（不是被求值后的静态值）。
//
// fixture 04 路径：testdata_smoke/04_学校成绩册_多Sheet交叉公式.xlsx
//   - K 列：=F+G+H+I+J（总分）
//   - L 列：=IF(K>=400,"优秀",...)（评级）
//   - 第 1 行学生 F=G=H=I=J=60 → K=300 → L="中等"
//
// 这个 fixture 不在 testdata 而是 testdata_smoke，CI 环境可能没有，t.Skip 跳过。
func TestExtract_FormulaCacheFallback(t *testing.T) {
	fixturePath := findSmokeFixture(t, "04_学校成绩册_多Sheet交叉公式.xlsx")
	if fixturePath == "" {
		t.Skip("smoke fixture 04 不存在，跳过（请先跑 go run ./cmd/gen-smoke-fixture/）")
	}

	// 用单文件夹方式跑（dir 里只有 fixture 04）
	srcDir := t.TempDir()
	srcCopy := filepath.Join(srcDir, "src.xlsx")
	if err := copyFile(fixturePath, srcCopy); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	outDir := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    srcDir,
		Keywords:      []string{"300"},
		MatchMode:     core.MatchExact, // 精准模式：只在数值化匹配，验证回退求值正确
		SearchAllCols: true,
		Output:        core.OutputMerged,
		OutputDir:     outDir,
		HeaderRow:     1,
		// 只跑学生成绩明细（其他 sheet 含跨 sheet 公式，不是本测试关注点）
		SheetNames: []string{"学生成绩明细"},
	}

	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if result.RowsMatched == 0 {
		t.Fatalf("没有命中行；说明公式回退求值没生效。RowsMatched=0")
	}
	t.Logf("命中行数=%d，输出文件数=%d", result.RowsMatched, len(result.OutputFiles))

	// 验证输出文件 K 列仍是公式（不是被求值后的静态值）
	if len(result.OutputFiles) == 0 {
		t.Fatal("无输出文件")
	}
	verifyOutputKColumnIsFormula(t, result.OutputFiles[0])
}

// verifyOutputKColumnIsFormula 打开输出文件，检查 K 列至少有一个 cell 是公式。
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

	// 表头第 1 行；数据从第 2 行开始
	// 找到"总分"对应的列字母
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

	// 检查"总分"列前几行的公式
	foundFormula := false
	for row := 2; row <= 5; row++ { // 检查前 4 个数据行
		ref, _ := excelizeCoord(totalColIdx+1, row)
		formula, err := r.CellFormula(sheets[0], ref)
		if err == nil && formula != "" {
			foundFormula = true
			t.Logf("输出文件 %s 是公式: %q ✅", ref, formula)
			break
		}
	}
	if !foundFormula {
		t.Errorf("输出文件\"总分\"列前 4 行都不是公式 — 公式被错误地静态化了")
	}
}

func excelizeCoord(col, row int) (string, error) {
	// 简化版 excelize.CoordinatesToCellName，避免引入 excelize 直接依赖
	// 仅支持 col <= 26，足够测试用
	if col < 1 || col > 26 {
		return "", os.ErrInvalid
	}
	return string(rune('A'+col-1)) + itoaSimple(row), nil
}

func itoaSimple(i int) string {
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

// findSmokeFixture 从测试运行目录向上找 testdata_smoke/<name>，
// 最多向上 6 层；找不到返回 ""。
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
