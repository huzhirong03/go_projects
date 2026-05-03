package splitter

// by_rows_inplace.go：按行数拆分的 inplace 输出分支。
//
// 流程：
//  1. 打开源文件读出每个 Sheet 的总行数；
//  2. 按 RowsPerFile 切成 N 个 part，每个 part 的 keepRows = [表头] + [start..end]；
//  3. 把所有 plan 交给 runInplaceSplit 做 Clone+CopySheetWithin+FilterRowsInSheet+AtomicReplace。
//
// 与"输出新文件"分支共用 collectSheetRowStats 拿行数。

import (
	"context"
	"fmt"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/pipeline"
)

func splitByRowsInplace(
	ctx context.Context, task core.SplitTask, emitter core.EventEmitter,
) (*Result, error) {
	r, err := excelio.Open(task.SourcePath)
	if err != nil {
		return nil, err
	}
	sheets := selectSheets(r.SheetNames(), task.SheetNames)
	if len(sheets) == 0 {
		_ = r.Close()
		return nil, core.New("NO_MATCHED_SHEET", "源文件没有任何匹配指定 Sheet 名的工作表")
	}
	stats, err := collectSheetRowStats(r.File(), sheets)
	_ = r.Close()
	if err != nil {
		return nil, err
	}

	plans := []InplacePlan{}
	totalRows := 0
	for _, sheet := range sheets {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return nil, err
		}
		st := stats[sheet]
		if st == nil {
			continue
		}
		dataStart := 1
		if task.HeaderRow > 0 {
			dataStart = task.HeaderRow + 1
		}
		if dataStart > st.totalRows {
			continue
		}
		parts := []InplacePart{}
		partIdx := 0
		for start := dataStart; start <= st.totalRows; start += task.RowsPerFile {
			end := start + task.RowsPerFile - 1
			if end > st.totalRows {
				end = st.totalRows
			}
			partIdx++
			keep := make([]int, 0, (end-start+1)+1)
			if task.HeaderRow > 0 {
				keep = append(keep, task.HeaderRow)
			}
			for rr := start; rr <= end; rr++ {
				keep = append(keep, rr)
			}
			parts = append(parts, InplacePart{
				Label:    fmt.Sprintf("part%03d", partIdx),
				KeepRows: keep,
			})
			totalRows += end - start + 1
		}
		if len(parts) > 0 {
			plans = append(plans, InplacePlan{SourceSheet: sheet, Parts: parts})
		}
	}
	if len(plans) == 0 {
		return nil, core.New("NO_PARTS", "源文件没有可拆分的数据行")
	}

	emitter.Progress(core.Progress{Stage: "finalizing",
		Message: fmt.Sprintf("写回源文件：%d 个 Sheet，%d 个分片",
			len(plans), countParts(plans))})

	created, err := runInplaceSplit(task.SourcePath, "拆_", plans, task.BackupSource)
	if err != nil {
		return nil, err
	}
	return &Result{
		SourceFile:     task.SourcePath,
		Mode:           string(core.SplitByRows),
		RowsScanned:    totalRows,
		PartsCreated:   len(created),
		ImagesMigrated: 0, // inplace 不便精确统计图片数，留给后续若需要
		OutputFiles:    []string{task.SourcePath},
	}, nil
}

func countParts(plans []InplacePlan) int {
	n := 0
	for _, p := range plans {
		n += len(p.Parts)
	}
	return n
}
