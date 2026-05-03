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

// SplitBySheet：每个 Sheet 产出一个新 xlsx。
// 输出命名：<源名>_<Sheet名>_<时间戳>.xlsx
func SplitBySheet(ctx context.Context, task core.SplitTask, emitter core.EventEmitter) (*Result, error) {
	if emitter == nil {
		emitter = pipeline.NoopEmitter{}
	}
	if err := validateCommon(task); err != nil {
		return nil, err
	}
	r, err := excelio.Open(task.SourcePath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	sheets := selectSheets(r.SheetNames(), task.SheetNames)
	if len(sheets) == 0 {
		return nil, core.New("NO_MATCHED_SHEET", "源文件没有任何匹配指定 Sheet 名的工作表")
	}
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
		rows, imgs, err := copySheetToNewFile(ctx, r, sheet, outPath)
		if err != nil {
			return result, err
		}
		result.RowsScanned += rows
		result.ImagesMigrated += imgs
		result.OutputFiles = append(result.OutputFiles, outPath)
		result.PartsCreated++
	}
	emitter.Progress(core.Progress{Stage: "finalizing", Done: total, Total: total,
		Message: fmt.Sprintf("完成：%d 个 Sheet", result.PartsCreated)})
	return result, nil
}

// copySheetToNewFile 流式把一个 Sheet 的所有行+图片复制到新文件，
// 并复刻源 Sheet 的列宽、每行的自定义行高、公式表达式。
func copySheetToNewFile(
	ctx context.Context, src *excelio.Reader, sheet, outPath string,
) (rowsCopied, imagesMigrated int, err error) {
	part, err := newPartWriter(outPath, sheet)
	if err != nil {
		return 0, 0, err
	}
	defer part.close()

	// 列宽：写第一行前 apply
	if widths, _ := src.ColumnWidths(sheet); len(widths) > 0 {
		if err := part.applyColumnWidthsIfNeeded(widths); err != nil {
			return 0, 0, err
		}
	}

	// 图片索引
	picIdx, perr := excelio.BuildPictureIndex(src.File(), sheet)
	if perr != nil {
		picIdx = nil // 降级：不带图
	}

	it, err := src.Iterate(sheet)
	if err != nil {
		return 0, 0, err
	}
	defer it.Close()

	for it.Next() {
		if it.RowNum()%500 == 0 {
			if err := pipeline.CheckCancel(ctx); err != nil {
				return rowsCopied, imagesMigrated, err
			}
		}
		cells, err := it.Columns()
		if err != nil {
			return rowsCopied, imagesMigrated, err
		}
		formulas := readRowFormulas(src, sheet, it.RowNum(), len(cells))
		values := rowToValues(cells, formulas)
		height, _, _ := src.RowHeight(sheet, it.RowNum())
		dstRow, err := part.writeRow(values, it.RowNum(), height)
		if err != nil {
			return rowsCopied, imagesMigrated, err
		}
		rowsCopied++
		if picIdx != nil {
			if pics := picIdx.PicturesOnRow(it.RowNum()); len(pics) > 0 {
				n, err := part.migratePictures(pics, dstRow)
				imagesMigrated += n
				if err != nil {
					return rowsCopied, imagesMigrated, err
				}
			}
		}
	}
	if err := it.Err(); err != nil {
		return rowsCopied, imagesMigrated, err
	}
	if err := part.save(); err != nil {
		return rowsCopied, imagesMigrated, err
	}
	return rowsCopied, imagesMigrated, nil
}

func stemOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func validateCommon(task core.SplitTask) error {
	if task.SourcePath == "" {
		return core.New("INVALID_TASK", "SourcePath 不能为空")
	}
	if task.OutputDir == "" {
		return core.New("INVALID_TASK", "OutputDir 不能为空")
	}
	return nil
}
