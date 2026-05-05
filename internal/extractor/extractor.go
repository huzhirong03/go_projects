package extractor

import (
	"context"
	"fmt"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/filter"
	"excel-master/internal/matcher"
	"excel-master/internal/pipeline"
)

// 每处理 N 行检查一次 context 取消与发射进度事件。
const progressEvery = 100

// Extract 是文件夹批量提取的总入口。
// 流程：Scan -> Schema -> MatchEngine -> 遍历每个文件流式匹配 -> OutputWriter -> Finalize
func Extract(ctx context.Context, task core.ExtractTask, emitter core.EventEmitter) (*Result, error) {
	if emitter == nil {
		emitter = pipeline.NoopEmitter{}
	}
	if err := validateTask(task); err != nil {
		return nil, err
	}

	// 1. 扫描（每个文件按 Sheet 展开为多个单元，受 task.SheetNames 过滤）
	pipeline.LogMem(emitter, "task start")
	emitter.Progress(core.Progress{Stage: "scanning", Message: "扫描文件夹"})
	tScan := time.Now()
	files, err := scanFolderInteractive(ctx, task.FolderPath, task.HeaderRow, task.SheetNames, emitter)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, core.New("NO_FILES", "文件夹内没有可处理的 Sheet（检查 .xlsx 文件 / Sheet 选择）")
	}
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 扫描源路径 %v: %d 个 Sheet",
		time.Since(tScan).Round(time.Millisecond), len(files)))
	pipeline.LogMem(emitter, "after probe")
	// 大文件前置警告（不阻断业务）：把"将处理多大数据 / 大概等多久"提前告诉用户，
	// 避免学员看到 UI 不动以为程序卡死。仅按"去重路径"统计，避免一个文件多 sheet 重复计入。
	pipeline.SizeBanner(emitter, distinctFilePaths(files))
	return ExtractUnits(ctx, task, files, emitter)
}

