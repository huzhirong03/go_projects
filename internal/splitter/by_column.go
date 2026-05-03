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

// SplitByColumn：按指定列值分桶拆分。列值相同的行进入同一个文件。
// SplitColumn 用列名（需要 HeaderRow > 0）。task.SheetNames 为空表示处理全部 Sheet；
// 多 Sheet 时每个 Sheet 独立拆分（同列值在不同 Sheet 各自一个文件），文件名带 Sheet 名。
//
// 输出命名（单 Sheet）：<源名>_<列值>_<时间戳>.xlsx
// 输出命名（多 Sheet）：<源名>_<Sheet名>_<列值>_<时间戳>.xlsx
func SplitByColumn(ctx context.Context, task core.SplitTask, emitter core.EventEmitter) (*Result, error) {
	if emitter == nil {
		emitter = pipeline.NoopEmitter{}
	}
	if err := validateCommon(task); err != nil {
		return nil, err
	}
	if task.HeaderRow <= 0 {
		return nil, core.New("INVALID_TASK", "按列值拆分需要指定表头行 (HeaderRow > 0)")
	}
	if strings.TrimSpace(task.SplitColumn) == "" {
		return nil, core.New("INVALID_TASK", "SplitColumn 不能为空")
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

	result := &Result{SourceFile: task.SourcePath, Mode: string(core.SplitByColumn)}
	base := stemOf(task.SourcePath)
	ts := timestamp()
	multi := len(sheets) > 1
	matchedSheets := 0

	for _, sheet := range sheets {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return result, err
		}
		ok, err := splitOneSheetByColumn(ctx, r, sheet, task, base, ts, multi, emitter, result)
		if err != nil {
			return result, err
		}
		if ok {
			matchedSheets++
		}
	}

	// 所有 Sheet 都没有该列 → 提前给用户明确错误，而不是让结果为空。
	if matchedSheets == 0 {
		return result, core.Wrap("COLUMN_NOT_FOUND",
			"所有选中 Sheet 的表头都找不到列: "+task.SplitColumn, core.ErrColumnNotFound)
	}

	emitter.Progress(core.Progress{Stage: "finalizing",
		Message: fmt.Sprintf("完成：%d 个分组", result.PartsCreated)})
	return result, nil
}

// splitOneSheetByColumn 处理单个 Sheet 的按列值拆分，结果累加到 result。
// 返回值 hasColumn 表示该 Sheet 是否包含目标列。
func splitOneSheetByColumn(
	ctx context.Context, r *excelio.Reader, sheet string,
	task core.SplitTask, base, ts string, multi bool,
	emitter core.EventEmitter, result *Result,
) (hasColumn bool, err error) {
	header, err := r.Header(sheet, task.HeaderRow)
	if err != nil {
		return false, err
	}
	colIdx := findColumn(header, task.SplitColumn)
	if colIdx < 0 {
		// 缺列：单 Sheet 直接报错；多 Sheet 警告后跳过，由主函数统计是否所有都缺。
		if multi {
			emitter.Log(core.LogWarn, fmt.Sprintf("[%s] 表头中找不到列 %q，跳过", sheet, task.SplitColumn))
			return false, nil
		}
		return false, core.Wrap("COLUMN_NOT_FOUND",
			"表头中找不到列: "+task.SplitColumn, core.ErrColumnNotFound)
	}
	headerValues := make([]any, len(header))
	for i, v := range header {
		headerValues[i] = v
	}
	headerHeight, _, _ := r.RowHeight(sheet, task.HeaderRow)
	colWidths, _ := r.ColumnWidths(sheet)

	var picIdx *excelio.PictureIndex
	if task.PreserveImages {
		picIdx, _ = excelio.BuildPictureIndex(r.File(), sheet)
	}

	it, err := r.Iterate(sheet)
	if err != nil {
		return true, err
	}
	defer it.Close()

	parts := map[string]*partWriter{}
	defer func() {
		for _, p := range parts {
			_ = p.close()
		}
	}()

	emitProgress := func(msg string) {
		emitter.Progress(core.Progress{Stage: "writing", Message: msg, Done: int64(result.PartsCreated)})
	}

	for it.Next() {
		if it.RowNum()%500 == 0 {
			if err := pipeline.CheckCancel(ctx); err != nil {
				return true, err
			}
		}
		if it.RowNum() <= task.HeaderRow {
			continue
		}
		cells, err := it.Columns()
		if err != nil {
			return true, err
		}
		key := ""
		if colIdx < len(cells) {
			key = strings.TrimSpace(cells[colIdx])
		}
		if key == "" {
			key = "__空值__"
		}
		p, ok := parts[key]
		if !ok {
			outPath := filepath.Join(task.OutputDir,
				fmt.Sprintf("%s%s_%s_%s.xlsx",
					sanitizeFileName(base), sheetSegment(sheet, multi),
					sanitizeFileName(key), ts))
			p, err = newPartWriter(outPath, sheet)
			if err != nil {
				return true, err
			}
			if err := p.applyColumnWidthsIfNeeded(colWidths); err != nil {
				return true, err
			}
			if _, err := p.writeRow(headerValues, 0, headerHeight); err != nil {
				return true, err
			}
			parts[key] = p
			result.PartsCreated++
			emitProgress(fmt.Sprintf("[%s] 新建分组: %s", sheet, key))
		}

		formulas := readRowFormulas(r, sheet, it.RowNum(), len(cells))
		values := rowToValues(cells, formulas)
		height, _, _ := r.RowHeight(sheet, it.RowNum())
		dstRow, err := p.writeRow(values, it.RowNum(), height)
		if err != nil {
			return true, err
		}
		result.RowsScanned++
		if picIdx != nil {
			if pics := picIdx.PicturesOnRow(it.RowNum()); len(pics) > 0 {
				n, err := p.migratePictures(pics, dstRow)
				result.ImagesMigrated += n
				if err != nil {
					return true, err
				}
			}
		}
	}
	if err := it.Err(); err != nil {
		return true, err
	}

	for _, p := range parts {
		if err := p.save(); err != nil {
			return true, err
		}
		result.OutputFiles = append(result.OutputFiles, p.path)
	}
	return true, nil
}

func findColumn(header []string, name string) int {
	key := strings.ToLower(strings.TrimSpace(name))
	for i, h := range header {
		if strings.ToLower(strings.TrimSpace(h)) == key {
			return i
		}
	}
	return -1
}
