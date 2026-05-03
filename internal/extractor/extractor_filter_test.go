package extractor

// extractor_filter_test.go：高级筛选集成测试。
// 复用 buildTestFolder()（extractor_test.go）：
//   file1: 产品名/型号/价格 → 口红 A SKU001 99 / 眼影 B SKU002 50 / 粉底 C SKU003 120
//   file2: 型号/产品名/价格 → SKU101 哑光口红 D 150 / SKU102 粉底 E 88
//   file3: 产品名/价格        → 眼影 F 40 / 无关商品 10

import (
	"context"
	"strings"
	"testing"

	"excel-master/internal/core"
)

// 收集 emitter 警告，便于断言"列缺失警告"。
type warnCollector struct {
	logs []string
}

func (e *warnCollector) Progress(_ core.Progress) {}
func (e *warnCollector) Log(level, msg string) {
	if level == core.LogWarn {
		e.logs = append(e.logs, msg)
	}
}
func (e *warnCollector) Done(_ any)    {}
func (e *warnCollector) Error(_ error) {}

// 1. 关键词 + filter AND：仅命中既符合关键词又通过条件的行
func TestExtract_FilterAfterKeyword_AND(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()

	task := core.ExtractTask{
		FolderPath:    src,
		Keywords:      []string{"口红", "fd"}, // 旧测试 4 行命中
		MatchMode:     core.MatchContains | core.MatchPinyin,
		SearchAllCols: true,
		Output:        core.OutputMerged,
		OutputDir:     out,
		HeaderRow:     1,
		AdvancedFilter: &core.AdvancedFilterSpec{
			Mode: "all",
			Conditions: []core.AdvancedFilterCondition{
				{Column: "价格", Op: "ge", Value: "100"},
			},
		},
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// 应只剩 价格>=100 的命中行：file1 粉底C=120, file2 哑光口红D=150 → 2 行
	if result.RowsMatched != 2 {
		t.Errorf("RowsMatched = %d, want 2 (filter ge 100 keeps 粉底C/哑光口红D)", result.RowsMatched)
	}
}

// 2. filter 把所有关键词命中行都剔除 → 0 命中
func TestExtract_FilterRemovesAll(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"口红"},
		MatchMode: core.MatchContains, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out, HeaderRow: 1,
		AdvancedFilter: &core.AdvancedFilterSpec{
			Mode: "all",
			Conditions: []core.AdvancedFilterCondition{
				{Column: "价格", Op: "lt", Value: "0"}, // 不可能命中
			},
		},
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.RowsMatched != 0 {
		t.Errorf("RowsMatched = %d, want 0 (filter 把所有命中行都剔除)", result.RowsMatched)
	}
}

// 3. filter 列在某些文件里完全缺失 → 该文件整体跳过 + 警告
func TestExtract_FilterAllMissingColumn_FileSkipped(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	w := &warnCollector{}
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"眼影"}, // 命中 file1 眼影B、file3 眼影F
		MatchMode: core.MatchContains, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out, HeaderRow: 1,
		AdvancedFilter: &core.AdvancedFilterSpec{
			Mode: "all",
			Conditions: []core.AdvancedFilterCondition{
				{Column: "型号", Op: "eq", Value: "SKU002"}, // file1 眼影B 是 SKU002
			},
		},
	}
	result, err := Extract(context.Background(), task, w)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// file1 眼影B 通过；file3 眼影F 因型号列全缺失被整体跳过 → 1 行
	if result.RowsMatched != 1 {
		t.Errorf("RowsMatched = %d, want 1 (file3 应被跳过)", result.RowsMatched)
	}
	// 应有列缺失警告
	hasMissingWarn := false
	for _, log := range w.logs {
		if strings.Contains(log, "缺失高级筛选列") || strings.Contains(log, "型号") {
			hasMissingWarn = true
			break
		}
	}
	if !hasMissingWarn {
		t.Errorf("期望有列缺失警告，实际日志: %v", w.logs)
	}
}

// 4. 部分列缺失：仍有谓词剩下，剩余条件继续生效
func TestExtract_FilterPartialMissingColumn(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"口红", "fd"},
		MatchMode: core.MatchContains | core.MatchPinyin, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out, HeaderRow: 1,
		// 两个条件：型号 (file3 没有) + 价格 (所有文件都有)
		AdvancedFilter: &core.AdvancedFilterSpec{
			Mode: "all",
			Conditions: []core.AdvancedFilterCondition{
				{Column: "型号", Op: "starts_with", Value: "SKU"}, // 所有 file1/2 行都通过
				{Column: "价格", Op: "ge", Value: "100"},          // 价格 >= 100
			},
		},
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// file1: 口红A=99 fail price; 粉底C=120 pass → 1
	// file2: 哑光口红D=150 pass, 粉底E=88 fail → 1
	// file3: 型号列缺失 → 部分缺失保留"价格" → 关键词命中行无（眼影F、无关）
	//        实际无关键词命中 → 0 (但价格条件仍正常计算)
	if result.RowsMatched != 2 {
		t.Errorf("RowsMatched = %d, want 2 (粉底C + 哑光口红D)", result.RowsMatched)
	}
}

