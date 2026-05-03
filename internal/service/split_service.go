package service

import (
	"context"
	"strings"

	"excel-master/internal/core"
	"excel-master/internal/matcher"
	"excel-master/internal/splitter"
)

// StartSplit 异步启动一次单文件拆分，立刻返回句柄。
func (s *Service) StartSplit(req SplitRequest) (*TaskHandle, error) {
	task, err := buildSplitTask(req)
	if err != nil {
		return nil, err
	}
	taskID := s.newTaskID()
	emitter := s.factory(taskID, s.broker)

	ctx, cancel := context.WithCancel(context.Background())
	s.register(taskID, cancel)

	go func() {
		defer s.unregister(taskID)
		defer cancel()
		result, err := splitter.Split(ctx, task, emitter)
		if err != nil {
			emitter.Error(err)
			return
		}
		emitter.Done(result)
	}()

	return &TaskHandle{TaskID: taskID}, nil
}

func buildSplitTask(req SplitRequest) (core.SplitTask, error) {
	mode := core.SplitMode(strings.ToLower(strings.TrimSpace(req.Mode)))
	switch mode {
	case core.SplitBySheet, core.SplitByRows, core.SplitByColumn, core.SplitByKeyword:
		// ok
	default:
		return core.SplitTask{}, core.New("INVALID_MODE", "未知拆分模式: "+req.Mode)
	}
	if req.SourcePath == "" {
		return core.SplitTask{}, core.New("INVALID_TASK", "SourcePath 不能为空")
	}
	outputTarget := parseOutputTarget(req.OutputTarget)
	// inplace 时 OutputDir 可为空（结果写回源文件，不需要输出目录）
	if outputTarget != core.OutputTargetInplaceSheets && req.OutputDir == "" {
		return core.SplitTask{}, core.New("INVALID_TASK", "OutputDir 不能为空")
	}
	headerRow := req.HeaderRow
	if headerRow == 0 && mode != core.SplitBySheet {
		headerRow = 1
	}

	task := core.SplitTask{
		SourcePath:     req.SourcePath,
		Mode:           mode,
		RowsPerFile:    req.RowsPerFile,
		SplitColumn:    req.SplitColumn,
		OutputDir:      req.OutputDir,
		HeaderRow:      headerRow,
		PreserveImages: req.PreserveImages,
		SheetNames:     req.SheetNames,
		OutputTarget:   outputTarget,
		BackupSource:   req.BackupSource,
	}

	// SplitByKeyword 需要解析关键词、匹配模式、输出策略。
	if mode == core.SplitByKeyword {
		keywords := matcher.ParseKeywords(req.KeywordsRaw)
		if len(keywords) == 0 {
			return core.SplitTask{}, core.New("INVALID_TASK", "按关键词拆分需要至少 1 个关键词")
		}
		var mm core.MatchMode
		if req.Exact {
			mm |= core.MatchExact
		}
		if req.Contains {
			mm |= core.MatchContains
		}
		if req.Pinyin {
			mm |= core.MatchPinyin
		}
		if mm == 0 {
			mm = core.MatchContains // 默认包含
		}
		strategy, err := parseStrategy(req.Strategy)
		if err != nil {
			return core.SplitTask{}, err
		}
		task.Keywords = keywords
		task.MatchMode = mm
		task.SearchAllCols = req.SearchAllCols || len(req.SearchColumns) == 0
		task.SearchColumns = req.SearchColumns
		task.Output = strategy
		task.CSVEncoding = req.CSVEncoding
		task.CSVDelimiter = req.CSVDelimiter
		// 高级筛选仅在 by_keyword 模式生效；其他三模式即使前端传了也忽略。
		task.AdvancedFilter = toCoreAdvancedFilter(req.AdvancedFilter)
	}

	return task, nil
}