// ExtractUnits 接受已经扫描好的 (file, sheet) 单元列表执行提取。
// 用于"单文件按关键词拆分"等无需重新扫描文件夹的复用场景。
// 调用方需自己保证 files 非空且 task.OutputDir 等字段已填好；
// validateTask 仍会运行（但允许 FolderPath 为空，此时校验放宽）。
func ExtractUnits(
	ctx context.Context, task core.ExtractTask, files []FileInfo, emitter core.EventEmitter,
) (*Result, error) {
	if emitter == nil {
		emitter = pipeline.NoopEmitter{}
	}
	if err := validateTaskRelaxed(task); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, core.New("NO_FILES", "没有可处理的 Sheet 单元")
	}
	emitter.Log(core.LogInfo, fmt.Sprintf("共发现 %d 个 Sheet（来自 %d 个文件）",
		len(files), countDistinctPaths(files)))

	// inplace 分支：把结果作为新 Sheet 写回源文件，不产出新文件。
	// 仅单文件 + xlsx 源生效，文件夹 / CSV 在此拦截。
	if task.OutputTarget == core.OutputTargetInplaceSheets {
		return extractInplace(ctx, task, files, emitter)
	}

	// 2. 统一 schema
	schema, err := BuildSchema(files, task.HeaderRow)
	if err != nil {
		return nil, err
	}

	// 3. 匹配引擎
	eng := matcher.NewEngine(task.Keywords, task.MatchMode)
	hasFilter := !task.AdvancedFilter.IsEmpty()
	if len(eng.Keywords()) == 0 {
		// 允许"只用高级筛选"，但必须至少有一种规则
		if !hasFilter {
			return nil, core.New("NO_RULES", "至少需要一个关键词或一条高级筛选条件")
		}
		// per_keyword 在无关键词时没有分组维度，自动降级 merged
		if task.Output == core.OutputPerKeyword {
			task.Output = core.OutputMerged
		}
	}

	// 4. 指定搜索列翻译成统一列索引（nil 表示全列）
	var unifiedSearchCols []int
	if !task.SearchAllCols && len(task.SearchColumns) > 0 {
		unifiedSearchCols = schema.ResolveSearchColumns(task.SearchColumns)
	}

	// 5. 选输出策略
	dedupCfg := buildDedupConfig(task.DedupColumn, task.DedupColumns, task.DedupIgnoreSpace, task.DedupIgnoreCase)
	// allXlsxSources：用于 per_keyword 策略选择 surgery vs excelize writer。
	// 只要所有源都是 xlsx 就走 zip 手术：
	//   - 单源 → ExtractToNewFileSurgery（100% 字节级复刻）
	//   - 多源 → CloneAndMergePreserved（primary 样板 + secondary 数据嫁接，
	//             同模板克隆场景 ~99% 复刻）
	// CSV 源不是 zip不能手术，保留老流式 excelize 路径。
	allXlsxSources := isAllXlsxSources(files)
	ow, imgCounterFn, err := newOutputWriter(task.Output, task.OutputDir, task.HeaderRow, task.SheetNames, task.FilenamePrefix, dedupCfg, allXlsxSources)
	if err != nil {
		return nil, err
	}
	defer ow.Close()
	if err := ow.Begin(schema); err != nil {
		return nil, err
	}

	// 6. 主循环。FilesScanned/FilesMatched 按"去重路径"统计，更符合用户直觉。
	result := &Result{FilesScanned: countDistinctPaths(files)}
	total := int64(len(schema.Files))
	matchedPaths := map[string]bool{}
	skippedPaths := map[string]bool{}
	for fi, fs := range schema.Files {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return nil, err
		}
		if skippedPaths[fs.File.Path] {
			continue
		}
		emitter.Progress(core.Progress{
			Stage:   "reading",
			Done:    int64(fi),
			Total:   total,
			Message: fs.File.Path + " [" + fs.File.SheetName + "]",
		})

		// 高级筛选按文件编译（headers 可能因文件而异）。
		decision, ferr := buildFilterForFile(task.AdvancedFilter, &fs)
		if ferr != nil {
			return nil, core.Wrap("FILTER_COMPILE_FAILED", "高级筛选编译失败", ferr)
		}
		if decision.SkipReason != "" {
			emitter.Log(core.LogWarn, decision.SkipReason)
			skippedPaths[fs.File.Path] = true
			continue
		}
		if len(decision.PartialMissing) > 0 {
			emitter.Log(core.LogWarn, fmt.Sprintf(
				"[%s / %s] 部分高级筛选列缺失 %v，仅用现有条件继续",
				fs.File.Path, fs.File.SheetName, decision.PartialMissing))
		}

		var matched int
		var skipped bool
		switch core.DetectSourceKind(fs.File.Path) {
		case core.SourceCSV:
			matched, skipped, err = processCSVFile(ctx, &fs, schema, eng, unifiedSearchCols, task, ow, decision.Filter, emitter)
		default:
			matched, skipped, err = processFile(ctx, &fs, schema, eng, unifiedSearchCols, task, ow, decision.Filter, emitter)
		}
		if err != nil {
			return nil, err
		}
		if skipped {
			skippedPaths[fs.File.Path] = true
			continue
		}
		if matched > 0 {
			matchedPaths[fs.File.Path] = true
			result.RowsMatched += matched
		}
	}
	result.FilesMatched = len(matchedPaths)

	// 7. 落盘
	pipeline.LogMem(emitter, "before finalize")
	emitter.Progress(core.Progress{Stage: "finalizing", Done: total, Total: total, Message: "写入输出文件"})
	tFinalize := time.Now()
	var paths []string
	if pf, ok := ow.(PromptFinalizer); ok {
		paths, err = pf.FinalizeWithPrompt(ctx, emitter)
	} else {
		paths, err = ow.Finalize()
	}
	if err != nil {
		return nil, err
	}
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] finalize %v: 输出 %d 个文件",
		time.Since(tFinalize).Round(time.Millisecond), len(paths)))
	pipeline.LogMem(emitter, "after finalize")
	result.OutputFiles = paths
	result.ImagesMigrated = imgCounterFn()

	emitter.Log(core.LogInfo, fmt.Sprintf("完成：命中 %d 行，迁移图片 %d 张，输出 %d 个文件",
		result.RowsMatched, result.ImagesMigrated, len(result.OutputFiles)))
	return result, nil
}

