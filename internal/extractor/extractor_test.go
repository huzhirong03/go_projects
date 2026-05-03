package extractor

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"excel-master/internal/core"
	"excel-master/internal/excelio"

	"github.com/xuri/excelize/v2"
)

// buildTestFolder 生成一个仿真实场景的文件夹：
//   - file1: 表头[产品名,型号,价格]，含"口红 A"行和图
//   - file2: 表头[型号,产品名,价格]（顺序不同）
//   - file3: 表头[产品名,价格]（缺 型号 列）
func buildTestFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeXlsx := func(name string, headers []string, rows [][]any, picRows map[int]int) {
		path := filepath.Join(dir, name)
		f := excelize.NewFile()
		defer f.Close()
		const sheet = "Sheet1"
		// headers
		for i, h := range headers {
			cell, _ := excelize.CoordinatesToCellName(i+1, 1)
			_ = f.SetCellValue(sheet, cell, h)
		}
		// rows
		for ri, r := range rows {
			for ci, v := range r {
				cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
				_ = f.SetCellValue(sheet, cell, v)
			}
		}
		// pictures: picRows maps rowIdx(1-based, relative to data including header) -> col
		for row, col := range picRows {
			cell, _ := excelize.CoordinatesToCellName(col, row)
			png := smallPNG(t, color.RGBA{200, 50, 50, 255})
			pic := &excelize.Picture{Extension: ".png", File: png, Format: &excelize.GraphicOptions{}}
			if err := f.AddPictureFromBytes(sheet, cell, pic); err != nil {
				t.Fatalf("AddPictureFromBytes: %v", err)
			}
		}
		if err := f.SaveAs(path); err != nil {
			t.Fatalf("SaveAs %s: %v", name, err)
		}
	}

	// file1: 产品名, 型号, 价格
	writeXlsx("file1.xlsx",
		[]string{"产品名", "型号", "价格"},
		[][]any{
			{"口红 A", "SKU001", 99.0},
			{"眼影 B", "SKU002", 50.0},
			{"粉底 C", "SKU003", 120.0},
		},
		map[int]int{2: 1}, // 在第 2 行第 1 列（"口红 A" 单元格）加图
	)

	// file2: 型号, 产品名, 价格（顺序不同）
	writeXlsx("file2.xlsx",
		[]string{"型号", "产品名", "价格"},
		[][]any{
			{"SKU101", "哑光口红 D", 150.0},
			{"SKU102", "粉底 E", 88.0},
		},
		map[int]int{2: 2}, // 行 2 列 2 是"哑光口红 D"
	)

	// file3: 产品名, 价格（缺 型号 列）
	writeXlsx("file3.xlsx",
		[]string{"产品名", "价格"},
		[][]any{
			{"眼影 F", 40.0},
			{"无关商品", 10.0},
		},
		nil,
	)

	// 放一个无关文件，验证过滤
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ignore me"), 0o644)
	// 放一个 ~$ 锁文件，验证过滤
	_ = os.WriteFile(filepath.Join(dir, "~$file1.xlsx"), []byte("lock"), 0o644)

	return dir
}

func smallPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png: %v", err)
	}
	return buf.Bytes()
}

func TestExtract_PerKeyword_WithImages(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()

	task := core.ExtractTask{
		FolderPath:     src,
		Keywords:       []string{"口红", "fd"}, // fd = 粉底 的拼音首字母
		MatchMode:      core.MatchExact | core.MatchContains | core.MatchPinyin,
		SearchAllCols:  true,
		Output:         core.OutputPerKeyword,
		OutputDir:      out,
		HeaderRow:      1,
		PreserveImages: true,
	}

	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// 命中行：file1 口红 A、file1 粉底 C、file2 哑光口红 D、file2 粉底 E → 4 行
	if result.RowsMatched != 4 {
		t.Errorf("RowsMatched = %d, want 4", result.RowsMatched)
	}
	// 图片：file1 "口红 A" 1 张、file2 "哑光口红 D" 1 张 → 2 张
	if result.ImagesMigrated != 2 {
		t.Errorf("ImagesMigrated = %d, want 2", result.ImagesMigrated)
	}
	// 输出文件：2 个（口红_*.xlsx 和 fd_*.xlsx）
	if len(result.OutputFiles) != 2 {
		t.Fatalf("OutputFiles = %d, want 2: %v", len(result.OutputFiles), result.OutputFiles)
	}

	// 验证每个输出文件都能被正常打开、且表头为统一 schema
	for _, p := range result.OutputFiles {
		verifyOutput(t, p)
	}
}

