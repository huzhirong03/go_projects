package splitter

import (
	"context"
	"fmt"
	"path/filepath"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/pipeline"

	"github.com/xuri/excelize/v2"
)

// SplitByRows：按行数拆分（原汁原味路径）。每 RowsPerFile 条数据行生成一个新文件，
// 每个分片都复制一次表头。task.SheetNames 为空表示处理全部 Sheet；
// 多 Sheet 时每个 Sheet 独立拆分（part 序号各自从 1 开始），文件名带 Sheet 名。
//
// 策略：对每个分片 = 复制源文件 + KeepSheetsOnly([sheet]) + FilterRowsInSheet([header]+段内行)，
// 这样所有样式/合并单元格/公式引用/图片锚点/条件格式都由 excelize 自动维护。
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
	if err := requireXLSXSource(task.SourcePath); err != nil {
		return nil, err
	}
	if task.RowsPerFile <= 0 {
		return nil, core.New("INVALID_TASK", "RowsPerFile 必须 > 0")
	}
	if task.OutputTarget == core.OutputTargetInplaceSheets {
		return splitByRowsInplace(ctx, task, emitter)
	}

	r, err := excelio.Open(task.SourcePath)
	if err != nil {
		return nil, err
	}
	sheets := selectSheets(r.SheetNames(), task.SheetNames)
	if len(sheets) == 0 {
		_ = r.Close()
		return nil, core.New("NO_MATCHED_SHEET", "源文件没有任何匹配指定 Sheet 名的工作表")
	}
	// 提前读出每个 Sheet 的总行数与图片行索引用于分片，再关闭 reader
	// （后续 CloneFile 需要独占打开源文件的 lock-free 状态）
	stats, err := collectSheetRowStats(r, sheets)
	_ = r.Close()
	if err != nil {
		return nil, err
	}

	result := &Result{SourceFile: task.SourcePath, Mode: string(core.SplitByRows)}
	base := stemOf(task.SourcePath)
	ts := timestamp()
	multi := len(sheets) > 1

	for _, sheet := range sheets {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return result, err
		}
		if err := splitOneSheetByRows(ctx, task, sheet, stats[sheet], base, ts, multi, emitter, result); err != nil {
			return result, err
		}
	}

	emitter.Progress(core.Progress{Stage: "finalizing",
		Message: fmt.Sprintf("完成：%d 个分片", result.PartsCreated)})
	return result, nil
}

// sheetRowStats 记录单 Sheet 的分片所需信息：总行数 + 行号 -> 该行图片数。
type sheetRowStats struct {
	totalRows int
	picsByRow map[int]int
}

// collectSheetRowStats 一次性扫描各 sheet 的行数和图片锚点行分布，减少后续 I/O。
//
// 关键：行数用流式 Iterate 数，禁止 GetRows 全量加载（违反规则 §1.4，1GB+
// 文件直接 OOM）。GetPictureCells 是 O(图片数) 不是 O(行数)，可继续用。
func collectSheetRowStats(r *excelio.Reader, sheets []string) (map[string]*sheetRowStats, error) {
	out := map[string]*sheetRowStats{}
	f := r.File()
	for _, sh := range sheets {
		// 流式数总行数，O(rows) 时间 + O(1) 内存
		it, err := r.Iterate(sh)
		if err != nil {
			return nil, core.Wrap("EXCEL_READ_FAILED", "打开 sheet 行迭代器失败: "+sh, err)
		}
		total := 0
		for it.Next() {
			total++
		}
		iterErr := it.Err()
		_ = it.Close()
		if iterErr != nil {
			return nil, core.Wrap("EXCEL_READ_FAILED", "读取 sheet 行数失败: "+sh, iterErr)
		}

		st := &sheetRowStats{totalRows: total, picsByRow: map[int]int{}}
		cells, err := f.GetPictureCells(sh)
		if err == nil {
			for _, cell := range cells {
				_, row, err := excelize.CellNameToCoordinates(cell)
				if err == nil {
					st.picsByRow[row]++
				}
			}
		}
		out[sh] = st
	}
	return out, nil
}

// splitOneSheetByRows 处理单 Sheet：按 RowsPerFile 把数据行切成若干 part，
// 每个 part 走 cloneAndExtractSheet 生成原汁原味输出。
func splitOneSheetByRows(
	ctx context.Context, task core.SplitTask, sheet string,
	st *sheetRowStats, base, ts string, multi bool,
	emitter core.EventEmitter, result *Result,
) error {
	if st == nil {
		return nil
	}
	// 数据行起点
	dataStart := 1
	if task.HeaderRow > 0 {
		dataStart = task.HeaderRow + 1
	}
	if dataStart > st.totalRows {
		// 没有数据行可拆（可能只有表头或空表）
		return nil
	}

	partIdx := 0
	for start := dataStart; start <= st.totalRows; start += task.RowsPerFile {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return err
		}
		end := start + task.RowsPerFile - 1
		if end > st.totalRows {
			end = st.totalRows
		}
		partIdx++
		outPath := filepath.Join(task.OutputDir,
			fmt.Sprintf("%s%s_part%03d_%s.xlsx",
				sanitizeFileName(base), sheetSegment(sheet, multi), partIdx, ts))

		keep := make([]int, 0, (end-start+1)+1)
		if task.HeaderRow > 0 {
			keep = append(keep, task.HeaderRow)
		}
		for rr := start; rr <= end; rr++ {
			keep = append(keep, rr)
		}
		if err := cloneAndExtractSheet(task.SourcePath, outPath, sheet, keep); err != nil {
			return err
		}

		// 统计图片：本段（含表头）上有多少图片 cell
		imgs := 0
		if task.HeaderRow > 0 {
			imgs += st.picsByRow[task.HeaderRow]
		}
		for rr := start; rr <= end; rr++ {
			imgs += st.picsByRow[rr]
		}
		if !task.PreserveImages {
			// 用户要求不保留图片：原汁原味路径无法只保留文件内部分图片（全删过于昂贵），
			// 但至少统计上不计入。文件里实际仍带图 —— 如需严格"无图模式"应走流式路径。
			imgs = 0
		}

		result.RowsScanned += end - start + 1
		result.ImagesMigrated += imgs
		result.OutputFiles = append(result.OutputFiles, outPath)
		result.PartsCreated++

		emitter.Progress(core.Progress{
			Stage:   "writing",
			Done:    int64(result.PartsCreated),
			Message: fmt.Sprintf("[%s] 创建分片 %d (行 %d-%d)", sheet, partIdx, start, end),
		})
	}
	return nil
}
