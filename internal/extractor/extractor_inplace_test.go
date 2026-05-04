package extractor

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"excel-master/internal/core"
	"excel-master/internal/pipeline"

	"github.com/xuri/excelize/v2"
)

// onePixelPNGForTest 返回一张合法的 1x1 PNG，用于带图源文件构造。
func onePixelPNGForTest() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
		0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42,
		0x60, 0x82,
	}
}

// buildInplaceSrc 生成一个带图源 xlsx：表头 + 5 行，每行 A 列关键词 + 一张图。
func buildInplaceSrc(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "src.xlsx")
	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "品类")
	_ = f.SetCellValue("Sheet1", "B1", "名称")
	data := []struct{ cat, name string }{
		{"口红", "口红A"},
		{"眼影", "眼影B"},
		{"口红", "口红C"},
		{"面膜", "面膜D"},
		{"口红", "口红E"},
	}
	for i, d := range data {
		row := i + 2
		cellA, _ := excelize.CoordinatesToCellName(1, row)
		cellB, _ := excelize.CoordinatesToCellName(2, row)
		_ = f.SetCellValue("Sheet1", cellA, d.cat)
		_ = f.SetCellValue("Sheet1", cellB, d.name)
		_ = f.AddPictureFromBytes("Sheet1", cellA, &excelize.Picture{
			Extension: ".png",
			File:      onePixelPNGForTest(),
			Format:    &excelize.GraphicOptions{AutoFit: true},
		})
	}
	if err := f.SaveAs(src); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return src
}

func runInplaceExtract(t *testing.T, task core.ExtractTask) *Result {
	t.Helper()
	// task.FolderPath 在测试里复用作源文件路径；用 ScanFile 拿到真正的 FileInfo（带 Headers）
	files, err := ScanFile(task.FolderPath, task.HeaderRow, nil)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	task.FolderPath = ""
	res, err := ExtractUnits(context.Background(), task, files, pipeline.NoopEmitter{})
	if err != nil {
		t.Fatalf("ExtractUnits: %v", err)
	}
	return res
}

