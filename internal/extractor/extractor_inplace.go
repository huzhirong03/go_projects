package extractor

// extractor_inplace.go：批量提取的"写回源文件新 Sheet"分支。
//
// 语义与默认 new_files 路径的差异：
//  1. 不产出新文件，直接在源 xlsx 里新增 Sheet（per_keyword 每关键词 1 个；merged 1 个）。
//  2. 仅"单文件 + xlsx 源"生效。调用 service 会过滤 CSV / 文件夹场景。
//  3. 原汁原味：CopySheetWithin 带走样式/合并/图片，FilterRowsInSheet 只保留命中行，
//     图片自动跟随（复用现有 excelio 能力）。
//  4. per_source 在单文件语义等同 merged，自动降级。

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/matcher"
	"excel-master/internal/pipeline"

	"github.com/xuri/excelize/v2"
)

// extractInplace 走"扫描命中 → 复制源 Sheet → 过滤行 → 原子替换源"的独立路径。
//
// files 的所有 Path 必须一致（单文件），否则拒绝。
func extractInplace(
	ctx context.Context, task core.ExtractTask, files []FileInfo, emitter core.EventEmitter,
) (*Result, error) {
	srcPath, strategy, err := validateInplace(task, files, emitter)
	if err != nil {
		return nil, err
	}

	// schema + matcher 复用默认路径
	schema, err := BuildSchema(files, task.HeaderRow)
	if err != nil {
		return nil, err
	}
	eng := matcher.NewEngine(task.Keywords, task.MatchMode)
	if len(eng.Keywords()) == 0 {
		return nil, core.New("NO_KEYWORDS", "至少需要一个关键词")
	}
	var unifiedSearchCols []int
	if !task.SearchAllCols && len(task.SearchColumns) > 0 {
		unifiedSearchCols = schema.ResolveSearchColumns(task.SearchColumns)
	}

	// 扫描每个 (file, sheet)，收集 sheet -> keyword -> []rowNum
	hits := map[string]map[string][]int{}
	rowsTotal := 0
	total := int64(len(schema.Files))
	for fi, fs := range schema.Files {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return nil, err
		}
		emitter.Progress(core.Progress{
			Stage: "scanning", Done: int64(fi), Total: total,
			Message: fs.File.Path + " [" + fs.File.SheetName + "]",
		})
		sheetHits, matched, err := scanHitsForInplace(ctx, &fs, eng, unifiedSearchCols, task, emitter)
		if err != nil {
			return nil, err
		}
		if matched == 0 {
			continue
		}
		if hits[fs.File.SheetName] == nil {
			hits[fs.File.SheetName] = map[string][]int{}
		}
		for kw, rs := range sheetHits {
			hits[fs.File.SheetName][kw] = append(hits[fs.File.SheetName][kw], rs...)
		}
		rowsTotal += matched
	}
	if rowsTotal == 0 {
		return nil, core.New("NO_MATCHES", "未命中任何行，已取消写回源文件")
	}

	// 可选备份
	if task.BackupSource {
		bak, berr := excelio.BackupCopy(srcPath)
		if berr != nil {
			return nil, berr
		}
		emitter.Log(core.LogInfo, "已生成备份: "+bak)
	}

	// 克隆源到 tmp；必须保留 .xlsx 扩展名，否则 excelize.Save 会拒绝。
	tmpPath := srcPath + ".tmp.xlsx"
	_ = os.Remove(tmpPath)
	if err := excelio.CloneFile(srcPath, tmpPath); err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.Remove(tmpPath) }

	f, err := excelize.OpenFile(tmpPath)
	if err != nil {
		cleanup()
		return nil, core.Wrap("EXCEL_OPEN_FAILED", "打开临时文件失败: "+tmpPath, err)
	}
	defer func() { _ = f.Close() }()

	emitter.Progress(core.Progress{Stage: "finalizing", Done: total, Total: total, Message: "写入新 Sheet"})
	prefix := task.FilenamePrefix
	if prefix == "" {
		prefix = "搜索_"
	}
	multiSheet := len(hits) > 1

	created, err := appendInplaceSheets(f, hits, strategy, prefix, task.HeaderRow, multiSheet)
	if err != nil {
		cleanup()
		return nil, err
	}

	if err := f.Save(); err != nil {
		cleanup()
		return nil, core.Wrap("EXCEL_SAVE_FAILED", "保存临时文件失败", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return nil, core.Wrap("EXCEL_CLOSE_FAILED", "关闭临时文件失败", err)
	}
	if err := excelio.AtomicReplace(srcPath, tmpPath); err != nil {
		cleanup()
		return nil, err
	}

	emitter.Log(core.LogInfo, fmt.Sprintf(
		"inplace 完成：命中 %d 行，新增 %d 个 Sheet 写回 %s", rowsTotal, len(created), srcPath))

	return &Result{
		FilesScanned: 1,
		FilesMatched: 1,
		RowsMatched:  rowsTotal,
		OutputFiles:  []string{srcPath},
		// inplace 图片数不便精确统计（CopySheetWithin 是整 sheet 级别），先报 0；如需后续加一个 GetPictureCells 计数。
		ImagesMigrated: 0,
	}, nil
}

