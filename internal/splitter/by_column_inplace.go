package splitter

// by_column_inplace.go：按列值拆分的 inplace 输出分支。
//
// 流程：
//  1. 流式扫描每个 Sheet，按目标列的列值分桶收集行号；
//  2. 对每个 (sheet, key) 生成一个 InplacePart（label = 列值，KeepRows = [表头]+rows）；
//  3. 交给 runInplaceSplit 写回源文件并替换。

import (
	"context"
	"fmt"
	"strings"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/pipeline"
)

func splitByColumnInplace(
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

	plans := []InplacePlan{}
	rowsScanned := 0
	matchedSheets := 0

	for _, sheet := range sheets {
		if err := pipeline.CheckCancel(ctx); err != nil {
			_ = r.Close()
			return nil, err
		}
		header, err := r.Header(sheet, task.HeaderRow)
		if err != nil {
			// 完全空的 Sheet 跳过（跟"缺列"处理一致），不要破坏整个拆分任务。
			if core.IsEmptySheet(err) {
				emitter.Log(core.LogWarn, fmt.Sprintf("[%s] 空 Sheet（0 行数据），跳过", sheet))
				continue
			}
			_ = r.Close()
			return nil, err
		}
		colIdx := findColumn(header, task.SplitColumn)
		if colIdx < 0 {
			if len(sheets) > 1 {
				emitter.Log(core.LogWarn, fmt.Sprintf("[%s] 表头中找不到列 %q，跳过", sheet, task.SplitColumn))
				continue
			}
			_ = r.Close()
			return nil, core.Wrap("COLUMN_NOT_FOUND",
				"表头中找不到列: "+task.SplitColumn, core.ErrColumnNotFound)
		}
		matchedSheets++

		// 公式回退精准求值（方案 A+），与 splitOneSheetByColumn 同款。
		// 拆分列是公式列且无 <v> 缓存时保证 key 不全是空值。真实业务文件零开销。
		var formulaValues map[string]string
		if uncached, uerr := r.UncachedFormulas(sheet); uerr != nil {
			emitter.Log(core.LogWarn, fmt.Sprintf("[%s] 获取无缓存公式列表失败，跳过回退: %s", sheet, uerr.Error()))
		} else if len(uncached) > 0 {
			if vals, stats, eerr := r.EvalFormulasAtWithStats(sheet, uncached); eerr != nil {
				emitter.Log(core.LogWarn, fmt.Sprintf("[%s] 公式精准求值失败: %s", sheet, eerr.Error()))
			} else {
				formulaValues = vals
				emitter.Log(core.LogInfo, fmt.Sprintf("[%s] 公式精准求值 %v: 请求=%d 算出=%d 跳过(跨sheet)=%d 跳过(超预算)=%d",
					sheet, stats.Elapsed.Round(1000000), stats.Requested, stats.Computed,
					stats.SkippedCrossSheet, stats.SkippedBudget))
			}
		}

		it, err := r.Iterate(sheet)
		if err != nil {
			_ = r.Close()
			return nil, err
		}
		bucketRows := map[string][]int{}
		keyOrder := []string{}
		for it.Next() {
			if it.RowNum()%500 == 0 {
				if err := pipeline.CheckCancel(ctx); err != nil {
					it.Close()
					_ = r.Close()
					return nil, err
				}
			}
			if it.RowNum() <= task.HeaderRow {
				continue
			}
			cells, err := it.Columns()
			if err != nil {
				it.Close()
				_ = r.Close()
				return nil, err
			}
			if len(formulaValues) > 0 {
				cells = excelio.FillRowCellsWithFormulaValues(cells, it.RowNum(), formulaValues)
			}
			key := ""
			if colIdx < len(cells) {
				key = strings.TrimSpace(cells[colIdx])
			}
			if key == "" {
				key = "__空值__"
			}
			if _, ok := bucketRows[key]; !ok {
				keyOrder = append(keyOrder, key)
			}
			bucketRows[key] = append(bucketRows[key], it.RowNum())
			rowsScanned++
		}
		if err := it.Err(); err != nil {
			it.Close()
			_ = r.Close()
			return nil, err
		}
		it.Close()

		parts := make([]InplacePart, 0, len(keyOrder))
		for _, key := range keyOrder {
			rows := bucketRows[key]
			keep := make([]int, 0, len(rows)+1)
			if task.HeaderRow > 0 {
				keep = append(keep, task.HeaderRow)
			}
			keep = append(keep, rows...)
			parts = append(parts, InplacePart{Label: key, KeepRows: keep})
		}
		if len(parts) > 0 {
			plans = append(plans, InplacePlan{SourceSheet: sheet, Parts: parts})
		}
	}
	_ = r.Close()

	if matchedSheets == 0 {
		return nil, core.Wrap("COLUMN_NOT_FOUND",
			"所有选中 Sheet 的表头都找不到列: "+task.SplitColumn, core.ErrColumnNotFound)
	}
	if len(plans) == 0 {
		return nil, core.New("NO_PARTS", "源文件没有可拆分的数据行")
	}

	emitter.Progress(core.Progress{Stage: "finalizing",
		Message: fmt.Sprintf("写回源文件：%d 个 Sheet，%d 个分组",
			len(plans), countParts(plans))})

	created, err := runInplaceSplit(task.SourcePath, "拆_", plans, task.BackupSource)
	if err != nil {
		return nil, err
	}
	return &Result{
		SourceFile:   task.SourcePath,
		Mode:         string(core.SplitByColumn),
		RowsScanned:  rowsScanned,
		PartsCreated: len(created),
		OutputFiles:  []string{task.SourcePath},
	}, nil
}
