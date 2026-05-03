package splitter

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/pipeline"
)

// SplitBySheet：每个 Sheet 产出一个新 xlsx（原汁原味路径）。
//
// 策略：复制源文件到目标 -> KeepSheetsOnly([该 sheet])。
// 所有样式/合并单元格/公式引用/图片锚点/条件格式/数据验证都由 excelize 自动维护。
//
// 输出命名：<源名>_<Sheet名>_<时间戳>.xlsx
func SplitBySheet(ctx context.Context, task core.SplitTask, emitter core.EventEmitter) (*Result, error) {
	if emitter == nil {
		emitter = pipeline.NoopEmitter{}
	}
	if err := validateCommon(task); err != nil {
		return nil, err
	}
	if err := requireXLSXSource(task.SourcePath); err != nil {
		return nil, err
	}
	if task.OutputTarget == core.OutputTargetInplaceSheets {
		return nil, core.New("INPLACE_BYSHEET_UNSUPPORTED",
			"按 Sheet 拆分不支持\"写回源文件新 Sheet\"：源里每个 Sheet 直接就是结果，无需再产出新 Sheet。请改用\"输出新文件\"。")
	}

	// 打开源，读取 sheet 列表以及每个 sheet 的行数/图片数（统计用）
	r, err := excelio.Open(task.SourcePath)
	if err != nil {
		return nil, err
	}
	sheets := selectSheets(r.SheetNames(), task.SheetNames)
	if len(sheets) == 0 {
		_ = r.Close()
		return nil, core.New("NO_MATCHED_SHEET", "源文件没有任何匹配指定 Sheet 名的工作表")
	}

	// 统计每个 sheet 的行数和图片数（用于 Result 展示，不参与写入）。
	// 关键：行数用流式 Iterate 数，禁止 GetRows 全量加载（违反规则 §1.4，1GB+
	// 文件直接 OOM）。GetPictureCells 是 O(图片数) 不是 O(行数)，可继续用。
	rowsStats := make(map[string]int, len(sheets))
	imgsStats := make(map[string]int, len(sheets))
	for _, sheet := range sheets {
		rowsStats[sheet] = countSheetRowsStream(r, sheet)
		if cells, err := r.File().GetPictureCells(sheet); err == nil {
			// 一个 cell 可能挂多张图，但对 Result 指标影响不大
			imgsStats[sheet] = len(cells)
		}
	}
	_ = r.Close()

	result := &Result{SourceFile: task.SourcePath, Mode: string(core.SplitBySheet)}
	base := stemOf(task.SourcePath)
	ts := timestamp()

	total := int64(len(sheets))
	for i, sheet := range sheets {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return result, err
		}
		emitter.Progress(core.Progress{
			Stage:   "writing",
			Done:    int64(i),
			Total:   total,
			Message: "处理 Sheet: " + sheet,
		})
		outPath := filepath.Join(task.OutputDir,
			sanitizeFileName(base)+"_"+sanitizeFileName(sheet)+"_"+ts+".xlsx")
		// 原汁原味：只保留这个 Sheet，不过滤行
		if err := cloneAndExtractSheet(task.SourcePath, outPath, sheet, nil); err != nil {
			return result, err
		}
		result.RowsScanned += rowsStats[sheet]
		result.ImagesMigrated += imgsStats[sheet]
		result.OutputFiles = append(result.OutputFiles, outPath)
		result.PartsCreated++
	}
	emitter.Progress(core.Progress{Stage: "finalizing", Done: total, Total: total,
		Message: fmt.Sprintf("完成：%d 个 Sheet", result.PartsCreated)})
	return result, nil
}

func stemOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func validateCommon(task core.SplitTask) error {
	if task.SourcePath == "" {
		return core.New("INVALID_TASK", "SourcePath 不能为空")
	}
	// inplace 写回源文件，不需要 OutputDir
	if task.OutputTarget != core.OutputTargetInplaceSheets && task.OutputDir == "" {
		return core.New("INVALID_TASK", "OutputDir 不能为空")
	}
	return nil
}
