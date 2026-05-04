package excelio

// 这是临时 PoC 测试，验证 github.com/thedatashed/xlsxreader 在我们数据上
// 相比 excelize 的扫描加速比。目标：100k 行 fixture 扫描耗时下降 ≥ 2×，
// 且两个引擎命中的行号集合必须**完全一致**；否则不正式接入。
//
// 运行门控：默认不跑，避免污染 `go test ./...`：
//
//	$env:EXCEL_MASTER_POC=1
//	$env:EXCEL_MASTER_POC_FILE='testdata_smoke\01_学生信息_10万行.xlsx'
//	$env:EXCEL_MASTER_POC_KW='汉族'
//	go test ./internal/excelio/ -run TestPoC_XlsxreaderVsExcelize -v -timeout=5m
//
// 验收通过后本文件不删除，留作"每次改 excelio 后可回放的基准"。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/thedatashed/xlsxreader"
)

// pocColumnIndex 把列字母（"A"=0, "Z"=25, "AA"=26, "AB"=27）转成 0-based 索引。
// xlsxreader Cell.Column 是字母形式，需要转成切片下标才能和 excelize 的 []string 对齐。
func pocColumnIndex(col string) int {
	idx := 0
	for i := 0; i < len(col); i++ {
		c := col[i]
		if c < 'A' || c > 'Z' {
			return -1
		}
		idx = idx*26 + int(c-'A') + 1
	}
	return idx - 1
}

// pocRowToCells 把 xlsxreader.Row 的稀疏 Cell 列表对齐到 numCols 长度的 []string。
// xlsxreader 会跳过空 cell（README 明说），所以要按 Column 字母回填。
// 超出 numCols 的列被丢弃（对应 excelize 的行为：空尾列会被 trim）。
func pocRowToCells(row xlsxreader.Row, numCols int) []string {
	out := make([]string, numCols)
	for _, c := range row.Cells {
		idx := pocColumnIndex(c.Column)
		if idx < 0 || idx >= numCols {
			continue
		}
		out[idx] = c.Value
	}
	return out
}

// pocContainsAny 等价于 extractor 里的 contains 匹配：任一 cell 包含任一关键词即命中。
// 为让对比"只测两个读引擎的差异"，这里故意写得最朴素，不走 matcher.Engine。
func pocContainsAny(cells []string, keywords []string) bool {
	for _, c := range cells {
		if c == "" {
			continue
		}
		for _, kw := range keywords {
			if strings.Contains(c, kw) {
				return true
			}
		}
	}
	return false
}

// pocScanWithExcelize 复刻 extractor.processFile 的扫描循环（去掉图片/公式/filter），
// 用 excelize.Rows + Columns 迭代，返回命中行号（1-based）和总耗时。
func pocScanWithExcelize(t *testing.T, path, sheet string, headerRow int, keywords []string) ([]int, time.Duration, int) {
	t.Helper()
	r, err := Open(path)
	if err != nil {
		t.Fatalf("excelize Open: %v", err)
	}
	defer r.Close()

	// 用 header 行推断列数，和 extractor 真实路径一致
	hdr, err := r.Header(sheet, headerRow)
	if err != nil {
		t.Fatalf("excelize Header: %v", err)
	}
	numCols := len(hdr)

	it, err := r.Iterate(sheet)
	if err != nil {
		t.Fatalf("excelize Iterate: %v", err)
	}
	defer it.Close()

	var hits []int
	totalRows := 0
	start := time.Now()
	for it.Next() {
		if it.RowNum() <= headerRow {
			continue
		}
		totalRows++
		cells, err := it.Columns()
		if err != nil {
			t.Fatalf("excelize Columns: %v", err)
		}
		// 对齐到 numCols，和 xlsxreader 侧比较公平
		if len(cells) > numCols {
			cells = cells[:numCols]
		}
		if pocContainsAny(cells, keywords) {
			hits = append(hits, it.RowNum())
		}
	}
	if err := it.Err(); err != nil {
		t.Fatalf("excelize iter err: %v", err)
	}
	elapsed := time.Since(start)
	sort.Ints(hits)
	return hits, elapsed, totalRows
}