// validateInplace 做 inplace 分支的前置校验，并返回归一化后的：源路径、实际策略。
func validateInplace(
	task core.ExtractTask, files []FileInfo, emitter core.EventEmitter,
) (string, core.OutputStrategy, error) {
	paths := map[string]struct{}{}
	for _, fi := range files {
		paths[fi.Path] = struct{}{}
	}
	if len(paths) != 1 {
		return "", "", core.New("INPLACE_MULTI_FILE",
			"写回源文件新 Sheet 仅支持单文件模式，当前涉及 "+strconv.Itoa(len(paths))+" 个文件")
	}
	srcPath := files[0].Path
	if core.DetectSourceKind(srcPath) == core.SourceCSV {
		return "", "", core.New("INPLACE_CSV_UNSUPPORTED", "CSV 源不支持写回新 Sheet（CSV 无 Sheet 概念）")
	}
	strategy := task.Output
	if strategy == core.OutputPerSource {
		strategy = core.OutputMerged
		emitter.Log(core.LogInfo, "inplace + 单文件：策略 per_source 自动降级为 merged")
	}
	return srcPath, strategy, nil
}

// appendInplaceSheets 在已打开的 f 上，依据策略把 hits 里的命中行落成新 Sheet。
// 返回新增 Sheet 名列表。
func appendInplaceSheets(
	f *excelize.File,
	hits map[string]map[string][]int,
	strategy core.OutputStrategy,
	prefix string,
	headerRow int,
	multiSheet bool,
) ([]string, error) {
	created := []string{}
	for sourceSheet, kwHits := range hits {
		switch strategy {
		case core.OutputPerKeyword:
			for kw, rows := range kwHits {
				name := excelio.UniqueSheetName(f,
					buildInplaceSheetName(prefix, kw, sourceSheet, multiSheet))
				if err := writeInplaceSheet(f, sourceSheet, name, headerRow, rows); err != nil {
					return created, err
				}
				created = append(created, name)
			}
		case core.OutputMerged:
			// 所有关键词的命中行合并去重
			merged := mergeKeywordRows(kwHits)
			if len(merged) == 0 {
				continue
			}
			name := excelio.UniqueSheetName(f,
				buildInplaceSheetName(prefix, "合并", sourceSheet, multiSheet))
			if err := writeInplaceSheet(f, sourceSheet, name, headerRow, merged); err != nil {
				return created, err
			}
			created = append(created, name)
		default:
			return created, core.New("INVALID_STRATEGY",
				"inplace 模式不支持策略: "+string(strategy))
		}
	}
	return created, nil
}

