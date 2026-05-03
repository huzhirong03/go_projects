package extractor

import (
	"context"
	"fmt"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/matcher"
	"excel-master/internal/pipeline"
)

// 每处理 N 行检查一次 context 取消与发射进度事件。
const progressEvery = 500

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
	emitter.Progress(core.Progress{Stage: "scanning", Message: "扫描文件夹"})
	files, err := ScanFolder(task.FolderPath, task.HeaderRow, task.SheetNames)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, core.New("NO_FILES", "文件夹内没有可处理的 Sheet（检查 .xlsx 文件 / Sheet 选择）")
	}
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

	// 2. 统一 schema
	schema, err := BuildSchema(files, task.HeaderRow)
	if err != nil {
		return nil, err
	}

	// 3. 匹配引擎
	eng := matcher.NewEngine(task.Keywords, task.MatchMode)
	if len(eng.Keywords()) == 0 {
		return nil, core.New("NO_KEYWORDS", "至少需要一个关键词")
	}

	// 4. 指定搜索列翻译成统一列索引（nil 表示全列）
	var unifiedSearchCols []int
	if !task.SearchAllCols && len(task.SearchColumns) > 0 {
		unifiedSearchCols = schema.ResolveSearchColumns(task.SearchColumns)
	}

	// 5. 选输出策略
	ow, imgCounterFn, err := newOutputWriter(task.Output, task.OutputDir)
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
	for fi, fs := range schema.Files {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return nil, err
		}
		emitter.Progress(core.Progress{
			Stage:   "reading",
			Done:    int64(fi),
			Total:   total,
			Message: fs.File.Path + " [" + fs.File.SheetName + "]",
		})

		matched, err := processFile(ctx, &fs, schema, eng, unifiedSearchCols, task, ow, emitter)
		if err != nil {
			return nil, err
		}
		if matched > 0 {
			matchedPaths[fs.File.Path] = true
			result.RowsMatched += matched
		}
	}
	result.FilesMatched = len(matchedPaths)

	// 7. 落盘
	emitter.Progress(core.Progress{Stage: "finalizing", Done: total, Total: total, Message: "写入输出文件"})
	paths, err := ow.Finalize()
	if err != nil {
		return nil, err
	}
	result.OutputFiles = paths
	result.ImagesMigrated = imgCounterFn()

	emitter.Log(core.LogInfo, fmt.Sprintf("完成：命中 %d 行，迁移图片 %d 张，输出 %d 个文件",
		result.RowsMatched, result.ImagesMigrated, len(result.OutputFiles)))
	return result, nil
}

// processFile 处理单个源文件：打开 → 构图片索引 → 流式行迭代 → 命中派发。
func processFile(
	ctx context.Context,
	fs *FileSchema,
	schema *UnifiedSchema,
	eng *matcher.Engine,
	unifiedSearchCols []int,
	task core.ExtractTask,
	ow OutputWriter,
	emitter core.EventEmitter,
) (int, error) {
	r, err := excelio.Open(fs.File.Path)
	if err != nil {
		return 0, err
	}
	defer r.Close()

	var picIdx *excelio.PictureIndex
	if task.PreserveImages {
		picIdx, err = excelio.BuildPictureIndex(r.File(), fs.File.SheetName)
		if err != nil {
			// 图片索引失败不中断，降级为不带图。
			emitter.Log(core.LogWarn, "构建图片索引失败，跳过图片: "+err.Error())
			picIdx = nil
		}
	}

	fileSearchCols := fs.FileSearchColumns(unifiedSearchCols)

	it, err := r.Iterate(fs.File.SheetName)
	if err != nil {
		return 0, err
	}
	defer it.Close()

	matched := 0
	for it.Next() {
		if it.RowNum() <= task.HeaderRow { // 跳过表头
			continue
		}
		if it.RowNum()%progressEvery == 0 {
			if err := pipeline.CheckCancel(ctx); err != nil {
				return matched, err
			}
		}
		cells, err := it.Columns()
		if err != nil {
			return matched, err
		}
		kw := eng.MatchRow(cells, fileSearchCols)
		if kw == "" {
			continue
		}

		// V1.1：仅对命中行做公式查询（昂贵但只在命中时触发，性能影响有限）。
		formulas := readRowFormulas(r, fs.File.SheetName, it.RowNum(), len(cells))
		values := fs.AlignRowWithFormulas(cells, formulas, len(schema.Columns))

		// 复刻源行高（默认值视为未设置，让目标用默认）。
		height, _, _ := r.RowHeight(fs.File.SheetName, it.RowNum())

		var pics []excelio.CellPictures
		if picIdx != nil {
			pics = picIdx.PicturesOnRow(it.RowNum())
		}
		if err := ow.EmitRow(MatchedRow{
			SourceFile: fs.File.Path,
			SourceRow:  it.RowNum(),
			MatchedKW:  kw,
			Values:     values,
			Pictures:   pics,
			RowHeight:  height,
		}, fs); err != nil {
			return matched, err
		}
		matched++
	}
	if err := it.Err(); err != nil {
		return matched, err
	}
	return matched, nil
}

func validateTask(t core.ExtractTask) error {
	if t.FolderPath == "" {
		return core.New("INVALID_TASK", "FolderPath 不能为空")
	}
	return validateTaskRelaxed(t)
}

// validateTaskRelaxed 允许 FolderPath 为空，用于 ExtractUnits（文件已由调用方提供）。
func validateTaskRelaxed(t core.ExtractTask) error {
	if t.OutputDir == "" {
		return core.New("INVALID_TASK", "OutputDir 不能为空")
	}
	if len(t.Keywords) == 0 {
		return core.New("INVALID_TASK", "Keywords 不能为空")
	}
	switch t.Output {
	case core.OutputPerKeyword, core.OutputMerged, core.OutputPerSource:
		// ok
	default:
		return core.New("INVALID_TASK", "未知 Output 策略: "+string(t.Output))
	}
	return nil
}

// countDistinctPaths 统计 FileInfo 列表中不重复的 Path 数。
func countDistinctPaths(units []FileInfo) int {
	seen := map[string]struct{}{}
	for _, u := range units {
		seen[u.Path] = struct{}{}
	}
	return len(seen)
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
func newOutputWriter(strategy core.OutputStrategy, outDir string) (OutputWriter, func() int, error) {
	const defaultSheet = "结果"
	switch strategy {
	case core.OutputPerKeyword:
		w := newPerKeywordWriter(outDir, defaultSheet)
		return w, w.ImagesMigrated, nil
	case core.OutputMerged:
		w := newMergedWriter(outDir, defaultSheet)
		return w, w.ImagesMigrated, nil
	case core.OutputPerSource:
		w := newPerSourceWriter(outDir, defaultSheet)
		return w, w.ImagesMigrated, nil
	default:
		return nil, nil, core.New("INVALID_STRATEGY", "未知输出策略")
	}
}
