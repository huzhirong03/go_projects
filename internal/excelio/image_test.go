package excelio

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// generatePNG 生成一张指定颜色的 2x2 PNG，用于单测。
func generatePNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

// buildFixtureWithImages 生成带图片的 xlsx 固件。
// B2 一张红图，B3 两张（红 + 绿），B4 无图。
func buildFixtureWithImages(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "with_images.xlsx")
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "Sheet1"
	_ = f.SetCellValue(sheet, "A1", "名称")
	_ = f.SetCellValue(sheet, "B1", "图片")
	_ = f.SetCellValue(sheet, "A2", "口红")
	_ = f.SetCellValue(sheet, "A3", "眼影")
	_ = f.SetCellValue(sheet, "A4", "粉底")

	red := generatePNG(t, color.RGBA{255, 0, 0, 255})
	green := generatePNG(t, color.RGBA{0, 255, 0, 255})

	addPic := func(cell string, data []byte) {
		pic := &excelize.Picture{
			Extension: ".png",
			File:      data,
			Format:    &excelize.GraphicOptions{},
		}
		if err := f.AddPictureFromBytes(sheet, cell, pic); err != nil {
			t.Fatalf("AddPictureFromBytes %s: %v", cell, err)
		}
	}
	addPic("B2", red)
	addPic("B3", red)
	addPic("B3", green)

	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}

func TestPictureIndexBuildAndMigrate(t *testing.T) {
	src := buildFixtureWithImages(t)
	r, err := Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	idx, err := BuildPictureIndex(r.File(), "Sheet1")
	if err != nil {
		t.Fatalf("BuildPictureIndex: %v", err)
	}

	// B2 应有 1 张图
	row2 := idx.PicturesOnRow(2)
	if len(row2) != 1 || len(row2[0].Pictures) != 1 {
		t.Errorf("Row 2 pictures = %+v, want 1 cell / 1 pic", row2)
	}
	// B3 应有 1 个 cell 包含 2 张图
	row3 := idx.PicturesOnRow(3)
	if len(row3) != 1 || len(row3[0].Pictures) != 2 {
		t.Errorf("Row 3 pictures = %+v, want 1 cell / 2 pics", row3)
	}
	// B4 无图
	if len(idx.PicturesOnRow(4)) != 0 {
		t.Error("Row 4 不应有图")
	}
	if total := idx.TotalPictures(); total != 3 {
		t.Errorf("TotalPictures = %d, want 3", total)
	}

	// 迁移：把源第 2 行图片迁移到目标文件第 10 行
	w := NewWriter()
	defer w.Close()
	sw, err := w.StreamFor("Sheet1")
	if err != nil {
		t.Fatalf("StreamFor: %v", err)
	}
	_ = sw.WriteRow(1, []any{"名称", "图片"})
	_ = sw.WriteRow(10, []any{"口红", ""})

	for _, cp := range idx.PicturesOnRow(2) {
		if err := MigratePicture(w.File(), "Sheet1", 10, cp); err != nil {
			t.Fatalf("MigratePicture: %v", err)
		}
	}

	out := filepath.Join(t.TempDir(), "migrated.xlsx")
	if err := w.Save(out); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 回读验证
	r2, err := Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()
	idx2, err := BuildPictureIndex(r2.File(), "Sheet1")
	if err != nil {
		t.Fatalf("BuildPictureIndex out: %v", err)
	}
	got := idx2.PicturesOnRow(10)
	if len(got) != 1 || len(got[0].Pictures) != 1 {
		t.Errorf("migrated row 10 = %+v, want 1 cell / 1 pic", got)
	}
}