// processFile 处理单个源文件：打开 → 构图片索引 → 流式行迭代 → 命中派发。
//
// flt：编译好的高级筛选；nil 或 IsZero 表示"无筛选"，所有命中行直接通过。
// 关键词命中后会立即跑 flt.Apply(cells)，未通过的行不会触发图片加载/EmitRow。
func processFile(
	ctx context.Context,
	fs *FileSchema,
	schema *UnifiedSchema,
	eng *matcher.Engine,
	unifiedSearchCols []int,
	task core.ExtractTask,
	ow OutputWriter,
	flt *filter.Filter,
	emitter core.EventEmitter,
) (int, bool, error) {
	var r *excelio.Reader
	var err error
	tOpen := time.Now()
	for {
		switch askOfficeLockDecision(ctx, emitter, fs.File.Path) {
		case fileOpenRetry:
			continue
		case fileOpenSkip:
			emitter.Log(core.LogWarn, "已跳过正在打开的文件: "+fs.File.Path)
			return 0, true, nil
		case fileOpenCancel:
			return 0, false, core.ErrCanceled
		}
		r, err = excelio.Open(fs.File.Path)
		if err == nil {
			break
		}
		switch askFileOpenDecision(ctx, emitter, fs.File.Path, err) {
		case fileOpenRetry:
			continue
		case fileOpenSkip:
			emitter.Log(core.LogWarn, "已跳过无法读取的文件: "+fs.File.Path)
			return 0, true, nil
		case fileOpenAbort:
			return 0, false, err
		default:
			return 0, false, core.ErrCanceled
		}
	}
	defer r.Close()
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 打开文件 %v: %s [%s]",
		time.Since(tOpen).Round(time.Millisecond), fs.File.Path, fs.File.SheetName))

	// 阶段 1（新路径）：用 archive/zip 直读 xlsx，解析 drawing.xml 建 row→anchor 索引。
	// 比 excelize O(N²) 的 GetPictures 路径快 1-2 个数量级；
	// zip 打开失败或没图时降级为"不带图"继续，不阻断主流程。
	var rowToRefs map[int][]excelio.PictureCellRef
	var zipSrc *excelio.ZipImageSource
	if task.PreserveImages {
		emitter.Progress(core.Progress{
			Stage:   "indexing-picture-meta",
			Message: "扫描图片位置: " + fs.File.Path + " [" + fs.File.SheetName + "]",
		})
		tStage := time.Now()
		zipSrc, err = excelio.OpenZipImageSource(fs.File.Path)
		if err != nil {
			emitter.Log(core.LogWarn, "zip 打开失败，跳过图片: "+err.Error())
		} else {
			defer zipSrc.Close()
			if err := zipSrc.LoadSheetAnchors(fs.File.SheetName); err != nil {
				emitter.Log(core.LogWarn, "扫描 drawing.xml 失败，跳过图片: "+err.Error())
				zipSrc = nil
			} else {
				rowToRefs = zipSrc.PictureCellsByRow(fs.File.SheetName)
				emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 扫描图片元数据(zip) %v: 共 %d 行有图",
					time.Since(tStage).Round(time.Millisecond), len(rowToRefs)))
			}
		}
	}

	fileSearchCols := fs.FileSearchColumns(unifiedSearchCols)

	// 阶段 1.5 + 1.8（B5 合并）：一次 zip 扫描同时产出"是否含公式"和"自定义行高 map"。
	// 合并前两阶段各 2.3~2.4s，独立 zip.OpenReader + xml.Decoder 扫同一份 sheetN.xml；
	// 合并后单次扫描约 2.4s 直接搞定两件事。
	//
	// 语义保留：
	//   - hasFormulas=true 时 extractor 仍走 readRowFormulas 保留公式（fixture 02 零回归）
	//   - heightMap 命中即用；nil 时降级到 r.RowHeight 逐行查询（行为完全一致）
	//
	// 失败策略：探测失败时按"含公式 + 空行高"保守处理，确保公式业务不丢，性能降级可接受。
	tProbe := time.Now()
	hasFormulas, heightMap, probeErr := r.ProbeSheet(fs.File.SheetName)
	if probeErr != nil {
		emitter.Log(core.LogWarn, "sheet 预检失败，按\"含公式+空行高\"保守处理: "+probeErr.Error())
		hasFormulas = true
		heightMap = nil
	}
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] sheet 预检 %v: hasFormulas=%v, 自定义行高=%d 行",
		time.Since(tProbe).Round(time.Millisecond), hasFormulas, len(heightMap)))

	// 阶段 1.9：公式回退精准求值（方案 A+，v1.4.1）。
	//
	// ProbeSheet 在 zip 流式扫描时已经顺手收集了"有 <f> 无 <v>" cell 的 ref + 公式文本
	// （写入 r.uncachedFormulasCache）。这里只对这些 ref 批量调 CalcCellValue。
	//
	// 性能保证：
	//   - 真实业务文件（>99% 公式都有 <v> 缓存）→ uncached map 为空 → 完全跳过求值
	//     （扫描阶段性能跟 v1.3.1 完全一致，无回归）
	//   - fixture 04 / 用户未保存文件 → uncached map 里有若干 ref → 只算这些 cell，
	//     不扫全表，fixture 04 (6000 cell) 约 2-3 秒
	//
	// 为什么不用 v1.4.0 的 EvaluateFormulas 全表扫：那个方案不管文件是否已有缓存
	// 都会用 excelize.Rows 遍历整个 sheet 查每个 cell，10 万行场景慢 20 秒（严重回归）。
	var formulaValues map[string]string
	if hasFormulas {
		uncached, uerr := r.UncachedFormulas(fs.File.SheetName)
		if uerr != nil {
			emitter.Log(core.LogWarn, "获取无缓存公式 cell 列表失败，跳过回退求值: "+uerr.Error())
		} else if len(uncached) > 0 {
			tFormula := time.Now()
			values, stats, eerr := r.EvalFormulasAtWithStats(fs.File.SheetName, uncached)
			if eerr != nil {
				emitter.Log(core.LogWarn, "公式精准求值失败，跳过回退: "+eerr.Error())
				formulaValues = nil
			} else {
				formulaValues = values
				emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 公式精准求值 %v: 请求=%d 算出=%d 跳过(跨sheet)=%d 跳过(超预算)=%d",
					time.Since(tFormula).Round(time.Millisecond),
					stats.Requested, stats.Computed, stats.SkippedCrossSheet, stats.SkippedBudget))
			}
		}
	}

	// 阶段 2：流式行迭代，收集命中行到内存（不加载图片字节）。
	// A：openScanIterator 优先用 xlsxreader 快路径（PoC 实测比 excelize 快 1.51×），
	// 不可用时静默回退到 r.Iterate（excelize），业务语义零差异。
	it, scanCleanup, err := openScanIterator(r, fs.File.Path, fs.File.SheetName, emitter)
	if err != nil {
		return 0, false, err
	}
	defer scanCleanup()

	type stagedRow struct {
		rowNum int
		kw     string
		values []any
		height float64
	}
	var matchedRows []stagedRow

	tScan := time.Now()
	lastRowNum := 0
	for it.Next() {
		if it.RowNum() <= task.HeaderRow { // 跳过表头
			continue
		}
		if it.RowNum()%progressEvery == 0 {
			if err := pipeline.CheckCancel(ctx); err != nil {
				return len(matchedRows), false, err
			}
			emitter.Progress(core.Progress{
				Stage:   "scanning",
				Message: fmt.Sprintf("扫描行 %d (%s)", it.RowNum(), fs.File.SheetName),
			})
		}
		lastRowNum = it.RowNum()
		cells, err := it.Columns()
		if err != nil {
			return len(matchedRows), false, err
		}
		// 公式回退兜底（方案 A+）：对无 <v> 缓存的公式 cell 用预求值结果填充，
		// 让搜索能命中公式计算结果。formulaValues 为空（真实业务文件场景）时
		// Fill 内部直接 return cells 不变，零开销。Fill 在 xlsxreader 跳过无 v
		// 公式 cell 导致 cells 数组缩短的情况下会自动扩展数组——必须用返回值。
		if len(formulaValues) > 0 {
			cells = excelio.FillRowCellsWithFormulaValues(cells, it.RowNum(), formulaValues)
		}
		var kw string
		if eng.HasKeywords() {
			kw = eng.MatchRow(cells, fileSearchCols)
			if kw == "" {
				continue
			}
		}
		// 高级筛选：关键词命中后立即应用，未通过的行不会触发下游公式查询/图片加载/EmitRow。
		// flt 为 nil 或 IsZero 时 Apply 内部短路返回 true。
		// 当无关键词时，filter 是唯一规则源；否则筛选是关键词命中的二次过滤（AND）。
		if !flt.Apply(cells) {
			continue
		}

		// V1.2：仅在 sheet 确实含公式时才查 cell 公式。fixture 01（无公式 sheet）
		// 命中 14286 行时跳过 20 万次 excelize.CellFormula 调用，省 10-40 秒。
		// fixture 02（含公式 sheet）保持原行为，公式零回归。
		var formulas []string
		if hasFormulas {
			formulas = readRowFormulas(r, fs.File.SheetName, it.RowNum(), len(cells))
		}
		values := fs.AlignRowWithFormulas(cells, formulas, len(schema.Columns))

		// B3：heightMap 命中即 O(1) 拿到自定义高度；预读失败或无该行 → 默认 height=0。
		// map 里只有显式设过 ht 的行（排除了 15.0 和 0），和 r.RowHeight 语义一致。
		// heightMap=nil（预读失败）时降级到 r.RowHeight，行为与 B3 前完全等价。
		var height float64
		if heightMap != nil {
			if h, ok := heightMap[it.RowNum()]; ok {
				height = h
			}
		} else {
			height, _, _ = r.RowHeight(fs.File.SheetName, it.RowNum())
		}

		matchedRows = append(matchedRows, stagedRow{
			rowNum: it.RowNum(),
			kw:     kw,
			values: values,
			height: height,
		})
	}
	if err := it.Err(); err != nil {
		return len(matchedRows), false, err
	}
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 扫描匹配 %v: %d 行 -> 命中 %d 行",
		time.Since(tScan).Round(time.Millisecond), lastRowNum, len(matchedRows)))
	pipeline.LogMem(emitter, "after scan: "+fs.File.SheetName)

	// 阶段 3：按需从 zip 直读命中行的图片字节。新路径完全绕过 excelize.GetPictures，
	// 预计把 O(N²) 的 anchor 扫描 + 重复 image.DecodeConfig 压缩到 O(命中行)。
	var picsByRow map[int][]excelio.CellPictures
	if task.PreserveImages && zipSrc != nil && rowToRefs != nil && len(matchedRows) > 0 {
		rows := make([]int, 0, len(matchedRows))
		for _, m := range matchedRows {
			if _, ok := rowToRefs[m.rowNum]; ok {
				rows = append(rows, m.rowNum)
			}
		}
		if len(rows) > 0 {
			tLoad := time.Now()
			picProgress := func(done, total int) {
				emitter.Progress(core.Progress{
					Stage: "loading-images",
					Done:  int64(done), Total: int64(total),
					Message: fmt.Sprintf("加载命中行图片 %d / %d", done, total),
				})
			}
			picsByRow, err = zipSrc.LoadPicturesForRowsZip(fs.File.SheetName, rows, heightMap, picProgress)
			if err != nil {
				emitter.Log(core.LogWarn, "zip 加载命中行图片失败，降级为不带图: "+err.Error())
				picsByRow = nil
			} else {
				emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 加载命中行图片(zip) %v: %d 行",
					time.Since(tLoad).Round(time.Millisecond), len(rows)))
			}
		}
	}

	// 阶段 4：按命中顺序 EmitRow 到 writer。
	tEmit := time.Now()
	for _, m := range matchedRows {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return len(matchedRows), false, err
		}
		var pics []excelio.CellPictures
		if picsByRow != nil {
			pics = picsByRow[m.rowNum]
		}
		if err := ow.EmitRow(MatchedRow{
			SourceFile: fs.File.Path,
			SourceRow:  m.rowNum,
			MatchedKW:  m.kw,
			Values:     m.values,
			Pictures:   pics,
			RowHeight:  m.height,
		}, fs); err != nil {
			return len(matchedRows), false, err
		}
	}
	if len(matchedRows) > 0 {
		emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 写入命中行 %v: %d 行",
			time.Since(tEmit).Round(time.Millisecond), len(matchedRows)))
	}
	return len(matchedRows), false, nil
}

