package extractor

import (
	"context"
	"path/filepath"
	"testing"

	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"
)

// buildDedupFolder 构造一个**故意含重复**的双文件夹：
//
//	file1.xlsx: 产品名,价格
//	  口红A, 10
//	  口红B, 20
//	  眼影C, 30
//
//	file2.xlsx: 产品名,价格
//	  口红A, 99    <-- 跨文件重复（产品名 = file1 第一行）
//	  口红D, 40
//	  眼影C, 88    <-- 跨文件重复（产品名 = file1 第三行）
//
// 用于验证 merged 全局去重 / per_source 文件内去重 / per_keyword 关键词内去重 三套语义。
func buildDedupFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeXlsx := func(name string, headers []string, rows [][]any) {
		f := excelize.NewFile()
		defer f.Close()
		const sheet = "Sheet1"
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
		if err := f.SaveAs(filepath.Join(dir, name)); err != nil {
			t.Fatalf("SaveAs %s: %v", name, err)
		}
	}
	writeXlsx("file1.xlsx", []string{"产品名", "价格"}, [][]any{
		{"口红A", 10.0}, {"口红B", 20.0}, {"眼影C", 30.0},
	})
	writeXlsx("file2.xlsx", []string{"产品名", "价格"}, [][]any{
		{"口红A", 99.0}, {"口红D", 40.0}, {"眼影C", 88.0},
	})
	return dir
}

// countDataRowsXlsx 打开 xlsx 计数指定 sheet 的非表头行数（行数 - 1）。
func countDataRowsXlsx(t *testing.T, path string, sheet string) int {
	t.Helper()
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("rows %s: %v", sheet, err)
	}
	if len(rows) == 0 {
		return 0
	}
	return len(rows) - 1 // 减表头
}

// TestExtract_Dedup_Merged：merged 策略下全局按"产品名"去重。
// 输入 6 行（file1 三行 + file2 三行），其中 2 个产品名跨文件重复。
// 期望输出唯一 4 行（口红A、口红B、眼影C、口红D），保留首次出现。
func TestExtract_Dedup_Merged(t *testing.T) {
	src := buildDedupFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    src,
		Keywords:      []string{"口红", "眼影"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputMerged,
		OutputDir:     out,
		HeaderRow:     1,
		DedupColumn:   "产品名",
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.OutputFiles) != 1 {
		t.Fatalf("merged 应输出 1 个文件，实际 %d", len(res.OutputFiles))
	}
	// merged 走 zip surgery，sheet 名继承源文件 Sheet1。
	got := countDataRowsXlsx(t, res.OutputFiles[0], "Sheet1")
	if got != 4 {
		t.Errorf("merged + dedup 全局应得 4 行（去掉跨文件重复 2 行），实际 %d", got)
	}
}

// TestExtract_Dedup_PerSource：per_source 策略下每个源文件内部去重。
// file1 / file2 内部都没有自重复，所以去重不影响行数；输出 2 个文件，每个文件 3 行。
func TestExtract_Dedup_PerSource(t *testing.T) {
	src := buildDedupFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    src,
		Keywords:      []string{"口红", "眼影"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputPerSource,
		OutputDir:     out,
		HeaderRow:     1,
		DedupColumn:   "产品名",
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.OutputFiles) != 2 {
		t.Fatalf("per_source 应输出 2 个文件，实际 %d", len(res.OutputFiles))
	}
	for _, p := range res.OutputFiles {
		got := countDataRowsXlsx(t, p, "Sheet1")
		if got != 3 {
			t.Errorf("per_source + dedup 每文件 3 行（无源内重复），实际 %s = %d", filepath.Base(p), got)
		}
	}
}

