package extractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// 工具：构造一个 UTF-8 CSV 临时文件并返回路径。
func tempCSV(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return p
}

// per_keyword 通路：CSV 输入命中行被写到一个 .xlsx，按关键词分文件。
func TestExtract_CSV_PerKeyword(t *testing.T) {
	csv := tempCSV(t, "src.csv",
		"姓名,年级,班级\n"+
			"张三,六年级,一班\n"+
			"李四,七年级,二班\n"+
			"王五,六年级,三班\n"+
			"赵六,八年级,四班\n",
	)
	outDir := t.TempDir()

	task := core.ExtractTask{
		Keywords:      []string{"六年级"},
		MatchMode:     core.MatchExact | core.MatchContains,
		HeaderRow:     1,
		Output:        core.OutputPerKeyword,
		OutputDir:     outDir,
		SearchAllCols: true,
	}
	files, err := ScanFile(csv, 1, nil)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	res, err := ExtractUnits(context.Background(), task, files, nil)
	if err != nil {
		t.Fatalf("ExtractUnits: %v", err)
	}
	if res.RowsMatched != 2 {
		t.Fatalf("RowsMatched=%d want=2", res.RowsMatched)
	}
	if len(res.OutputFiles) != 1 {
		t.Fatalf("OutputFiles=%v", res.OutputFiles)
	}

	// 打开输出 xlsx 验证内容
	r, err := excelio.Open(res.OutputFiles[0])
	if err != nil {
		t.Fatalf("open out: %v", err)
	}
	defer r.Close()
	sheets := r.SheetNames()
	if len(sheets) == 0 {
		t.Fatalf("output has no sheet")
	}
	it, err := r.Iterate(sheets[0])
	if err != nil {
		t.Fatalf("iterate out: %v", err)
	}
	defer it.Close()
	var rows [][]string
	for it.Next() {
		cols, _ := it.Columns()
		rows = append(rows, append([]string(nil), cols...))
	}
	if len(rows) < 3 {
		t.Fatalf("rows=%d want >=3", len(rows))
	}
	got := map[string]bool{}
	for _, row := range rows[1:] {
		if len(row) > 0 {
			got[row[0]] = true
		}
	}
	if !got["张三"] || !got["王五"] {
		t.Fatalf("命中行错: rows=%v", rows)
	}
}

// per_source 对 CSV 源走 exportOneCSV（流式 StreamWriter），不调 zip 手术。
func TestExtract_CSV_PerSource(t *testing.T) {
	csv := tempCSV(t, "x.csv",
		"name,grade\nalice,1st\nbob,2nd\ncarol,1st\n",
	)
	outDir := t.TempDir()
	task := core.ExtractTask{
		Keywords:      []string{"1st"},
		MatchMode:     core.MatchExact | core.MatchContains,
		HeaderRow:     1,
		Output:        core.OutputPerSource,
		OutputDir:     outDir,
		SearchAllCols: true,
	}
	files, err := ScanFile(csv, 1, nil)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	res, err := ExtractUnits(context.Background(), task, files, nil)
	if err != nil {
		t.Fatalf("ExtractUnits: %v", err)
	}
	if res.RowsMatched != 2 {
		t.Fatalf("RowsMatched=%d want=2", res.RowsMatched)
	}
	if len(res.OutputFiles) != 1 {
		t.Fatalf("OutputFiles=%v", res.OutputFiles)
	}
}

// merged 含 CSV 源时退化到 finalizeStreaming 流式合并路径。
func TestExtract_CSV_Merged_TwoFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.csv")
	b := filepath.Join(dir, "b.csv")
	_ = os.WriteFile(a, []byte("name,grade\nalice,1st\n"), 0o644)
	_ = os.WriteFile(b, []byte("name,grade\nbob,1st\ncarol,2nd\n"), 0o644)
	outDir := t.TempDir()

	task := core.ExtractTask{
		FolderPath:    dir,
		Keywords:      []string{"1st"},
		MatchMode:     core.MatchExact | core.MatchContains,
		HeaderRow:     1,
		Output:        core.OutputMerged,
		OutputDir:     outDir,
		SearchAllCols: true,
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.RowsMatched != 2 {
		t.Fatalf("RowsMatched=%d want=2", res.RowsMatched)
	}
	if len(res.OutputFiles) != 1 {
		t.Fatalf("OutputFiles=%v", res.OutputFiles)
	}
}

// Extract() 入口：文件夹含 CSV 能被发现并跑完 per_keyword 通路。
func TestExtract_CSV_FolderScan_PerKeyword(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.csv"),
		[]byte("name,grade\nalice,1st\nbob,2nd\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.csv"),
		[]byte("name,grade\ncarol,1st\n"), 0o644)
	outDir := t.TempDir()

	task := core.ExtractTask{
		FolderPath:    dir,
		Keywords:      []string{"1st"},
		MatchMode:     core.MatchExact | core.MatchContains,
		HeaderRow:     1,
		Output:        core.OutputPerKeyword,
		OutputDir:     outDir,
		SearchAllCols: true,
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.RowsMatched != 2 {
		t.Fatalf("RowsMatched=%d want=2", res.RowsMatched)
	}
}
