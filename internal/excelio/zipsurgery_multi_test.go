package excelio

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// 验证 multi-sheet zip 手术：
//   - 保留多个 sheet
//   - 每个 sheet 各自的 keepRows（nil 表示全保留，切片表示过滤）
//   - 没列在 keepSheetRows 里的 sheet 整体被删（含 [Content_Types].xml / workbook.xml.rels 清理）

func TestCloneAndExtractZipMulti_KeepBothSheets(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "multi_both.xlsx")

	// 保留 A 的第 1、3 行 + B 全部
	err := CloneAndExtractZipMulti(src, dst, map[string][]int{
		"A": {1, 3},
		"B": nil,
	})
	if err != nil {
		t.Fatalf("CloneAndExtractZipMulti err=%v", err)
	}

	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatalf("打开输出失败: %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) != 2 {
		t.Fatalf("期望 2 个 sheet，实际 %d: %v", len(sheets), sheets)
	}

	rowsA, _ := f.GetRows("A")
	if len(rowsA) != 2 {
		t.Fatalf("Sheet A 期望 2 行（表头 + 香蕉），实际 %d 行: %v", len(rowsA), rowsA)
	}
	if rowsA[0][0] != "名称" || rowsA[1][0] != "香蕉" {
		t.Fatalf("Sheet A 内容错: %v", rowsA)
	}

	rowsB, _ := f.GetRows("B")
	if len(rowsB) != 1 || rowsB[0][0] != "单列" {
		t.Fatalf("Sheet B 应保留全部（仅表头一行），实际 %v", rowsB)
	}
}

func TestCloneAndExtractZipMulti_DropOtherSheet(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "multi_drop.xlsx")

	// 只保留 A，删 B（B 不出现在 keepSheetRows 里）
	err := CloneAndExtractZipMulti(src, dst, map[string][]int{
		"A": nil,
	})
	if err != nil {
		t.Fatalf("CloneAndExtractZipMulti err=%v", err)
	}
	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatalf("打开输出失败: %v", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "A" {
		t.Fatalf("应只保留 A，实际 %v", sheets)
	}
	rowsA, _ := f.GetRows("A")
	if len(rowsA) != 6 {
		t.Fatalf("Sheet A 应保留全部 6 行，实际 %d", len(rowsA))
	}
}

func TestCloneAndExtractZipMulti_RejectEmpty(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "multi_empty.xlsx")
	if err := CloneAndExtractZipMulti(src, dst, nil); err == nil {
		t.Fatal("期望 NO_KEEP_SHEETS，实际 nil")
	}
	if err := CloneAndExtractZipMulti(src, dst, map[string][]int{}); err == nil {
		t.Fatal("期望 NO_KEEP_SHEETS，实际 nil")
	}
}

func TestCloneAndExtractZipMulti_RejectMissingSheet(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "multi_missing.xlsx")
	err := CloneAndExtractZipMulti(src, dst, map[string][]int{
		"NonExistent": {1, 2},
	})
	if err == nil {
		t.Fatal("期望报 SHEET_NOT_FOUND，实际 nil")
	}
}

// 单 sheet API（CloneAndExtractZip）回归：现在内部走 multi 实现，确认行为不变。
func TestCloneAndExtractZip_BackwardCompat(t *testing.T) {
	dir := t.TempDir()
	src := makeTempXlsx(t, dir)
	dst := filepath.Join(dir, "single.xlsx")
	if err := CloneAndExtractZip(src, dst, "A", []int{1, 2}); err != nil {
		t.Fatalf("err=%v", err)
	}
	f, err := excelize.OpenFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "A" {
		t.Fatalf("应只保留 A，实际 %v", sheets)
	}
	rowsA, _ := f.GetRows("A")
	if len(rowsA) != 2 {
		t.Fatalf("应 2 行，实际 %d", len(rowsA))
	}
}
