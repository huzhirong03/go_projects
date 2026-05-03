package splitter

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/pipeline"

	"github.com/xuri/excelize/v2"
)

// SplitByColumn：按指定列值分桶拆分（原汁原味路径）。
// 列值相同的行进入同一个输出文件。
// SplitColumn 用列名（需要 HeaderRow > 0）。task.SheetNames 为空表示处理全部 Sheet；
// 多 Sheet 时每个 Sheet 独立拆分（同列值在不同 Sheet 各自一个文件），文件名带 Sheet 名。
//
// 策略：先扫描源文件按列值分桶收集每个 key 命中的行号；
// 然后对每个 key 调 cloneAndExtractSheet(src, out, sheet, [header]+rows) 生成输出。
// 所有样式/合并单元格/公式引用/图片锚点/条件格式都由 excelize 自动维护。
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
	if err := requireXLSXSource(task.SourcePath); err != nil {
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

// splitOneSheetByColumn 处理单个 Sheet 的按列值拆分（原汁原味路径）。
//
// 流程：
//  1. 读表头找到目标列索引；缺列按 multi 语义处理。
//  2. 流式迭代数据行，按列值分桶收集"行号列表"到 map[key][]int（不做任何写入）。
//  3. 一次扫描图片锚点，建 row -> 图片数 索引，供每个 key 输出图片统计。
//  4. 对每个 key，调 cloneAndExtractSheet(src,out,sheet,[header]+rows) 生成原汁原味分片。
//
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

	// 图片行索引：一次扫描，给每个 key 统计图片数用。
	picsOnRow := map[int]int{}
	if task.PreserveImages {
		if cells, err := r.File().GetPictureCells(sheet); err == nil {
			for _, cell := range cells {
				_, row, err := excelize.CellNameToCoordinates(cell)
				if err == nil {
					picsOnRow[row]++
				}
			}
		}
	}

	it, err := r.Iterate(sheet)
	if err != nil {
		return true, err
	}
	defer it.Close()

	// 列值 -> 数据行号列表 (1-based)，保持首次出现顺序让输出稳定
	bucketRows := map[string][]int{}
	keyOrder := []string{}

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
		if _, exists := bucketRows[key]; !exists {
			keyOrder = append(keyOrder, key)
		}
		bucketRows[key] = append(bucketRows[key], it.RowNum())
		result.RowsScanned++
	}
	if err := it.Err(); err != nil {
		return true, err
	}

	// 按首次出现顺序生成每个分组的原汁原味分片
	for _, key := range keyOrder {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return true, err
		}
		rows := bucketRows[key]
		outPath := filepath.Join(task.OutputDir,
			fmt.Sprintf("%s%s_%s_%s.xlsx",
				sanitizeFileName(base), sheetSegment(sheet, multi),
				sanitizeFileName(key), ts))

		keep := make([]int, 0, len(rows)+1)
		if task.HeaderRow > 0 {
			keep = append(keep, task.HeaderRow)
		}
		keep = append(keep, rows...)
		if err := cloneAndExtractSheet(task.SourcePath, outPath, sheet, keep); err != nil {
			return true, err
		}

		imgs := 0
		if task.PreserveImages {
			if task.HeaderRow > 0 {
				imgs += picsOnRow[task.HeaderRow]
			}
			for _, rr := range rows {
				imgs += picsOnRow[rr]
			}
		}
		result.PartsCreated++
		result.ImagesMigrated += imgs
		result.OutputFiles = append(result.OutputFiles, outPath)
		emitter.Progress(core.Progress{
			Stage:   "writing",
			Done:    int64(result.PartsCreated),
			Message: fmt.Sprintf("[%s] 新建分组: %s (%d 行)", sheet, key, len(rows)),
		})
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
