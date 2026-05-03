package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/extractor"
	"excel-master/internal/matcher"
)

// PreviewFolder 扫描文件夹：返回第一个 xlsx 的表头 + 文件夹下所有 Sheet 名并集。
// 前端用来给用户"勾选搜索列 + 勾选要处理的 Sheet"。
func (s *Service) PreviewFolder(folder string, headerRow int) (*HeaderPreview, error) {
	if folder == "" {
		return nil, core.New("INVALID_FOLDER", "文件夹路径为空")
	}
	stat, err := os.Stat(folder)
	if err != nil || !stat.IsDir() {
		return nil, core.New("INVALID_FOLDER", "不是有效的文件夹: "+folder)
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, core.Wrap("INVALID_FOLDER", "读取文件夹失败", err)
	}

	preview := &HeaderPreview{}
	allSheets := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "~$") {
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".xlsx") {
			continue
		}
		full := filepath.Join(folder, name)
		r, err := excelio.Open(full)
		if err != nil {
			return nil, err
		}
		sheets := r.SheetNames()
		// 第一个有效文件用来读表头
		if preview.FirstFile == "" && len(sheets) > 0 {
			preview.FirstFile = name
			if headerRow > 0 {
				preview.Columns, _ = r.Header(sheets[0], headerRow)
			}
		}
		for _, sh := range sheets {
			if _, ok := allSheets[sh]; !ok {
				allSheets[sh] = struct{}{}
				preview.Sheets = append(preview.Sheets, sh)
			}
		}
		_ = r.Close()
	}
	if preview.FirstFile == "" {
		return nil, core.New("NO_FILES", "文件夹内没有 .xlsx 文件")
	}
	return preview, nil
}

// PreviewFile 选完单文件后预览：返回所有 Sheet 名 + 第一个 Sheet 的表头。
// 前端在"单文件拆分"页用来：1) 列出 Sheet 让用户勾选；2) 列出列名供按列值/按关键词搜索时勾选。
func (s *Service) PreviewFile(path string, headerRow int) (*FilePreview, error) {
	if path == "" {
		return nil, core.New("INVALID_FILE", "文件路径为空")
	}
	if !strings.EqualFold(filepath.Ext(path), ".xlsx") {
		return nil, core.New("INVALID_FILE", "仅支持 .xlsx 文件: "+path)
	}
	r, err := excelio.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	sheets := r.SheetNames()
	if len(sheets) == 0 {
		return nil, core.New("EMPTY_WORKBOOK", "工作簿没有 Sheet")
	}
	out := &FilePreview{Path: path, Sheets: sheets}
	if headerRow > 0 {
		out.Columns, _ = r.Header(sheets[0], headerRow)
	}
	return out, nil
}

// StartExtract 异步启动一次批量提取。立刻返回 TaskHandle；
// 进度和结果通过 emitter 以事件形式发给前端。
func (s *Service) StartExtract(req ExtractRequest) (*TaskHandle, error) {
	task, err := buildExtractTask(req)
	if err != nil {
		return nil, err
	}
	taskID := s.newTaskID()
	emitter := s.factory(taskID)

	ctx, cancel := context.WithCancel(context.Background())
	s.register(taskID, cancel)

	go func() {
		defer s.unregister(taskID)
		defer cancel()
		result, err := extractor.Extract(ctx, task, emitter)
		if err != nil {
			emitter.Error(err)
			return
		}
		emitter.Done(result)
	}()

	return &TaskHandle{TaskID: taskID}, nil
}

func buildExtractTask(req ExtractRequest) (core.ExtractTask, error) {
	keywords := matcher.ParseKeywords(req.KeywordsRaw)
	if len(keywords) == 0 {
		return core.ExtractTask{}, core.New("INVALID_TASK", "关键词不能为空")
	}
	var mode core.MatchMode
	if req.Exact {
		mode |= core.MatchExact
	}
	if req.Contains {
		mode |= core.MatchContains
	}
	if req.Pinyin {
		mode |= core.MatchPinyin
	}
	if mode == 0 {
		mode = core.MatchContains // 默认包含
	}

	strategy, err := parseStrategy(req.Strategy)
	if err != nil {
		return core.ExtractTask{}, err
	}
	headerRow := req.HeaderRow
	if headerRow == 0 {
		headerRow = 1 // 默认首行表头
	}
	return core.ExtractTask{
		FolderPath:     req.FolderPath,
		Keywords:       keywords,
		MatchMode:      mode,
		SearchAllCols:  req.SearchAllCols || len(req.SearchColumns) == 0,
		SearchColumns:  req.SearchColumns,
		Output:         strategy,
		OutputDir:      req.OutputDir,
		HeaderRow:      headerRow,
		PreserveImages: req.PreserveImages,
		SheetNames:     req.SheetNames,
	}, nil
}

func parseStrategy(s string) (core.OutputStrategy, error) {
	switch core.OutputStrategy(strings.ToLower(strings.TrimSpace(s))) {
	case core.OutputPerKeyword:
		return core.OutputPerKeyword, nil
	case core.OutputMerged:
		return core.OutputMerged, nil
	case core.OutputPerSource:
		return core.OutputPerSource, nil
	case "":
		return core.OutputPerKeyword, nil
	default:
		return "", core.New("INVALID_STRATEGY", "未知输出策略: "+s)
	}
}