func validateTask(t core.ExtractTask) error {
	if t.FolderPath == "" {
		return core.New("INVALID_TASK", "FolderPath 不能为空")
	}
	return validateTaskRelaxed(t)
}

// validateTaskRelaxed 允许 FolderPath 为空，用于 ExtractUnits（文件已由调用方提供）。
func validateTaskRelaxed(t core.ExtractTask) error {
	// inplace 模式结果写回源文件，不需要 OutputDir
	if t.OutputTarget != core.OutputTargetInplaceSheets && t.OutputDir == "" {
		return core.New("INVALID_TASK", "OutputDir 不能为空")
	}
	if len(t.Keywords) == 0 && t.AdvancedFilter.IsEmpty() {
		return core.New("INVALID_TASK", "至少需要一个关键词或一条高级筛选条件")
	}
	switch t.Output {
	case core.OutputPerKeyword, core.OutputMerged, core.OutputPerSource:
		// ok
	default:
		return core.New("INVALID_TASK", "未知 Output 策略: "+string(t.Output))
	}
	return nil
}

// isAllXlsxSources 判断 files 是否全部是 xlsx 源（不含任何 CSV / TSV / 其他格式）。
// per_keyword surgery writer 的使用前提：所有源必须是 zip 兼容的 xlsx。
// CSV 不是 zip，不能做手术 → 走原 perKeywordWriter 流式路径。
func isAllXlsxSources(units []FileInfo) bool {
	if len(units) == 0 {
		return false
	}
	for _, u := range units {
		if core.DetectSourceKind(u.Path) != core.SourceXLSX {
			return false
		}
	}
	return true
}

