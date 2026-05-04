// Package service 是应用服务层。
// 它的角色是：
//  1. 接收来自前端（Wails）的 DTO 请求，翻译成 internal/core 领域任务；
//  2. 调用 extractor / splitter 执行；
//  3. 通过 EventEmitter（WailsEmitter）把进度/日志/完成/错误事件推给前端；
//  4. 维护任务注册表，支持 Cancel。
//
// 本包是 app.go 桥接的唯一下游，app.go 里每个方法都应该 ≤ 5 行。
package service

import "excel-master/internal/core"

// ExtractRequest 是前端请求批量提取时提交的 DTO。
// 字段全部使用 JSON 友好类型，匹配模式用两个独立布尔而非位掩码。
type ExtractRequest struct {
	FolderPath     string   `json:"folderPath"`
	KeywordsRaw    string   `json:"keywordsRaw"` // 用户原始输入，服务端用 matcher.ParseKeywords 切分
	Exact          bool     `json:"exact"`
	Contains       bool     `json:"contains"`
	SearchAllCols  bool     `json:"searchAllCols"`
	SearchColumns  []string `json:"searchColumns"`
	Strategy       string   `json:"strategy"` // per_keyword / merged / per_source
	OutputDir      string   `json:"outputDir"`
	HeaderRow      int      `json:"headerRow"`
	PreserveImages bool     `json:"preserveImages"`
	SheetNames     []string `json:"sheetNames"`     // V1.1 空 = 所有 Sheet 都参与
	FilenamePrefix string   `json:"filenamePrefix"` // 输出文件名前缀；空串 = 默认；例 "搜索_"

	// CSV 源可选参数；空字符串 = 自动推断。xlsx 源忽略。
	CSVEncoding  string `json:"csvEncoding"`
	CSVDelimiter string `json:"csvDelimiter"`

	// 输出目标（仅单文件 + xlsx 源生效，其它场景自动回退为 new_files）
	// "" / "new_files" = 默认输出新文件
	// "inplace_sheets" = 写回源文件新 Sheet
	OutputTarget string `json:"outputTarget"`
	BackupSource bool   `json:"backupSource"` // inplace 前是否生成 .bak

	// 高级筛选（V1.5+）：在关键词命中之后再按列条件二次过滤。
	// AdvancedFilter == nil 或 Conditions 为空时等同于不启用，行为完全跟旧版一致。
	AdvancedFilter *AdvancedFilterDTO `json:"advancedFilter,omitempty"`

	// 去重列（V1.1+）：按该列去重，保留首次出现的行。空串 = 不去重（默认）。
	// 前端以 checkbox 控制：未勾时提交空串，勾了但没选列也会被前端检测抦截。
	// 去重范围由 Strategy 自动推导（见 core.ExtractTask.DedupColumn 的说明）。
	DedupColumn string `json:"dedupColumn"`
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
	SearchAllCols bool     `json:"searchAllCols"`
	SearchColumns []string `json:"searchColumns"`
	Strategy      string   `json:"strategy"`

	// CSV 源可选参数（仅 by_keyword + .csv 生效）
	CSVEncoding  string `json:"csvEncoding"`
	CSVDelimiter string `json:"csvDelimiter"`

	// 输出目标（xlsx 源生效；by_sheet / CSV 源自动回退为 new_files）
	OutputTarget string `json:"outputTarget"`
	BackupSource bool   `json:"backupSource"`

	// 高级筛选（V1.5+）：仅 by_keyword 模式生效；其他三种拆分模式服务端忽略。
	AdvancedFilter *AdvancedFilterDTO `json:"advancedFilter,omitempty"`

	// 去重列（V1.1+）：仅 by_keyword 模式生效；其他三种拆分模式服务端强制清空。
	// 语义跟 ExtractRequest.DedupColumn 一致。
	DedupColumn string `json:"dedupColumn"`
}

// AdvancedFilterDTO 是前端 → 后端的高级筛选 DTO。
// 字段命名严格跟前端 camelCase / 后端 internal/filter.Spec 对齐。
type AdvancedFilterDTO struct {
	Mode       string                 `json:"mode"`       // "all" / "any"
	Conditions []AdvancedConditionDTO `json:"conditions"` // 条件列表
}

// AdvancedConditionDTO 单条件 DTO。
type AdvancedConditionDTO struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  string `json:"value"`
	Value2 string `json:"value2,omitempty"`
	Format string `json:"format,omitempty"`
}

// toCoreAdvancedFilter 把前端 DTO 转成 core 域 spec；nil/空 → nil（视作不启用）。
func toCoreAdvancedFilter(dto *AdvancedFilterDTO) *core.AdvancedFilterSpec {
	if dto == nil || len(dto.Conditions) == 0 {
		return nil
	}
	conds := make([]core.AdvancedFilterCondition, 0, len(dto.Conditions))
	for _, c := range dto.Conditions {
		// 跳过完全空的占位条件
		if c.Column == "" || c.Op == "" {
			continue
		}
		conds = append(conds, core.AdvancedFilterCondition{
			Column: c.Column,
			Op:     c.Op,
			Value:  c.Value,
			Value2: c.Value2,
			Format: c.Format,
		})
	}
	if len(conds) == 0 {
		return nil
	}
	return &core.AdvancedFilterSpec{Mode: dto.Mode, Conditions: conds}
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