func TestExtract_Merged(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"口红"},
		MatchMode: core.MatchContains, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out,
		HeaderRow: 1, PreserveImages: false,
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.OutputFiles) != 1 {
		t.Fatalf("want 1 merged output, got %d", len(result.OutputFiles))
	}
	// 命中：口红 A、哑光口红 D → 2 行
	if result.RowsMatched != 2 {
		t.Errorf("RowsMatched = %d, want 2", result.RowsMatched)
	}
	// V1.4 起 merged 改为 zip 手术（原汁原味）：表头来自 primary 源，不再追加"命中关键词/来源文件"列。
	r, err := excelio.Open(result.OutputFiles[0])
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer r.Close()
	sheets := r.SheetNames()
	if len(sheets) != 1 {
		t.Fatalf("merged 输出应只有 1 个 sheet（继承自 primary），实际 %d: %v", len(sheets), sheets)
	}
	header, _ := r.Header(sheets[0], 1)
	if len(header) == 0 {
		t.Errorf("merged 表头为空")
	}
	for _, h := range header {
		if h == "命中关键词" || h == "来源文件" {
			t.Errorf("V1.4 merged 不应再追加 %q 列：%v", h, header)
		}
	}
}

func TestExtract_PerSource(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"口红", "粉底"},
		MatchMode: core.MatchContains, SearchAllCols: true,
		Output: core.OutputPerSource, OutputDir: out,
		HeaderRow: 1, PreserveImages: false,
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// file1 有口红 A + 粉底 C、file2 有哑光口红 D + 粉底 E → 两个源文件都命中
	// file3 无命中
	if len(result.OutputFiles) != 2 {
		t.Errorf("OutputFiles = %d, want 2", len(result.OutputFiles))
	}
	for _, p := range result.OutputFiles {
		if !strings.Contains(filepath.Base(p), "已提取") {
			t.Errorf("output name 格式不对: %s", p)
		}
	}
}

func TestAskFileOpenDecision_FileOccupied(t *testing.T) {
	err := core.Wrap("EXCEL_OPEN_FAILED", "打开 Excel 失败: demo.xlsx", os.ErrPermission)
	emitter := &promptTestEmitter{choice: core.FileBlockedRetry}
	if got := askFileOpenDecision(context.Background(), emitter, "demo.xlsx", err); got != fileOpenRetry {
		t.Fatalf("decision=%v, want retry", got)
	}
	if emitter.calls != 1 {
		t.Fatalf("prompt calls=%d, want 1", emitter.calls)
	}
}

func TestAskFileOpenDecision_NotFileOccupied(t *testing.T) {
	err := core.New("BAD_XLSX", "文件格式错误")
	emitter := &promptTestEmitter{choice: core.FileBlockedRetry}
	if got := askFileOpenDecision(context.Background(), emitter, "bad.xlsx", err); got != fileOpenAbort {
		t.Fatalf("decision=%v, want abort", got)
	}
	if emitter.calls != 0 {
		t.Fatalf("prompt calls=%d, want 0", emitter.calls)
	}
}

func TestAskOfficeLockDecision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "供应商D_清单.xlsx")
	lockPath := filepath.Join(dir, "~$供应商D_清单.xlsx")
	if err := os.WriteFile(lockPath, []byte("lock"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	emitter := &promptTestEmitter{choice: core.FileBlockedSkip}
	if got := askOfficeLockDecision(context.Background(), emitter, path); got != fileOpenSkip {
		t.Fatalf("decision=%v, want skip", got)
	}
	if emitter.calls != 1 {
		t.Fatalf("prompt calls=%d, want 1", emitter.calls)
	}
}

type promptTestEmitter struct {
	choice core.FileBlockedChoice
	calls  int
}

func (p *promptTestEmitter) Progress(core.Progress) {}
func (p *promptTestEmitter) Log(string, string)     {}
func (p *promptTestEmitter) Done(any)               {}
func (p *promptTestEmitter) Error(error)            {}

func (p *promptTestEmitter) PromptFileBlocked(ctx context.Context, req core.FileBlockedRequest) core.FileBlockedChoice {
	p.calls++
	return p.choice
}

// verifyOutput 简单校验输出文件能被 excelize 打开且含表头。
func verifyOutput(t *testing.T, path string) {
	t.Helper()
	r, err := excelio.Open(path)
	if err != nil {
		t.Fatalf("reopen %s: %v", path, err)
	}
	defer r.Close()
	sheets := r.SheetNames()
	if len(sheets) == 0 {
		t.Fatalf("%s 没有 Sheet", path)
	}
	header, err := r.Header(sheets[0], 1)
	if err != nil || len(header) == 0 {
		t.Fatalf("%s 读表头失败: %v", path, err)
	}
	// 应包含统一 schema 的三列
	want := map[string]bool{"产品名": false, "型号": false, "价格": false}
	for _, h := range header {
		if _, ok := want[h]; ok {
			want[h] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("%s 输出表头缺少 %q: %v", path, k, header)
		}
	}
}
