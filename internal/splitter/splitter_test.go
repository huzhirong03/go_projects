package splitter

import (
	"context"
	"path/filepath"
	"testing"

	"excel-master/internal/core"
	"excel-master/internal/excelio"

	"github.com/xuri/excelize/v2"
)

// buildFixture 生成一个有 2 个 Sheet、多行数据的源文件。
func buildFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	writeSheet := func(sheet string, headers []string, rows [][]any) {
		idx, _ := f.NewSheet(sheet)
		f.SetActiveSheet(idx)
		for i, h := range headers {
			cell, _ := excelize.CoordinatesToCellName(i+1, 1)
			_ = f.SetCellValue(sheet, cell, h)
		}
		for ri, r := range rows {
			for ci, v := range r {
				cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
				_ = f.SetCellValue(sheet, cell, v)
			}
		}
	}

	writeSheet("Sheet1",
		[]string{"产品名", "类别", "价格"},
		[][]any{
			{"口红 A", "美妆", 99},
			{"眼影 B", "美妆", 50},
			{"水杯 C", "百货", 30},
			{"笔 D", "文具", 5},
			{"笔 E", "文具", 6},
		},
	)
	writeSheet("库存",
		[]string{"产品名", "库存"},
		[][]any{
			{"口红 A", 100},
			{"眼影 B", 50},
		},
	)

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func TestSplitBySheet(t *testing.T) {
	src := buildFixture(t)
	out := t.TempDir()
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitBySheet,
		OutputDir: out, HeaderRow: 1, PreserveImages: false,
	}
	result, err := SplitBySheet(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitBySheet: %v", err)
	}
	if result.PartsCreated != 2 {
		t.Errorf("PartsCreated = %d, want 2", result.PartsCreated)
	}
	if len(result.OutputFiles) != 2 {
		t.Errorf("OutputFiles = %d, want 2", len(result.OutputFiles))
	}
	for _, p := range result.OutputFiles {
		verifyOpenable(t, p)
	}
}

func TestSplitByRows(t *testing.T) {
	src := buildFixture(t)
	out := t.TempDir()
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByRows, RowsPerFile: 2,
		OutputDir: out, HeaderRow: 1,
	}
	result, err := SplitByRows(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByRows: %v", err)
	}
	// V1.1：默认处理全部 Sheet。
	// Sheet1 5 行 → 3 份 (2+2+1)；库存 2 行 → 1 份 (2)；合计 4 份, 7 行。
	if result.PartsCreated != 4 {
		t.Errorf("PartsCreated = %d, want 4", result.PartsCreated)
	}
	if result.RowsScanned != 7 {
		t.Errorf("RowsScanned = %d, want 7", result.RowsScanned)
	}
	for _, p := range result.OutputFiles {
		verifyHeaderPresent(t, p)
	}
}

func TestSplitByRows_OnlySelectedSheet(t *testing.T) {
	src := buildFixture(t)
	out := t.TempDir()
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByRows, RowsPerFile: 2,
		OutputDir: out, HeaderRow: 1,
		SheetNames: []string{"Sheet1"}, // 只处理 Sheet1
	}
	result, err := SplitByRows(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByRows: %v", err)
	}
	if result.PartsCreated != 3 {
		t.Errorf("PartsCreated = %d, want 3", result.PartsCreated)
	}
	if result.RowsScanned != 5 {
		t.Errorf("RowsScanned = %d, want 5", result.RowsScanned)
	}
}

func TestSplitByColumn(t *testing.T) {
	src := buildFixture(t)
	out := t.TempDir()
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByColumn, SplitColumn: "类别",
		OutputDir: out, HeaderRow: 1,
	}
	result, err := SplitByColumn(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByColumn: %v", err)
	}
	// 类别值：美妆（2）+ 百货（1）+ 文具（2）→ 3 个分组
	if result.PartsCreated != 3 {
		t.Errorf("PartsCreated = %d, want 3", result.PartsCreated)
	}
}

func TestSplitByColumn_MissingColumn(t *testing.T) {
	src := buildFixture(t)
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByColumn, SplitColumn: "不存在的列",
		OutputDir: t.TempDir(), HeaderRow: 1,
	}
	_, err := SplitByColumn(context.Background(), task, nil)
	if err == nil {
		t.Fatal("期望返回列不存在错误")
	}
}

// TestSplitByKeyword 验证按关键词拆分单文件：复用 extractor 引擎，多 Sheet 默认全部参与。
func TestSplitByKeyword(t *testing.T) {
	src := buildFixture(t)
	out := t.TempDir()
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByKeyword,
		OutputDir: out, HeaderRow: 1,
		Keywords:      []string{"口红"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputPerKeyword,
	}
	result, err := SplitByKeyword(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByKeyword: %v", err)
	}
	// "口红 A" 在 Sheet1 第 1 数据行 + 在 库存 第 1 数据行 → 共 2 行命中（多 Sheet 默认全选）
	if result.RowsScanned != 2 {
		t.Errorf("RowsScanned = %d, want 2", result.RowsScanned)
	}
	if result.PartsCreated != 1 {
		t.Errorf("PartsCreated = %d, want 1（per_keyword 单关键词只产 1 个文件）", result.PartsCreated)
	}
	if len(result.OutputFiles) != 1 {
		t.Errorf("OutputFiles = %d, want 1", len(result.OutputFiles))
	}
}

// TestSplitByKeyword_OnlyOneSheet 验证 SheetNames 过滤生效。
func TestSplitByKeyword_OnlyOneSheet(t *testing.T) {
	src := buildFixture(t)
	out := t.TempDir()
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitByKeyword,
		OutputDir: out, HeaderRow: 1,
		SheetNames:    []string{"Sheet1"}, // 只处理 Sheet1
		Keywords:      []string{"口红"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputPerKeyword,
	}
	result, err := SplitByKeyword(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("SplitByKeyword: %v", err)
	}
	if result.RowsScanned != 1 {
		t.Errorf("RowsScanned = %d, want 1（仅 Sheet1 命中 1 行）", result.RowsScanned)
	}
}

func TestSplit_Dispatcher(t *testing.T) {
	src := buildFixture(t)
	task := core.SplitTask{
		SourcePath: src, Mode: core.SplitBySheet,
		OutputDir: t.TempDir(), HeaderRow: 1,
	}
	_, err := Split(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Split dispatcher: %v", err)
	}
}

func verifyOpenable(t *testing.T, path string) {
	t.Helper()
	r, err := excelio.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	_ = r.Close()
}

func verifyHeaderPresent(t *testing.T, path string) {
	t.Helper()
	r, err := excelio.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()
	sheets := r.SheetNames()
	if len(sheets) == 0 {
		t.Fatalf("%s 无 Sheet", path)
	}
	h, err := r.Header(sheets[0], 1)
	if err != nil || len(h) == 0 {
		t.Fatalf("%s 缺表头: %v", path, err)
	}
}