// 5. filter 编译硬错（between 非数）→ 任务失败
func TestExtract_FilterCompileError(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"口红"},
		MatchMode: core.MatchContains, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out, HeaderRow: 1,
		AdvancedFilter: &core.AdvancedFilterSpec{
			Mode: "all",
			Conditions: []core.AdvancedFilterCondition{
				{Column: "价格", Op: "between", Value: "abc", Value2: "def"},
			},
		},
	}
	_, err := Extract(context.Background(), task, nil)
	if err == nil {
		t.Fatal("应返回硬错误")
	}
	if !strings.Contains(err.Error(), "高级筛选编译失败") && !strings.Contains(err.Error(), "FILTER_COMPILE_FAILED") {
		t.Errorf("err msg 应提示筛选编译失败，实际 %v", err)
	}
}

// 6. nil filter 回归：行为完全跟旧版一致
func TestExtract_NilFilter_Regression(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"口红", "fd"},
		MatchMode: core.MatchContains | core.MatchPinyin, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out, HeaderRow: 1,
		AdvancedFilter: nil, // 显式 nil
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// 旧基线：4 行命中
	if result.RowsMatched != 4 {
		t.Errorf("RowsMatched = %d, want 4 (nil filter 应 100%% 通过)", result.RowsMatched)
	}
}

// 7. 任一满足（OR）模式
func TestExtract_FilterModeAny(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"口红", "fd"},
		MatchMode: core.MatchContains | core.MatchPinyin, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out, HeaderRow: 1,
		AdvancedFilter: &core.AdvancedFilterSpec{
			Mode: "any", // 任一满足
			Conditions: []core.AdvancedFilterCondition{
				{Column: "价格", Op: "ge", Value: "1000"},      // 没有命中行通过
				{Column: "产品名", Op: "contains", Value: "粉底"}, // 命中 粉底C 粉底E
			},
		},
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// file1 粉底C, file2 粉底E → 2 行（口红 A/D 含产品名"粉底"=false，价格也不超千）
	if result.RowsMatched != 2 {
		t.Errorf("RowsMatched = %d, want 2 (OR: 包含'粉底' 命中)", result.RowsMatched)
	}
}

// 9. 仅高级筛选无关键词（V1.5 新场景）：per_keyword 自动降级 merged
func TestExtract_FilterOnly_NoKeyword(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src,
		Keywords:   nil, // 没关键词
		MatchMode:  core.MatchContains, SearchAllCols: true,
		Output:    core.OutputPerKeyword, // 应自动降级为 merged
		OutputDir: out, HeaderRow: 1,
		AdvancedFilter: &core.AdvancedFilterSpec{
			Mode: "all",
			Conditions: []core.AdvancedFilterCondition{
				{Column: "价格", Op: "ge", Value: "100"},
			},
		},
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// 价格>=100 行：file1 粉底C=120, file2 哑光口红D=150 → 2 行
	if result.RowsMatched != 2 {
		t.Errorf("RowsMatched = %d, want 2 (filter-only ge 100)", result.RowsMatched)
	}
	// 应只产出 1 个 merged 文件（per_keyword 已被降级）
	if len(result.OutputFiles) != 1 {
		t.Errorf("OutputFiles = %d, want 1 (per_keyword auto-downgraded to merged)", len(result.OutputFiles))
	}
}

// 10. 关键词和筛选都空 → 报错
func TestExtract_NoRules(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: nil,
		MatchMode: core.MatchContains, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out, HeaderRow: 1,
		AdvancedFilter: nil,
	}
	_, err := Extract(context.Background(), task, nil)
	if err == nil {
		t.Fatal("两者都空应报错")
	}
	if !strings.Contains(err.Error(), "至少需要") {
		t.Errorf("err msg 应含'至少需要'，实际 %v", err)
	}
}

// 8. IsEmpty 跳过：spec 有 conditions 但全是空字段（占位行）
func TestExtract_FilterAllEmpty_Bypassed(t *testing.T) {
	src := buildTestFolder(t)
	out := t.TempDir()
	task := core.ExtractTask{
		FolderPath: src, Keywords: []string{"口红"},
		MatchMode: core.MatchContains, SearchAllCols: true,
		Output: core.OutputMerged, OutputDir: out, HeaderRow: 1,
		AdvancedFilter: &core.AdvancedFilterSpec{
			Mode: "all",
			Conditions: []core.AdvancedFilterCondition{
				{Column: "", Op: ""}, // 占位空行，应被视作不启用
			},
		},
	}
	result, err := Extract(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// 行为应跟旧版一致：口红 A + 哑光口红 D → 2 行
	if result.RowsMatched != 2 {
		t.Errorf("RowsMatched = %d, want 2 (空 conditions 应等同于不启用)", result.RowsMatched)
	}
}
