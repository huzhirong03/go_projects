package excelio

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildFixtureNoImages 生成一个完全没有图片的 xlsx，用来验证 LoadSheetAnchors 的 fast path：
// 没有 sheet rels 文件 / 没有 drawing 关系 → 不应该解压 sheet1.xml 即可立即返回。
func buildFixtureNoImages(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "no_images.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "Sheet1"
	_ = f.SetCellValue(sheet, "A1", "id")
	_ = f.SetCellValue(sheet, "B1", "name")
	for i := 0; i < 50; i++ {
		_ = f.SetCellValue(sheet, "A"+itoa(i+2), i+1)
		_ = f.SetCellValue(sheet, "B"+itoa(i+2), "row")
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

// TestZipImageSource_NoImagesFastPath 验证：无图 xlsx 调 LoadSheetAnchors 立刻返回，
// 索引为空，不会触发"解压整个 sheet1.xml 找一个 <drawing> 标签"的旧慢路径。
func TestZipImageSource_NoImagesFastPath(t *testing.T) {
	path := buildFixtureNoImages(t)

	src, err := OpenZipImageSource(path)
	if err != nil {
		t.Fatalf("OpenZipImageSource: %v", err)
	}
	defer func() { _ = src.Close() }()

	if err := src.LoadSheetAnchors("Sheet1"); err != nil {
		t.Fatalf("LoadSheetAnchors no-img: %v", err)
	}
	idx, ok := src.sheetIdx["Sheet1"]
	if !ok {
		t.Fatalf("sheetIdx 未注册")
	}
	if idx.drawingXML != "" {
		t.Errorf("无图文件 drawingXML 应为空，实际 %q", idx.drawingXML)
	}
	if len(idx.anchorsByRow) != 0 {
		t.Errorf("无图文件 anchorsByRow 应为空，实际 %d 条", len(idx.anchorsByRow))
	}
	if len(idx.rIdToMedia) != 0 {
		t.Errorf("无图文件 rIdToMedia 应为空，实际 %d 条", len(idx.rIdToMedia))
	}
	if cells := src.PictureCellsByRow("Sheet1"); len(cells) != 0 {
		t.Errorf("无图文件 PictureCellsByRow 应为空，实际 %d 行", len(cells))
	}
}

// TestZipImageSource_WithImagesParse 验证有图 xlsx 走完整路径仍然能拿到正确锚点。
// 这是 B1 优化的回归测试：修改后不能让有图的解析能力退步。
func TestZipImageSource_WithImagesParse(t *testing.T) {
	path := buildFixtureWithImages(t) // 来自 image_test.go：B2 红图、B3 红+绿

	src, err := OpenZipImageSource(path)
	if err != nil {
		t.Fatalf("OpenZipImageSource: %v", err)
	}
	defer func() { _ = src.Close() }()

	if err := src.LoadSheetAnchors("Sheet1"); err != nil {
		t.Fatalf("LoadSheetAnchors: %v", err)
	}
	idx, ok := src.sheetIdx["Sheet1"]
	if !ok {
		t.Fatalf("sheetIdx 未注册")
	}
	if idx.drawingXML == "" {
		t.Errorf("有图文件 drawingXML 不应为空")
	}
	if len(idx.rIdToMedia) == 0 {
		t.Errorf("有图文件 rIdToMedia 不应为空")
	}
	cells := src.PictureCellsByRow("Sheet1")
	if len(cells) == 0 {
		t.Errorf("有图文件 PictureCellsByRow 不应为空")
	}
	// 行 2 (B2) 至少 1 张；行 3 (B3) 至少 2 张
	if got := len(cells[2]); got < 1 {
		t.Errorf("行 2 应至少 1 张图，实际 %d", got)
	}
	if got := len(cells[3]); got < 2 {
		t.Errorf("行 3 应至少 2 张图，实际 %d", got)
	}
}

// TestZipImageSource_LoadSheetAnchorsIdempotent 验证 LoadSheetAnchors 幂等：
// 多次调用对同一 sheet 不会重复解析、不会改变结果。
func TestZipImageSource_LoadSheetAnchorsIdempotent(t *testing.T) {
	path := buildFixtureNoImages(t)
	src, err := OpenZipImageSource(path)
	if err != nil {
		t.Fatalf("OpenZipImageSource: %v", err)
	}
	defer func() { _ = src.Close() }()

	for i := 0; i < 3; i++ {
		if err := src.LoadSheetAnchors("Sheet1"); err != nil {
			t.Fatalf("第 %d 次 LoadSheetAnchors: %v", i, err)
		}
	}
	if len(src.sheetIdx) != 1 {
		t.Errorf("sheetIdx 长度 = %d, want 1（幂等应只创建一次）", len(src.sheetIdx))
	}
}