// TestExtract_Dedup_PerKeyword：per_keyword 策略下每个关键词文件内独立去重。
// 关键词"口红"：file1{口红A,口红B} + file2{口红A,口红D} = 4 命中，"产品名" 去重后保留 3 个（口红A,口红B,口红D）
// 关键词"眼影"：file1{眼影C} + file2{眼影C} = 2 命中，"产品名" 去重后保留 1 个（眼影C）
func TestExtract_Dedup_PerKeyword(t *testing.T) {
	src := buildDedupFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    src,
		Keywords:      []string{"口红", "眼影"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputPerKeyword,
		OutputDir:     out,
		HeaderRow:     1,
		DedupColumn:   "产品名",
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.OutputFiles) != 2 {
		t.Fatalf("per_keyword 应输出 2 个文件，实际 %d", len(res.OutputFiles))
	}
	// 文件名包含关键词，方便区分
	want := map[string]int{"口红": 3, "眼影": 1}
	for _, p := range res.OutputFiles {
		base := filepath.Base(p)
		// per_keyword writer 默认 sheet 名 "结果"
		got := countDataRowsXlsx(t, p, "结果")
		matched := false
		for kw, expected := range want {
			if containsAny(base, kw) {
				if got != expected {
					t.Errorf("kw=%s 期望 %d 行，实际 %d (%s)", kw, expected, got, base)
				}
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("无法识别输出文件归属哪个关键词: %s", base)
		}
	}
}

// TestExtract_Dedup_ColumnNotFound：去重列在所有文件里都不存在 → no-op，输出跟未启用一致。
func TestExtract_Dedup_ColumnNotFound(t *testing.T) {
	src := buildDedupFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    src,
		Keywords:      []string{"口红", "眼影"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputMerged,
		OutputDir:     out,
		HeaderRow:     1,
		DedupColumn:   "不存在的列",
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := countDataRowsXlsx(t, res.OutputFiles[0], "Sheet1")
	if got != 6 {
		t.Errorf("dedup 列不存在时应零回归（6 行全保留），实际 %d", got)
	}
}

// containsAny 判断 s 是否包含 sub（简单 wrapper，便于测试可读性）。
func containsAny(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ======================================================================
// V1.2 集成测试：多列组合去重 + 忽略空白/大小写
// ======================================================================

// buildDedupFolderV12 构造 V1.2 测试用的文件夹：一个文件，带故意的"空白/大小写/多列"混杂。
//
//	file1.xlsx: 品牌,型号,价格
//	  Apple,   iPhone,   100     <- 基准
//	  APPLE,   IPHONE,   200     <- 与上行：大小写不同
//	  apple ,   iphone ,  300    <- 与上行：大小写+前后空白
//	  Apple,   Watch,    400     <- 与第一行：型号不同（多列组合才能保留）
//	  Xiaomi,  Mi10,     500     <- 无重复
func buildDedupFolderV12(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"
	headers := []string{"品牌", "型号", "价格"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	rows := [][]any{
		{"Apple", "iPhone", 100.0},
		{"APPLE", "IPHONE", 200.0},
		{"apple ", "iphone ", 300.0},
		{"Apple", "Watch", 400.0},
		{"Xiaomi", "Mi10", 500.0},
	}
	for ri, r := range rows {
		for ci, v := range r {
			cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	if err := f.SaveAs(filepath.Join(dir, "v12.xlsx")); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return dir
}

// TestExtract_Dedup_MultiCol_Merged：双列组合（品牌+型号）全局去重，严格比较（无归一化）
// 输入 5 行，按 strict 比较：
//
//	(Apple,iPhone), (APPLE,IPHONE), (apple ,iphone ), (Apple,Watch), (Xiaomi,Mi10)
//
// 5 组都不相等 → 全保留 5 行
func TestExtract_Dedup_MultiCol_Merged(t *testing.T) {
	src := buildDedupFolderV12(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    src,
		Keywords:      []string{"iPhone", "IPHONE", "iphone", "Watch", "Mi10"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputMerged,
		OutputDir:     out,
		HeaderRow:     1,
		DedupColumns:  []string{"品牌", "型号"},
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := countDataRowsXlsx(t, res.OutputFiles[0], "Sheet1")
	if got != 5 {
		t.Errorf("严格双列去重应全保留 5 行，实际 %d", got)
	}
}

// TestExtract_Dedup_MultiCol_IgnoreCase：双列 + 忽略大小写
// strict 下 5 行；忽略大小写后 Apple/APPLE、iPhone/IPHONE 视作同 → (Apple,iPhone) 有两行
// 预期：第 2 行被 drop，保留 4 行
func TestExtract_Dedup_MultiCol_IgnoreCase(t *testing.T) {
	src := buildDedupFolderV12(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:      src,
		Keywords:        []string{"iPhone", "IPHONE", "iphone", "Watch", "Mi10"},
		MatchMode:       core.MatchContains,
		SearchAllCols:   true,
		Output:          core.OutputMerged,
		OutputDir:       out,
		HeaderRow:       1,
		DedupColumns:    []string{"品牌", "型号"},
		DedupIgnoreCase: true,
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := countDataRowsXlsx(t, res.OutputFiles[0], "Sheet1")
	// 行 2 (APPLE,IPHONE) 被 drop（与行 1 同）；行 3 (apple ,iphone ) 保留（trailing space 让 strict 比较后不同）
	if got != 4 {
		t.Errorf("忽略大小写后应保留 4 行，实际 %d", got)
	}
}

// TestExtract_Dedup_MultiCol_IgnoreSpaceAndCase：双列 + 同时忽略空白+大小写
// 此时 Apple/APPLE/apple、iPhone/IPHONE/iphone（带空白）全视作同 → (Apple,iPhone) 有 3 行
// 预期：第 2、3 行被 drop，保留 3 行（首 + Watch + Mi10）
func TestExtract_Dedup_MultiCol_IgnoreSpaceAndCase(t *testing.T) {
	src := buildDedupFolderV12(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:       src,
		Keywords:         []string{"iPhone", "IPHONE", "iphone", "Watch", "Mi10"},
		MatchMode:        core.MatchContains,
		SearchAllCols:    true,
		Output:           core.OutputMerged,
		OutputDir:        out,
		HeaderRow:        1,
		DedupColumns:     []string{"品牌", "型号"},
		DedupIgnoreSpace: true,
		DedupIgnoreCase:  true,
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := countDataRowsXlsx(t, res.OutputFiles[0], "Sheet1")
	if got != 3 {
		t.Errorf("双开关后应保留 3 行，实际 %d", got)
	}
}

// TestExtract_Dedup_BackwardCompat_V11：只填 DedupColumn（V1.1 单列字段）仍生效
// V1.1 样例：单列"产品名"对 6 行输入去重，保留 4 行（跟 TestExtract_Dedup_Merged 一致语义）。
func TestExtract_Dedup_BackwardCompat_V11(t *testing.T) {
	src := buildDedupFolder(t) // 复用 V1.1 fixture
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    src,
		Keywords:      []string{"口红", "眼影"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputMerged,
		OutputDir:     out,
		HeaderRow:     1,
		DedupColumn:   "产品名", // 老字段，DedupColumns 留空
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := countDataRowsXlsx(t, res.OutputFiles[0], "Sheet1")
	if got != 4 {
		t.Errorf("V1.1 单列字段应向后兼容（预期 4 行），实际 %d", got)
	}
}

// TestExtract_Dedup_V11_and_V12_Coexist：DedupColumn 和 DedupColumns 同时填
// buildDedupConfig 会合并去重：结果等价于"产品名 + 价格"双列。
// 6 行数据：
//
//	(口红A,10), (口红B,20), (眼影C,30), (口红A,99), (口红D,40), (眼影C,88)
//
// 6 个 (产品名,价格) 组合都不相等 → 全保留 6 行。
func TestExtract_Dedup_V11_and_V12_Coexist(t *testing.T) {
	src := buildDedupFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath:    src,
		Keywords:      []string{"口红", "眼影"},
		MatchMode:     core.MatchContains,
		SearchAllCols: true,
		Output:        core.OutputMerged,
		OutputDir:     out,
		HeaderRow:     1,
		DedupColumn:   "产品名",
		DedupColumns:  []string{"价格"},
	}
	res, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := countDataRowsXlsx(t, res.OutputFiles[0], "Sheet1")
	if got != 6 {
		t.Errorf("V1.1+V1.2 共用字段：产品名+价格双列全部不同，应保留 6 行，实际 %d", got)
	}
}
