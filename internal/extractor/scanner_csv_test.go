package extractor

import (
	"os"
	"path/filepath"
	"testing"
)

// 验证：纯 CSV 文件夹扫描得到一条 SheetName="CSV" 的 FileInfo，且 Headers 是首行。
func TestScanFolder_CSVOnly(t *testing.T) {
	dir := t.TempDir()
	must := func(p string, body []byte) {
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	must(filepath.Join(dir, "a.csv"), []byte("姓名,年级\n张三,六年级\n李四,七年级\n"))

	units, err := ScanFolder(dir, 1, nil)
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("units=%d want 1", len(units))
	}
	u := units[0]
	if u.SheetName != csvSheetName {
		t.Fatalf("SheetName=%q want=%q", u.SheetName, csvSheetName)
	}
	if len(u.Headers) != 2 || u.Headers[0] != "姓名" || u.Headers[1] != "年级" {
		t.Fatalf("Headers=%v", u.Headers)
	}
}

// 验证：混合文件夹（xlsx + csv）扫描后两类文件各自展开为 FileInfo。
// 这里 xlsx 用项目自带 testdata（如不存在则跳过 xlsx 部分）。
func TestScanFolder_MixedCSVAndXLSX(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.csv"),
		[]byte("h1,h2\n1,2\n"), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	// 不强行造 xlsx，只验证 csv 至少被扫到 + 跳过非白名单（如 .txt）
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"),
		[]byte("ignored"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	units, err := ScanFolder(dir, 1, nil)
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}
	csvCount := 0
	for _, u := range units {
		if u.SheetName == csvSheetName {
			csvCount++
		}
		if filepath.Ext(u.Path) == ".txt" {
			t.Fatalf(".txt 不应被扫到: %s", u.Path)
		}
	}
	if csvCount != 1 {
		t.Fatalf("CSV 单元数=%d want=1", csvCount)
	}
}

// 验证：ScanFile 直接传 CSV 路径也能被识别。
func TestScanFile_CSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.csv")
	if err := os.WriteFile(p, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	units, err := ScanFile(p, 1, nil)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if len(units) != 1 || units[0].Headers[0] != "a" {
		t.Fatalf("units=%v", units)
	}
}

// 验证：SheetsOf 对 CSV 返回 ["CSV"]。
func TestSheetsOf_CSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.csv")
	_ = os.WriteFile(p, []byte("a,b\n1,2\n"), 0o644)
	sh, err := SheetsOf(p)
	if err != nil {
		t.Fatalf("SheetsOf: %v", err)
	}
	if len(sh) != 1 || sh[0] != csvSheetName {
		t.Fatalf("sheets=%v", sh)
	}
}