// writeInplaceSheet 在 f 上创建新 Sheet newName（从 sourceSheet 复制），
// 只保留"表头行 + 命中行"。
func writeInplaceSheet(
	f *excelize.File, sourceSheet, newName string, headerRow int, hitRows []int,
) error {
	if err := excelio.CopySheetWithin(f, sourceSheet, newName); err != nil {
		return err
	}
	keep := headerRowsToKeep(headerRow)
	keep = append(keep, hitRows...)
	return excelio.FilterRowsInSheet(f, newName, keep)
}

// mergeKeywordRows 把 kw->rows 合并为不重复升序的行号切片。
func mergeKeywordRows(kwHits map[string][]int) []int {
	set := map[int]struct{}{}
	for _, rows := range kwHits {
		for _, r := range rows {
			set[r] = struct{}{}
		}
	}
	out := make([]int, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	return out
}

// headerRowsToKeep 返回 1..headerRow 的切片；headerRow<=0 时返回 nil。
func headerRowsToKeep(headerRow int) []int {
	if headerRow <= 0 {
		return nil
	}
	out := make([]int, 0, headerRow)
	for r := 1; r <= headerRow; r++ {
		out = append(out, r)
	}
	return out
}

// buildInplaceSheetName 拼接 inplace 新 Sheet 名；multiSheet=true 时带上源 Sheet 名后缀。
func buildInplaceSheetName(prefix, label, sourceSheet string, multiSheet bool) string {
	base := prefix + label
	if multiSheet {
		base = base + "_" + sourceSheet
	}
	return excelio.SanitizeSheetName(base)
}

// scanHitsForInplace 流式扫描单个 (file, sheet)，返回 keyword -> []rowNum 和总命中数。
// 文件占用时弹 retry/skip/cancel；skipped 返回 (nil, 0, nil)。
func scanHitsForInplace(
	ctx context.Context, fs *FileSchema, eng *matcher.Engine,
	unifiedSearchCols []int, task core.ExtractTask, emitter core.EventEmitter,
) (map[string][]int, int, error) {
	var r *excelio.Reader
	var err error
	for {
		switch askOfficeLockDecision(ctx, emitter, fs.File.Path) {
		case fileOpenRetry:
			continue
		case fileOpenSkip:
			return nil, 0, nil
		case fileOpenCancel:
			return nil, 0, core.ErrCanceled
		}
		r, err = excelio.Open(fs.File.Path)
		if err == nil {
			break
		}
		switch askFileOpenDecision(ctx, emitter, fs.File.Path, err) {
		case fileOpenRetry:
			continue
		case fileOpenSkip:
			return nil, 0, nil
		case fileOpenAbort:
			return nil, 0, err
		default:
			return nil, 0, core.ErrCanceled
		}
	}
	defer r.Close()

	fileSearchCols := fs.FileSearchColumns(unifiedSearchCols)
	it, err := r.Iterate(fs.File.SheetName)
	if err != nil {
		return nil, 0, err
	}
	defer it.Close()

	hits := map[string][]int{}
	total := 0
	tScan := time.Now()
	for it.Next() {
		if it.RowNum() <= task.HeaderRow {
			continue
		}
		if it.RowNum()%progressEvery == 0 {
			if err := pipeline.CheckCancel(ctx); err != nil {
				return nil, total, err
			}
		}
		cells, err := it.Columns()
		if err != nil {
			return nil, total, err
		}
		kw := eng.MatchRow(cells, fileSearchCols)
		if kw == "" {
			continue
		}
		hits[kw] = append(hits[kw], it.RowNum())
		total++
	}
	if err := it.Err(); err != nil {
		return nil, total, err
	}
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] inplace 扫描 %v: [%s] 命中 %d",
		time.Since(tScan).Round(time.Millisecond), fs.File.SheetName, total))
	return hits, total, nil
}
