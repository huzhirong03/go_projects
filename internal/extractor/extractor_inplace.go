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
	"excel-master/internal/filter"
	"excel-master/internal/matcher"
	"excel-master/internal/pipeline"
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
	if !eng.HasKeywords() && task.AdvancedFilter.IsEmpty() {
		return nil, core.New("NO_RULES", "至少需要一个关键词或一条高级筛选条件")
	}
	var unifiedSearchCols []int
	if !task.SearchAllCols && len(task.SearchColumns) > 0 {
		unifiedSearchCols = schema.ResolveSearchColumns(task.SearchColumns)
	}

	// 构造 deduper：inplace 是单文件路径，所有 sheet 共用同一 deduper 实例维持已见 key。
	// Bind 的列名来源：用源文件第一个 sheet 的 headers（inplace 是单文件，同文件不同 sheet 的
	// headers 可能不同但并不常见；本版简化使用第一个 sheet 的 headers）。找不到列时 deduper 自动 no-op。
	dedup := newDeduper(task.DedupColumn)
	if len(schema.Files) > 0 {
		dedup.Bind(schema.Files[0].File.Headers)
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
		// 高级筛选编译（inplace 是单文件路径，但保持跟批量提取一致的处理：
		// 缺失列直接报硬错误而非“跳过文件”——单文件下“跳过”等于整个任务空跑）。
		flt, missing, ferr := buildFilter(task.AdvancedFilter, fs.File.Headers)
		if ferr != nil {
			return nil, core.Wrap("FILTER_COMPILE_FAILED", "高级筛选编译失败", ferr)
		}
		if len(missing) > 0 {
			emitter.Log(core.LogWarn, fmt.Sprintf("[%s / %s] 高级筛选列缺失 %v",
				fs.File.Path, fs.File.SheetName, missing))
		}
		sheetHits, matched, err := scanHitsForInplace(ctx, &fs, eng, unifiedSearchCols, task, flt, dedup, strategy, emitter)
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

	emitter.Progress(core.Progress{Stage: "finalizing", Done: total, Total: total, Message: "构造新 Sheet"})
	prefix := task.FilenamePrefix
	if prefix == "" {
		prefix = "搜索_"
	}
	multiSheet := len(hits) > 1

	// 把 hits 转换为 InplaceSheetSpec 列表（带唯一化命名）
	existingNames, err := excelio.ListSheetNamesZip(srcPath)
	if err != nil {
		return nil, err
	}
	nameSet := map[string]struct{}{}
	for _, n := range existingNames {
		nameSet[n] = struct{}{}
	}
	specs := buildInplaceSpecs(hits, strategy, prefix, task.HeaderRow, multiSheet, nameSet)
	if len(specs) == 0 {
		return nil, core.New("NO_INPLACE_SPECS", "未生成任何待写回的新 Sheet")
	}

	// 一次性 zip 手术：复制原 xlsx + 追加 N 个过滤后的新 Sheet 到 tmp，避开 excelize.RemoveRow O(N²) 陷阱
	tmpPath := srcPath + ".tmp.xlsx"
	_ = os.Remove(tmpPath)
	cleanup := func() { _ = os.Remove(tmpPath) }
	tZip := time.Now()
	if err := excelio.AddFilteredSheetsZip(srcPath, tmpPath, specs); err != nil {
		cleanup()
		return nil, err
	}
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] inplace zip 手术 %v：追加 %d 个 Sheet",
		time.Since(tZip).Round(time.Millisecond), len(specs)))

	if err := excelio.AtomicReplace(srcPath, tmpPath); err != nil {
		cleanup()
		return nil, err
	}
	created := make([]string, 0, len(specs))
	for _, s := range specs {
		created = append(created, s.NewSheetName)
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
	// 无关键词（仅高级筛选）时 per_keyword 没有分组维度，自动降级 merged
	if strategy == core.OutputPerKeyword && len(task.Keywords) == 0 {
		strategy = core.OutputMerged
		emitter.Log(core.LogInfo, "inplace + 仅高级筛选：策略 per_keyword 自动降级为 merged")
	}
	return srcPath, strategy, nil
}

// buildInplaceSpecs 把 hits 转为一批 InplaceSheetSpec，名字动态结合 nameSet 去重。
// nameSet 会被就地更新（加入本次生成的名字，避免 spec 内部重名）。
func buildInplaceSpecs(
	hits map[string]map[string][]int,
	strategy core.OutputStrategy,
	prefix string,
	headerRow int,
	multiSheet bool,
	nameSet map[string]struct{},
) []excelio.InplaceSheetSpec {
	headerRows := headerRowsToKeep(headerRow)
	specs := []excelio.InplaceSheetSpec{}
	for sourceSheet, kwHits := range hits {
		switch strategy {
		case core.OutputPerKeyword:
			for kw, rows := range kwHits {
				base := buildInplaceSheetName(prefix, kw, sourceSheet, multiSheet)
				name := excelio.UniqueNameInSet(base, nameSet)
				keep := append([]int{}, headerRows...)
				keep = append(keep, rows...)
				specs = append(specs, excelio.InplaceSheetSpec{
					SourceSheet: sourceSheet, NewSheetName: name, KeepRows: keep,
				})
			}
		case core.OutputMerged:
			merged := mergeKeywordRows(kwHits)
			if len(merged) == 0 {
				continue
			}
			base := buildInplaceSheetName(prefix, "合并", sourceSheet, multiSheet)
			name := excelio.UniqueNameInSet(base, nameSet)
			keep := append([]int{}, headerRows...)
			keep = append(keep, merged...)
			specs = append(specs, excelio.InplaceSheetSpec{
				SourceSheet: sourceSheet, NewSheetName: name, KeepRows: keep,
			})
		}
	}
	return specs
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
//
// flt：编译好的高级筛选；nil/IsZero 表示无筛选，关键词命中行直接计入。
//
// dedup：去重器；no-op 时不影响性能。strategy 决定去重桶：
//   - OutputPerKeyword -> bucket = kw（每个新 Sheet 内部独立去重）
//   - OutputMerged     -> bucket = ""（所有 kw 合并后全局去重）
func scanHitsForInplace(
	ctx context.Context, fs *FileSchema, eng *matcher.Engine,
	unifiedSearchCols []int, task core.ExtractTask, flt *filter.Filter,
	dedup *deduper, strategy core.OutputStrategy, emitter core.EventEmitter,
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
		var kw string
		if eng.HasKeywords() {
			kw = eng.MatchRow(cells, fileSearchCols)
			if kw == "" {
				continue
			}
		} else {
			// 仅高级筛选场景，没有关键词分组维度——用空 key 收集到单个 bucket。
			// 上层 buildInplaceSpecs 看到空 keyword 会走 merged 路径生成单个新 sheet。
			kw = ""
		}
		// 高级筛选：关键词命中后立即应用，未通过的行不计入 inplace 写回。
		if !flt.Apply(cells) {
			continue
		}
		// 去重判断：按 strategy 决定 bucket。cells 是 []string，包装成 []any 适配 deduper 接口。
		var bucket string
		if strategy == core.OutputPerKeyword {
			bucket = kw
		}
		if dedup.Enabled() {
			rowAny := make([]any, len(cells))
			for i, c := range cells {
				rowAny[i] = c
			}
			if dedup.ShouldDrop(bucket, rowAny) {
				continue
			}
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