// countDistinctPaths 统计 FileInfo 列表中不重复的 Path 数。
func countDistinctPaths(units []FileInfo) int {
	seen := map[string]struct{}{}
	for _, u := range units {
		seen[u.Path] = struct{}{}
	}
	return len(seen)
}

// distinctFilePaths 返回 FileInfo 列表中去重后的 Path 列表，保持首次出现顺序。
// 给 SizeBanner 等"按文件而非按 Sheet"统计的场景用。
func distinctFilePaths(units []FileInfo) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(units))
	for _, u := range units {
		if _, ok := seen[u.Path]; ok {
			continue
		}
		seen[u.Path] = struct{}{}
		out = append(out, u.Path)
	}
	return out
}

// readRowFormulas 查询某一行所有 cell 的公式，返回与 cells 平行的切片。
// 非公式 cell 对应位置为空字符串。任何 cell 查询失败静默置空（不影响主流程）。
func readRowFormulas(r *excelio.Reader, sheet string, row, ncells int) []string {
	out := make([]string, ncells)
	for i := 0; i < ncells; i++ {
		cellName, err := excelio.CellName(i+1, row)
		if err != nil {
			continue
		}
		f, err := r.CellFormula(sheet, cellName)
		if err != nil {
			continue
		}
		out[i] = f
	}
	return out
}

