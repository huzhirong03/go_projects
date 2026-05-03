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

// PreviewFolder 扫描"源数据"：可以是文件夹（批量）或单个 .xlsx 文件。
//   - 文件夹：返回第一个 xlsx 的表头 + 所有 Sheet 名并集。
//   - 单文件：把它当成"只有 1 个文件的文件夹"等价处理（FirstFile=文件名，
//     Sheets=该文件全部 Sheet，Columns=第 1 个 Sheet 的表头）。
//
// 前端用来给用户"勾选搜索列 + 勾选要处理的 Sheet"。
func (s *Service) PreviewFolder(folder string, headerRow int) (*HeaderPreview, error) {
	if folder == "" {
		return nil, core.New("INVALID_FOLDER", "源路径为空")
	}
	stat, err := os.Stat(folder)
	if err != nil {
		return nil, core.Wrap("INVALID_FOLDER", "无法访问源路径: "+folder, err)
	}
	// 单文件分支：直接读 1 个文件的预览，组装成 HeaderPreview 返回。
	if !stat.IsDir() {
		if !core.IsSupported(folder) {
			return nil, core.New(core.CodeSourceFormatUnsupported, "仅支持 .xlsx / .xlsm / .csv 文件: "+folder)
		}
		fp, err := s.PreviewFile(folder, headerRow)
		if err != nil {
			return nil, err
		}
		return &HeaderPreview{
			FirstFile: filepath.Base(fp.Path),
			Columns:   fp.Columns,
			Sheets:    fp.Sheets,
		}, nil
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
		if !core.IsSupported(name) {
			continue
		}
		full := filepath.Join(folder, name)
		// CSV 没有 Sheet 概念：用虚拟 Sheet "CSV"，表头取首行。
		if core.DetectSourceKind(full) == core.SourceCSV {
			if preview.FirstFile == "" {
				units, err := extractor.ScanFile(full, headerRow, nil)
				if err != nil {
					return nil, err
				}
				preview.FirstFile = name
				if len(units) > 0 {
					preview.Columns = units[0].Headers
				}
			}
			if _, ok := allSheets["CSV"]; !ok {
				allSheets["CSV"] = struct{}{}
				preview.Sheets = append(preview.Sheets, "CSV")
			}
			continue
		}
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
		return nil, core.New("NO_FILES", "文件夹内没有 .xlsx / .csv 文件")
	}
	return preview, nil
}

// PreviewFile 选完单文件后预览：返回所有 Sheet 名 + 第一个 Sheet 的表头。
// 前端在"单文件拆分"页用来：1) 列出 Sheet 让用户勾选；2) 列出列名供按列值/按关键词搜索时勾选。
func (s *Service) PreviewFile(path string, headerRow int) (*FilePreview, error) {
	if path == "" {
		return nil, core.New("INVALID_FILE", "文件路径为空")
	}
	if !core.IsSupported(path) {
		return nil, core.New(core.CodeSourceFormatUnsupported, "仅支持 .xlsx / .xlsm / .csv 文件: "+path)
	}
	// CSV 没有真正的 "Sheet" 概念；用虚拟 sheet "CSV"，表头来自首行。
	if core.DetectSourceKind(path) == core.SourceCSV {
		units, err := extractor.ScanFile(path, headerRow, nil)
		if err != nil {
			return nil, err
		}
		out := &FilePreview{Path: path, Sheets: []string{"CSV"}}
		if len(units) > 0 {
			out.Columns = units[0].Headers
		}
		return out, nil
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
	emitter := s.factory(taskID, s.broker)

	ctx, cancel := context.WithCancel(context.Background())
	s.register(taskID, cancel)

	go func() {
		// defer 顺序（后注册先执行）：
		//   1) recoverToEmitter 最先执行 → 把 panic 转成 emitter.Error
		//   2) tl.Close 关日志（写入 ended-at 元信息）
		//   3) cancel 取消 ctx
		//   4) unregister 从注册表删除
		defer s.unregister(taskID)
		defer cancel()
		// 任务日志：每次任务一个独立 .log 文件，落盘后即使关软件也能事后回看
		tl, _ := OpenTaskLog(taskID, "extract")
		defer tl.Close()
		taskEmitter := wrapEmitterWithTaskLog(emitter, tl)
		defer recoverToEmitter(taskEmitter)
		result, err := extractor.Extract(ctx, task, taskEmitter)
		if err != nil {
			taskEmitter.Error(err)
			return
		}
		taskEmitter.Done(result)
	}()

	return &TaskHandle{TaskID: taskID}, nil
}

func buildExtractTask(req ExtractRequest) (core.ExtractTask, error) {
	keywords := matcher.ParseKeywords(req.KeywordsRaw)
	advFilter := toCoreAdvancedFilter(req.AdvancedFilter)
	// 关键词或高级筛选至少有一个；两者都空 → 拒绝
	if len(keywords) == 0 && advFilter.IsEmpty() {
		return core.ExtractTask{}, core.New("INVALID_TASK", "至少需要一个关键词或一条高级筛选条件")
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
		FilenamePrefix: req.FilenamePrefix,
		CSVEncoding:    req.CSVEncoding,
		CSVDelimiter:   req.CSVDelimiter,
		OutputTarget:   parseOutputTarget(req.OutputTarget),
		BackupSource:   req.BackupSource,
		AdvancedFilter: advFilter,
	}, nil
}

// parseOutputTarget 把前端字符串翻译成 core.OutputTarget，未知值一律回退为 new_files。
func parseOutputTarget(s string) core.OutputTarget {
	switch core.OutputTarget(s) {
	case core.OutputTargetInplaceSheets:
		return core.OutputTargetInplaceSheets
	default:
		return core.OutputTargetNewFiles
	}
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
