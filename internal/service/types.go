// Package service 是应用服务层。
// 它的角色是：
//  1. 接收来自前端（Wails）的 DTO 请求，翻译成 internal/core 领域任务；
//  2. 调用 extractor / splitter 执行；
//  3. 通过 EventEmitter（WailsEmitter）把进度/日志/完成/错误事件推给前端；
//  4. 维护任务注册表，支持 Cancel。
//
// 本包是 app.go 桥接的唯一下游，app.go 里每个方法都应该 ≤ 5 行。
package service

// ExtractRequest 是前端请求批量提取时提交的 DTO。
// 字段全部使用 JSON 友好类型，匹配模式用三个独立布尔而非位掩码。
type ExtractRequest struct {
	FolderPath     string   `json:"folderPath"`
	KeywordsRaw    string   `json:"keywordsRaw"` // 用户原始输入，服务端用 matcher.ParseKeywords 切分
	Exact          bool     `json:"exact"`
	Contains       bool     `json:"contains"`
	Pinyin         bool     `json:"pinyin"`
	SearchAllCols  bool     `json:"searchAllCols"`
	SearchColumns  []string `json:"searchColumns"`
	Strategy       string   `json:"strategy"` // per_keyword / merged / per_source
	OutputDir      string   `json:"outputDir"`
	HeaderRow      int      `json:"headerRow"`
	PreserveImages bool     `json:"preserveImages"`
	SheetNames     []string `json:"sheetNames"`     // V1.1 空 = 所有 Sheet 都参与
	FilenamePrefix string   `json:"filenamePrefix"` // 输出文件名前缀；空串 = 默认；例 "搜索_"
}

// SplitRequest 是前端请求单文件拆分时提交的 DTO。
// V1.1：新增 SheetNames（空 = 所有 Sheet）；新增 by_keyword 模式相关字段。
type SplitRequest struct {
	SourcePath     string   `json:"sourcePath"`
	Mode           string   `json:"mode"` // by_sheet / by_rows / by_column / by_keyword
	RowsPerFile    int      `json:"rowsPerFile"`
	SplitColumn    string   `json:"splitColumn"`
	OutputDir      string   `json:"outputDir"`
	HeaderRow      int      `json:"headerRow"`
	PreserveImages bool     `json:"preserveImages"`
	SheetNames     []string `json:"sheetNames"`

	// 仅 mode == by_keyword 用：和 ExtractRequest 字段含义一致。
	KeywordsRaw   string   `json:"keywordsRaw"`
	Exact         bool     `json:"exact"`
	Contains      bool     `json:"contains"`
	Pinyin        bool     `json:"pinyin"`
	SearchAllCols bool     `json:"searchAllCols"`
	SearchColumns []string `json:"searchColumns"`
	Strategy      string   `json:"strategy"`
}

// TaskHandle 是启动任务后返回给前端的句柄。
// 前端收到后通过 EventEmitter 推送的事件里带 TaskID 做绑定。
type TaskHandle struct {
	TaskID string `json:"taskId"`
}

// HeaderPreview 用于"上传文件夹后展示第一个文件的列名"以供用户勾选搜索列。
// V1.1 增加 Sheets 字段（文件夹中所有 Sheet 名的并集）让用户勾选要处理的 Sheet。
type HeaderPreview struct {
	FirstFile string   `json:"firstFile"`
	Columns   []string `json:"columns"`
	Sheets    []string `json:"sheets"`
}

// FilePreview 用于"选完单文件后给用户列 Sheet + 表头"。
// 用于单文件拆分场景（按 Sheet 勾选 / 按列值选列名）。
type FilePreview struct {
	Path    string   `json:"path"`
	Sheets  []string `json:"sheets"`  // 全部 Sheet 名
	Columns []string `json:"columns"` // 第一个 Sheet 的表头（headerRow > 0 时）
}