// newOutputWriter 按策略构造 writer，并返回一个用于查询图片迁移数量的回调。
//
// headerRow / sheets 仅 per_source（原汁原味路径）需要：
//   - headerRow 用于保留表头行；<=0 表示不强制保留。
//   - sheets 是用户在前端选中的 Sheet 名列表；原汁原味会只保留这些 Sheet 里有命中的。
//
// per_keyword / merged 是"多源合并"本质，继续用流式重写的 defaultSheet="结果"。
//
// dedupCfg（V1.1+ / V1.2+）：去重配置；Columns 为空 = 不去重。各 writer 内部按自身策略决定分桶：
//   - merged     -> 全局唯一桶
//   - per_keyword -> 按关键词分桶
//   - per_source -> 按源文件分桶
func newOutputWriter(
	strategy core.OutputStrategy, outDir string, headerRow int, sheets []string, filenamePrefix string, dedupCfg dedupConfig,
	allXlsxSources bool,
) (OutputWriter, func() int, error) {
	const defaultSheet = "结果"
	switch strategy {
	case core.OutputPerKeyword:
		// 所有源都是 xlsx 走 zip 手术：
		//   - 单源 → ExtractToNewFileSurgery（100% 字节级复刻）
		//   - 多源 → CloneAndMergePreserved（同模板克隆 ~99% 复刻）
		// 含 CSV 源时保留老流式路径（CSV 不是 zip）。
		if allXlsxSources {
			w := newPerKeywordSurgeryWriter(outDir, filenamePrefix, headerRow, dedupCfg)
			return w, w.ImagesMigrated, nil
		}
		w := newPerKeywordWriter(outDir, defaultSheet, filenamePrefix, dedupCfg)
		return w, w.ImagesMigrated, nil
	case core.OutputMerged:
		w := newMergedWriter(outDir, defaultSheet, filenamePrefix, dedupCfg)
		return w, w.ImagesMigrated, nil
	case core.OutputPerSource:
		w := newPerSourceWriter(outDir, headerRow, sheets, filenamePrefix, dedupCfg)
		return w, w.ImagesMigrated, nil
	default:
		return nil, nil, core.New("INVALID_STRATEGY", "未知输出策略")
	}
}
