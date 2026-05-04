package excelio

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildSparseFixture 构造一行只在 A 和 C 列写入、B 列为空的 xlsx，
// 用来验证 FastRowIterator.Columns() 把稀疏 cell 填补成密集 [A, "", C]。
func buildSparseFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sparse.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "数据"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")
	_ = f.SetCellValue(sheet, "A1", "X")
	// 故意跳过 B1
	_ = f.SetCellValue(sheet, "C1", "Y")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

// TestFastReader_BasicIterate 验证 FastRowIterator 能流式读出所有行 + 行号。
func TestFastReader_BasicIterate(t *testing.T) {
	path := buildFixture(t, 5)
	r, err := OpenFast(path)
	if err != nil {
		t.Fatalf("OpenFast: %v", err)
	}
	defer r.Close()

	if got := r.SheetNames(); len(got) == 0 || got[0] != "数据" {
		t.Fatalf("SheetNames = %v, want [数据]", got)
	}

	it, err := r.Iterate("数据")
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	defer it.Close()

	rows := 0
	for it.Next() {
		cells, err := it.Columns()
		if err != nil {
			t.Fatalf("Columns: %v", err)
		}
		// fixture 是 3 列：产品名/数量/价格
		if len(cells) < 3 {
			t.Errorf("row %d cells %v 长度应 >= 3", it.RowNum(), cells)
		}
		rows++
	}
	if err := it.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	// fixture 是 1 表头 + 5 数据 = 6 行
	if rows != 6 {
		t.Errorf("总行数 = %d, want 6", rows)
	}
}

// TestFastReader_UnknownSheet 不存在的 sheet 应返回错误而非 panic。
func TestFastReader_UnknownSheet(t *testing.T) {
	path := buildFixture(t, 1)
	r, err := OpenFast(path)
	if err != nil {
		t.Fatalf("OpenFast: %v", err)
	}
	defer r.Close()

	_, err = r.Iterate("不存在的sheet")
	if err == nil {
		t.Fatalf("不存在的 sheet 应报错")
	}
}

// TestFastReader_EquivalentToExcelize 关键回归：FastRowIterator 和 RowIterator
// 对同一 fixture 必须产出**完全相同**的命中行号集合。
//
// 这是 A 接入 extractor 的前置条件——业务语义零回归。
func TestFastReader_EquivalentToExcelize(t *testing.T) {
	path := buildFixture(t, 50)

	// 用 excelize 路径
	rA, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rA.Close()
	itA, err := rA.Iterate("数据")
	if err != nil {
		t.Fatalf("Iterate excelize: %v", err)
	}
	defer itA.Close()
	var hitsA []int
	for itA.Next() {
		cells, _ := itA.Columns()
		// 简单匹配：第二列 "数量" == "10" 视为命中
		if len(cells) >= 2 && cells[1] == "10" {
			hitsA = append(hitsA, itA.RowNum())
		}
	}
	if err := itA.Err(); err != nil {
		t.Fatalf("itA.Err: %v", err)
	}

	// 用 xlsxreader 路径
	rB, err := OpenFast(path)
	if err != nil {
		t.Fatalf("OpenFast: %v", err)
	}
	defer rB.Close()
	itB, err := rB.Iterate("数据")
	if err != nil {
		t.Fatalf("Iterate fast: %v", err)
	}
	defer itB.Close()
	var hitsB []int
	for itB.Next() {
		cells, _ := itB.Columns()
		if len(cells) >= 2 && cells[1] == "10" {
			hitsB = append(hitsB, itB.RowNum())
		}
	}
	if err := itB.Err(); err != nil {
		t.Fatalf("itB.Err: %v", err)
	}

	if len(hitsA) != len(hitsB) {
		t.Fatalf("命中数不一致: excelize=%d, xlsxreader=%d (rowsA=%v rowsB=%v)",
			len(hitsA), len(hitsB), hitsA, hitsB)
	}
	for i := range hitsA {
		if hitsA[i] != hitsB[i] {
			t.Errorf("行号不一致 [%d]: excelize=%d, xlsxreader=%d", i, hitsA[i], hitsB[i])
		}
	}
}

// TestFastReader_DenseColumns 验证 Columns() 返回值是从 A 列开始的密集切片。
// xlsxreader 原始 Cells 是稀疏的，我们的 wrapper 必须把空 cell 填 ""。
func TestFastReader_DenseColumns(t *testing.T) {
	// 用一个空格列的 fixture：只在 A 和 C 写入
	path := buildSparseFixture(t)
	r, err := OpenFast(path)
	if err != nil {
		t.Fatalf("OpenFast: %v", err)
	}
	defer r.Close()

	it, err := r.Iterate("数据")
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	defer it.Close()

	if !it.Next() {
		t.Fatalf("应能读到第 1 行")
	}
	cells, _ := it.Columns()
	if len(cells) < 3 {
		t.Fatalf("Columns 应为密集 3 列（A B C），got %v", cells)
	}
	if cells[0] != "X" || cells[1] != "" || cells[2] != "Y" {
		t.Errorf("密集填充错: got %v want [X, '', Y]", cells)
	}
}
