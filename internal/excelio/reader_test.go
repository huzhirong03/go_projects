package excelio

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildFixture 在临时目录里生成一个最小 xlsx，用于单测。
// 结构：Sheet "数据"，第 1 行表头，随后 n 行数据。
func buildFixture(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "数据"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{"产品名", "数量", "价格"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			t.Fatalf("SetCellValue header: %v", err)
		}
	}
	for row := 2; row <= n+1; row++ {
		_ = f.SetCellValue(sheet, mustCell(1, row), "产品"+itoa(row-1))
		_ = f.SetCellValue(sheet, mustCell(2, row), row-1)
		_ = f.SetCellValue(sheet, mustCell(3, row), float64(row-1)*9.9)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func mustCell(col, row int) string {
	c, _ := excelize.CoordinatesToCellName(col, row)
	return c
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestReaderHeaderAndIterate(t *testing.T) {
	path := buildFixture(t, 5)
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	sheets := r.SheetNames()
	if len(sheets) != 1 || sheets[0] != "数据" {
		t.Fatalf("SheetNames = %v, want [数据]", sheets)
	}

	header, err := r.Header("数据", 1)
	if err != nil {
		t.Fatalf("Header: %v", err)
	}
	want := []string{"产品名", "数量", "价格"}
	if len(header) != len(want) {
		t.Fatalf("Header len = %d, want %d", len(header), len(want))
	}
	for i := range want {
		if header[i] != want[i] {
			t.Errorf("Header[%d] = %q, want %q", i, header[i], want[i])
		}
	}

	it, err := r.Iterate("数据")
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	defer it.Close()

	rows := 0
	for it.Next() {
		cols, err := it.Columns()
		if err != nil {
			t.Fatalf("Columns: %v", err)
		}
		if len(cols) == 0 {
			t.Error("空行")
		}
		rows++
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iter err: %v", err)
	}
	// 1 行表头 + 5 行数据 = 6
	if rows != 6 {
		t.Errorf("rows = %d, want 6", rows)
	}
}
