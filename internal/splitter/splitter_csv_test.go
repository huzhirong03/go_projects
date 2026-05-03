package splitter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"excel-master/internal/core"
)

func tempCSV(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return p
}

// by_sheet + CSV 源 → 友好拒绝。
func TestSplit_BySheet_CSV_Rejected(t *testing.T) {
	csv := tempCSV(t, "x.csv", "a,b\n1,2\n")
	_, err := SplitBySheet(context.Background(), core.SplitTask{
		SourcePath: csv,
		OutputDir:  t.TempDir(),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), core.CodeSourceFormatUnsupported) {
		t.Fatalf("want unsupported error, got %v", err)
	}
}

// by_rows + CSV → 友好拒绝。
func TestSplit_ByRows_CSV_Rejected(t *testing.T) {
	csv := tempCSV(t, "x.csv", "a,b\n1,2\n")
	_, err := SplitByRows(context.Background(), core.SplitTask{
		SourcePath:  csv,
		OutputDir:   t.TempDir(),
		RowsPerFile: 10,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), core.CodeSourceFormatUnsupported) {
		t.Fatalf("want unsupported error, got %v", err)
	}
}

// by_column + CSV → 友好拒绝。
func TestSplit_ByColumn_CSV_Rejected(t *testing.T) {
	csv := tempCSV(t, "x.csv", "a,b\n1,2\n")
	_, err := SplitByColumn(context.Background(), core.SplitTask{
		SourcePath:  csv,
		OutputDir:   t.TempDir(),
		HeaderRow:   1,
		SplitColumn: "a",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), core.CodeSourceFormatUnsupported) {
		t.Fatalf("want unsupported error, got %v", err)
	}
}

// by_keyword + CSV → 正常工作（复用 extractor 路径）。
func TestSplit_ByKeyword_CSV_OK(t *testing.T) {
	csv := tempCSV(t, "x.csv", "name,grade\nalice,1st\nbob,2nd\ncarol,1st\n")
	res, err := SplitByKeyword(context.Background(), core.SplitTask{
		SourcePath:    csv,
		OutputDir:     t.TempDir(),
		HeaderRow:     1,
		Keywords:      []string{"1st"},
		MatchMode:     core.MatchExact | core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputPerKeyword,
	}, nil)
	if err != nil {
		t.Fatalf("SplitByKeyword: %v", err)
	}
	if res.RowsScanned != 2 {
		t.Fatalf("RowsScanned=%d want=2", res.RowsScanned)
	}
}