// pocScanWithXlsxreader 用 xlsxreader 做同样的扫描。返回命中行号和耗时。
func pocScanWithXlsxreader(t *testing.T, path, sheet string, headerRow, numCols int, keywords []string) ([]int, time.Duration, int) {
	t.Helper()
	xl, err := xlsxreader.OpenFile(path)
	if err != nil {
		t.Fatalf("xlsxreader OpenFile: %v", err)
	}
	defer xl.Close()

	// 验证 sheet 存在
	found := false
	for _, s := range xl.Sheets {
		if s == sheet {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("xlsxreader: sheet %q 不在 %v 中", sheet, xl.Sheets)
	}

	var hits []int
	totalRows := 0
	start := time.Now()
	for row := range xl.ReadRows(sheet) {
		if row.Error != nil {
			t.Fatalf("xlsxreader row err at %d: %v", row.Index, row.Error)
		}
		if row.Index <= headerRow {
			continue
		}
		totalRows++
		cells := pocRowToCells(row, numCols)
		if pocContainsAny(cells, keywords) {
			hits = append(hits, row.Index)
		}
	}
	elapsed := time.Since(start)
	sort.Ints(hits)
	return hits, elapsed, totalRows
}

// TestPoC_XlsxreaderVsExcelize 是 PoC 验收测试。门控环境变量 EXCEL_MASTER_POC=1。
// 读 fixture 文件（默认 01_学生信息_10万行.xlsx），用关键词 "汉族" contains 扫描，
// 对比两个读引擎的耗时和命中行号一致性。
func TestPoC_XlsxreaderVsExcelize(t *testing.T) {
	if os.Getenv("EXCEL_MASTER_POC") != "1" {
		t.Skip("set EXCEL_MASTER_POC=1 to run this PoC benchmark")
	}

	fixture := os.Getenv("EXCEL_MASTER_POC_FILE")
	if fixture == "" {
		fixture = filepath.Join("..", "..", "testdata_smoke", "01_学生信息_10万行.xlsx")
	}
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture not found: %s", fixture)
	}

	kwRaw := os.Getenv("EXCEL_MASTER_POC_KW")
	if kwRaw == "" {
		kwRaw = "汉族"
	}
	keywords := strings.Split(kwRaw, "|")
	for i := range keywords {
		keywords[i] = strings.TrimSpace(keywords[i])
	}

	headerRow := 1
	if v := os.Getenv("EXCEL_MASTER_POC_HEADER"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &headerRow); err != nil || n != 1 {
			t.Fatalf("invalid EXCEL_MASTER_POC_HEADER: %s", v)
		}
	}

	// 先用 excelize 探 sheet 名和 numCols，保证两路对齐到同一列数
	r, err := Open(fixture)
	if err != nil {
		t.Fatalf("probe Open: %v", err)
	}
	sheets := r.SheetNames()
	if len(sheets) == 0 {
		r.Close()
		t.Fatalf("fixture 没有 Sheet: %s", fixture)
	}
	sheet := sheets[0]
	hdr, err := r.Header(sheet, headerRow)
	if err != nil {
		r.Close()
		t.Fatalf("probe Header: %v", err)
	}
	numCols := len(hdr)
	r.Close()

	t.Logf("[PoC] fixture=%s sheet=%q headerRow=%d numCols=%d keywords=%v",
		fixture, sheet, headerRow, numCols, keywords)

	// excelize 路径
	hitsA, timeA, totalA := pocScanWithExcelize(t, fixture, sheet, headerRow, keywords)
	t.Logf("[PoC] excelize:   耗时 %v, 扫描 %d 行, 命中 %d 行", timeA.Round(time.Millisecond), totalA, len(hitsA))

	// xlsxreader 路径
	hitsB, timeB, totalB := pocScanWithXlsxreader(t, fixture, sheet, headerRow, numCols, keywords)
	t.Logf("[PoC] xlsxreader: 耗时 %v, 扫描 %d 行, 命中 %d 行", timeB.Round(time.Millisecond), totalB, len(hitsB))

	// 命中行号集合必须完全一致（这是接入新引擎的前置条件）
	if len(hitsA) != len(hitsB) {
		t.Fatalf("命中数不一致: excelize=%d, xlsxreader=%d", len(hitsA), len(hitsB))
	}
	for i := range hitsA {
		if hitsA[i] != hitsB[i] {
			// 找出第一个差异位置附近列出来
			lo := i - 3
			if lo < 0 {
				lo = 0
			}
			hi := i + 3
			if hi > len(hitsA) {
				hi = len(hitsA)
			}
			t.Fatalf("命中行号不一致，位置 %d: excelize=%v vs xlsxreader=%v", i, hitsA[lo:hi], hitsB[lo:hi])
		}
	}

	// 加速比
	speedup := float64(timeA) / float64(timeB)
	t.Logf("[PoC] ✅ 命中行号完全一致（%d 行），加速比 excelize/xlsxreader = %.2fx", len(hitsA), speedup)
	if speedup < 1.5 {
		t.Logf("[PoC] ⚠️  加速不足 1.5×，不值得切换引擎")
	} else if speedup < 2.0 {
		t.Logf("[PoC] ⚠️  加速 1.5-2×，可接入但收益有限")
	} else {
		t.Logf("[PoC] ✅ 加速 ≥ 2×，建议正式接入")
	}
}