// TestExtractInplace_PerKeyword: 每个关键词一个新 Sheet，图片跟随命中行。
func TestExtractInplace_PerKeyword(t *testing.T) {
	dir := t.TempDir()
	src := buildInplaceSrc(t, dir)

	task := core.ExtractTask{
		FolderPath:     src,
		Keywords:       []string{"口红", "眼影"},
		MatchMode:      core.MatchContains,
		SearchAllCols:  true,
		Output:         core.OutputPerKeyword,
		HeaderRow:      1,
		PreserveImages: true,
		FilenamePrefix: "搜索_",
		OutputTarget:   core.OutputTargetInplaceSheets,
	}
	res := runInplaceExtract(t, task)
	if res.RowsMatched != 4 { // 口红 3 + 眼影 1
		t.Fatalf("期望命中 4 行，实际 %d", res.RowsMatched)
	}
	if len(res.OutputFiles) != 1 || res.OutputFiles[0] != src {
		t.Fatalf("OutputFiles 应为 [源文件]，实际 %v", res.OutputFiles)
	}

	f, err := excelize.OpenFile(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	sort.Strings(sheets)
	// 应包含原 Sheet1 + 两个新 Sheet
	got := map[string]bool{}
	for _, s := range sheets {
		got[s] = true
	}
	if !got["Sheet1"] || !got["搜索_口红"] || !got["搜索_眼影"] {
		t.Fatalf("Sheet 列表应包含 Sheet1/搜索_口红/搜索_眼影，实际 %v", sheets)
	}

	// 原 Sheet1 不变：5 数据行 + 1 表头 = 6，5 张图
	rows1, _ := f.GetRows("Sheet1")
	if len(rows1) != 6 {
		t.Fatalf("原 Sheet1 应 6 行，实际 %d", len(rows1))
	}
	pics1, _ := f.GetPictureCells("Sheet1")
	if len(pics1) != 5 {
		t.Fatalf("原 Sheet1 应 5 张图，实际 %d", len(pics1))
	}

	// 新 Sheet "搜索_口红"：表头 + 3 命中行 = 4 行，3 张图
	rowsKh, _ := f.GetRows("搜索_口红")
	if len(rowsKh) != 4 {
		t.Fatalf("搜索_口红 应 4 行，实际 %d: %v", len(rowsKh), rowsKh)
	}
	picsKh, _ := f.GetPictureCells("搜索_口红")
	if len(picsKh) != 3 {
		t.Fatalf("搜索_口红 应 3 张图，实际 %d", len(picsKh))
	}

	// 新 Sheet "搜索_眼影"：表头 + 1 命中行 = 2 行，1 张图
	rowsYy, _ := f.GetRows("搜索_眼影")
	if len(rowsYy) != 2 {
		t.Fatalf("搜索_眼影 应 2 行，实际 %d", len(rowsYy))
	}
}

// TestExtractInplace_Merged: merged 策略合并所有关键词到单个新 Sheet。
func TestExtractInplace_Merged(t *testing.T) {
	dir := t.TempDir()
	src := buildInplaceSrc(t, dir)

	task := core.ExtractTask{
		FolderPath:     src,
		Keywords:       []string{"口红", "眼影"},
		MatchMode:      core.MatchContains,
		SearchAllCols:  true,
		Output:         core.OutputMerged,
		HeaderRow:      1,
		PreserveImages: true,
		FilenamePrefix: "搜索_",
		OutputTarget:   core.OutputTargetInplaceSheets,
	}
	res := runInplaceExtract(t, task)
	if res.RowsMatched != 4 {
		t.Fatalf("期望 4 行，实际 %d", res.RowsMatched)
	}
	f, err := excelize.OpenFile(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// 应有 Sheet1 + 搜索_合并
	got := map[string]bool{}
	for _, s := range f.GetSheetList() {
		got[s] = true
	}
	if !got["Sheet1"] || !got["搜索_合并"] {
		t.Fatalf("Sheet 列表应含 Sheet1/搜索_合并，实际 %v", f.GetSheetList())
	}
	// 合并 Sheet：表头 + 4 命中行 = 5 行
	rows, _ := f.GetRows("搜索_合并")
	if len(rows) != 5 {
		t.Fatalf("搜索_合并 应 5 行，实际 %d", len(rows))
	}
}

// TestExtractInplace_BackupSource: BackupSource 为 true 时生成 .bak。
func TestExtractInplace_BackupSource(t *testing.T) {
	dir := t.TempDir()
	src := buildInplaceSrc(t, dir)

	task := core.ExtractTask{
		FolderPath:     src,
		Keywords:       []string{"口红"},
		MatchMode:      core.MatchContains,
		SearchAllCols:  true,
		Output:         core.OutputMerged,
		HeaderRow:      1,
		PreserveImages: true,
		OutputTarget:   core.OutputTargetInplaceSheets,
		BackupSource:   true,
	}
	_ = runInplaceExtract(t, task)

	// V1.2: 备份命名从 <src>.bak 改成 <src 去扩展>_备份_<时间戳>.xlsx
	ext := filepath.Ext(src)
	prefix := src[:len(src)-len(ext)] + "_备份_"
	matches, err := filepath.Glob(prefix + "*" + ext)
	if err != nil || len(matches) == 0 {
		t.Fatalf("期望生成 %s*%s，未找到（err=%v matches=%v）", prefix, ext, err, matches)
	}
}

// TestExtractInplace_PerSourceDegrades: per_source 在单文件时自动降级为 merged。
func TestExtractInplace_PerSourceDegrades(t *testing.T) {
	dir := t.TempDir()
	src := buildInplaceSrc(t, dir)

	task := core.ExtractTask{
		FolderPath:     src,
		Keywords:       []string{"口红"},
		MatchMode:      core.MatchContains,
		SearchAllCols:  true,
		Output:         core.OutputPerSource,
		HeaderRow:      1,
		PreserveImages: true,
		OutputTarget:   core.OutputTargetInplaceSheets,
	}
	_ = runInplaceExtract(t, task)
	f, _ := excelize.OpenFile(src)
	defer f.Close()
	got := map[string]bool{}
	for _, s := range f.GetSheetList() {
		got[s] = true
	}
	if !got["搜索_合并"] {
		t.Fatalf("per_source 应降级成 merged 产出 搜索_合并，实际 %v", f.GetSheetList())
	}
}
