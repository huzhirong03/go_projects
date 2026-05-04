package splitter

import (
	"context"

	"excel-master/internal/core"
	"excel-master/internal/extractor"
	"excel-master/internal/pipeline"
)

// SplitByKeyword：把单个 xlsx 按关键词拆成多个新文件。
// 复用 extractor 的引擎（不重复实现匹配/图片迁移/表头合并），
// 把当前文件 + 选中 Sheet 包装成"虚拟单元列表"投喂给 extractor.ExtractUnits。
//
// 输出策略 Output（per_keyword/merged/per_source）的语义与批量提取一致：
//   - per_keyword：每关键词一个文件
//   - merged：所有命中合并到一个文件
//   - per_source：本质等价 merged（只有一个源），但仍然保留以便在前端选项里复用
//
// SheetNames 为空表示处理全部 Sheet；多 Sheet 自动按列名合并 schema。
func SplitByKeyword(ctx context.Context, task core.SplitTask, emitter core.EventEmitter) (*Result, error) {
	if emitter == nil {
		emitter = pipeline.NoopEmitter{}
	}
	if err := validateCommon(task); err != nil {
		return nil, err
	}
	if len(task.Keywords) == 0 && task.AdvancedFilter.IsEmpty() {
		return nil, core.New("INVALID_TASK", "至少需要一个关键词或一条高级筛选条件")
	}
	if task.Output == "" {
		// 默认每关键词一个文件，最直观。
		task.Output = core.OutputPerKeyword
	}

	// 1. 扫描该文件的 Sheet 单元（带 SheetNames 过滤）
	emitter.Progress(core.Progress{Stage: "scanning", Message: "解析源文件 Sheet"})
	units, err := extractor.ScanFile(task.SourcePath, task.HeaderRow, task.SheetNames)
	if err != nil {
		return nil, err
	}

	// 2. 翻译为 ExtractTask 并复用 extractor.ExtractUnits
	et := core.ExtractTask{
		FolderPath:     "", // ExtractUnits 不要求
		Keywords:       task.Keywords,
		MatchMode:      task.MatchMode,
		SearchAllCols:  task.SearchAllCols,
		SearchColumns:  task.SearchColumns,
		Output:         task.Output,
		OutputDir:      task.OutputDir,
		HeaderRow:      task.HeaderRow,
		PreserveImages: task.PreserveImages,
		SheetNames:     task.SheetNames,
		CSVEncoding:    task.CSVEncoding,
		CSVDelimiter:   task.CSVDelimiter,
		OutputTarget:   task.OutputTarget,
		BackupSource:   task.BackupSource,
		AdvancedFilter: task.AdvancedFilter,
		DedupColumn:    task.DedupColumn, // V1.1+ 透传给 extractor，由 writer 按策略分桶去重
	}
	er, err := extractor.ExtractUnits(ctx, et, units, emitter)
	if err != nil {
		return nil, err
	}

	// 3. 把 extractor 结果翻译回 splitter 的 Result。
	//    PartsCreated 用 OutputFiles 数量；RowsScanned 用 RowsMatched（拆分语义里就是命中行）。
	return &Result{
		SourceFile:     task.SourcePath,
		Mode:           string(core.SplitByKeyword),
		RowsScanned:    er.RowsMatched,
		PartsCreated:   len(er.OutputFiles),
		ImagesMigrated: er.ImagesMigrated,
		OutputFiles:    er.OutputFiles,
	}, nil
}
