package splitter

import (
	"context"
	"fmt"
	"path/filepath"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/pipeline"
)

// SplitByRows：按行数拆分。每 RowsPerFile 条数据行生成一个新文件，
// 每个分片都复制一次表头。task.SheetNames 为空表示处理全部 Sheet；
// 多 Sheet 时每个 Sheet 独立拆分（part 序号各自从 1 开始），文件名带 Sheet 名。
//
// 输出命名（单 Sheet）：<源名>_part<序号>_<时间戳>.xlsx
// 输出命名（多 Sheet）：<源名>_<Sheet名>_part<序号>_<时间戳>.xlsx
func SplitByRows(ctx context.Context, task core.SplitTask, emitter core.EventEmitter) (*Result, error) {
	if emitter == nil {
		emitter = pipeline.NoopEmitter{}
	}
	if err := validateCommon(task); err != nil {
		return nil, err
	}
	if task.RowsPerFile <= 0 {
		return nil, core.New("INVALID_TASK", "RowsPerFile 必须 > 0")
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

	result := &Result{SourceFile: task.SourcePath, Mode: string(core.SplitByRows)}
	base := stemOf(task.SourcePath)
	ts := timestamp()
	multi := len(sheets) > 1

	for _, sheet := range sheets {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return result, err
		}
		if err := splitOneSheetByRows(ctx, r, sheet, task, base, ts, multi, emitter, result); err != nil {
			return result, err
		}
	}

	emitter.Progress(core.Progress{Stage: "finalizing",
		Message: fmt.Sprintf("完成：%d 个分片", result.PartsCreated)})
	return result, nil
}

// splitOneSheetByRows 处理单个 Sheet 的按行数拆分，把结果累加到 result。
func splitOneSheetByRows(
	ctx context.Context, r *excelio.Reader, sheet string,
	task core.SplitTask, base, ts string, multi bool,
	emitter core.EventEmitter, result *Result,
) error {
	// 表头（用于复制到每个分片）
	var headerValues []any
	var headerHeight float64
	if task.HeaderRow > 0 {
		h, err := r.Header(sheet, task.HeaderRow)
		if err != nil {
			return err
		}
		headerValues = make([]any, len(h))
		for i, v := range h {
			headerValues[i] = v
		}
		headerHeight, _, _ = r.RowHeight(sheet, task.HeaderRow)
	}

	// 列宽：一次读取，每个 part 创建后 apply
	colWidths, _ := r.ColumnWidths(sheet)

	var picIdx *excelio.PictureIndex
	if task.PreserveImages {
		picIdx, _ = excelio.BuildPictureIndex(r.File(), sheet)
	}

	it, err := r.Iterate(sheet)
	if err != nil {
		return err
	}
	defer it.Close()

	var (
		part         *partWriter
		partIdx      int
		partDataRows int
	)
	closePart := func() error {
		if part == nil {
			return nil
		}
		if err := part.save(); err != nil {
			return err
		}
		result.OutputFiles = append(result.OutputFiles, part.path)
		_ = part.close()
		part = nil
		return nil
	}

	for it.Next() {
		if it.RowNum()%500 == 0 {
			if err := pipeline.CheckCancel(ctx); err != nil {
				_ = closePart()
				return err
			}
		}
		if task.HeaderRow > 0 && it.RowNum() <= task.HeaderRow {
			continue
		}
		cells, err := it.Columns()
		if err != nil {
			_ = closePart()
			return err
		}

		if part == nil || partDataRows >= task.RowsPerFile {
			if err := closePart(); err != nil {
				return err
			}
			partIdx++
			partDataRows = 0
			outPath := filepath.Join(task.OutputDir,
				fmt.Sprintf("%s%s_part%03d_%s.xlsx",
					sanitizeFileName(base), sheetSegment(sheet, multi), partIdx, ts))
			part, err = newPartWriter(outPath, sheet)
			if err != nil {
				return err
			}
			if err := part.applyColumnWidthsIfNeeded(colWidths); err != nil {
				return err
			}
			if headerValues != nil {
				if _, err := part.writeRow(headerValues, 0, headerHeight); err != nil {
					return err
				}
			}
			result.PartsCreated++
			emitter.Progress(core.Progress{
				Stage:   "writing",
				Done:    int64(result.PartsCreated),
				Message: fmt.Sprintf("[%s] 创建分片 %d", sheet, partIdx),
			})
		}

		formulas := readRowFormulas(r, sheet, it.RowNum(), len(cells))
		values := rowToValues(cells, formulas)
		height, _, _ := r.RowHeight(sheet, it.RowNum())
		dstRow, err := part.writeRow(values, it.RowNum(), height)
		if err != nil {
			return err
		}
		result.RowsScanned++
		partDataRows++

		if picIdx != nil {
			if pics := picIdx.PicturesOnRow(it.RowNum()); len(pics) > 0 {
				n, err := part.migratePictures(pics, dstRow)
				result.ImagesMigrated += n
				if err != nil {
					return err
				}
			}
		}
	}
	if err := it.Err(); err != nil {
		_ = closePart()
		return err
	}
	return closePart()
}
