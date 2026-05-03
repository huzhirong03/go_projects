package core

import "context"

// MatchMode 匹配模式（可组合使用，按位或）。
type MatchMode uint8

const (
	MatchExact    MatchMode = 1 << iota // 精准匹配：完全相等
	MatchContains                       // 子串包含
	MatchPinyin                         // 拼音匹配（含首字母）
)

// Has 判断当前模式是否包含指定模式。
func (m MatchMode) Has(mode MatchMode) bool { return m&mode != 0 }

// OutputStrategy 批量提取时的输出组织策略。
type OutputStrategy string

const (
	OutputPerKeyword OutputStrategy = "per_keyword" // 每个关键词一个文件
	OutputMerged     OutputStrategy = "merged"      // 合成一个文件，多一列命中关键词
	OutputPerSource  OutputStrategy = "per_source"  // 每个源文件一个新文件
)

// OutputTarget 输出目标：新文件 vs 写回源文件新 Sheet。
//   - OutputTargetNewFiles: 默认行为，输出到 OutputDir（所有现有语义不变）
//   - OutputTargetInplaceSheets: 把结果以"新 Sheet"形式追加到源文件里（仅单文件模式有效）
//     inplace 下 OutputDir 被忽略。
type OutputTarget string

const (
	OutputTargetNewFiles      OutputTarget = "new_files"
	OutputTargetInplaceSheets OutputTarget = "inplace_sheets"
)

// SplitMode 单文件拆分模式。
type SplitMode string

const (
	SplitBySheet   SplitMode = "by_sheet"   // 每个 Sheet 一个文件
	SplitByRows    SplitMode = "by_rows"    // 每 N 行一个文件
	SplitByColumn  SplitMode = "by_column"  // 按列值拆分
	SplitByKeyword SplitMode = "by_keyword" // 按关键词拆分（复用 extractor 引擎）
)

// ExtractTask 文件夹批量提取任务。
//
// SheetNames 为空表示"处理每个文件的全部 Sheet"（V1.1 默认行为）；
// 非空表示"只处理这些名称的 Sheet"，未匹配到的 Sheet 静默跳过。
// 注：跨文件 Sheet 名可能不同，常见做法是取多文件 Sheet 名的并集让用户勾选。
type ExtractTask struct {
	FolderPath     string         // 源文件夹绝对路径
	Keywords       []string       // 关键词列表（已解析）
	MatchMode      MatchMode      // 匹配模式（可组合）
	SearchAllCols  bool           // true=全列扫描；false=只扫 SearchColumns
	SearchColumns  []string       // 指定的搜索列（按列名匹配表头）
	Output         OutputStrategy // 输出策略
	OutputDir      string         // 输出目录绝对路径
	HeaderRow      int            // 表头所在行号，1-based；0 表示无表头
	PreserveImages bool           // 是否保留图片
	SheetNames     []string       // 指定的 Sheet 名（空 = 全部）
	FilenamePrefix string         // 输出文件名前缀，空字符串 = 默认；常用 "搜索_"

	// CSV 源专用（仅源含有 .csv 时生效，xlsx 源忽略）
	CSVEncoding  string // "" = 自动嗅探；允许 "utf-8" / "gbk" / "gb18030" / "big5" / "utf-16" 等
	CSVDelimiter string // "" = 自动推断；允许 "," / ";" / "\t" / "|"

	// inplace 输出相关（仅单文件模式 + xlsx 源生效；CSV / 文件夹会在服务层拦截）
	OutputTarget OutputTarget // "" 视作 OutputTargetNewFiles
	BackupSource bool         // 为 true 且 OutputTarget=inplace 时，先生成 .bak 再写回
}

// SplitTask 单文件拆分任务。
//
// SheetNames 为空表示"处理全部 Sheet"。
// SplitByKeyword 模式额外使用 Keywords / MatchMode / SearchAllCols / SearchColumns / Output。
type SplitTask struct {
	SourcePath     string    // 源文件绝对路径
	Mode           SplitMode // 拆分方式
	RowsPerFile    int       // SplitByRows 用：每份行数
	SplitColumn    string    // SplitByColumn 用：依据列名
	OutputDir      string    // 输出目录
	HeaderRow      int       // 表头行号
	PreserveImages bool
	SheetNames     []string // 参与拆分的 Sheet 名（空 = 全部）

	// 仅 SplitByKeyword 用
	Keywords      []string
	MatchMode     MatchMode
	SearchAllCols bool
	SearchColumns []string
	Output        OutputStrategy

	// CSV 源专用（仅 SplitByKeyword + .csv 生效）
	CSVEncoding  string
	CSVDelimiter string

	// inplace 输出相关（仅 xlsx 源生效；by_sheet 模式不适用，服务层拦截）
	OutputTarget OutputTarget
	BackupSource bool
}

// Progress 任务进度快照。Done == Total 表示完成。
type Progress struct {
	Stage   string // scanning / reading / writing / finalizing
	Done    int64
	Total   int64
	Message string
}

type FileBlockedRequest struct {
	PromptID string
	Path     string
	Message  string
}

type FileBlockedChoice string

const (
	FileBlockedRetry  FileBlockedChoice = "retry"
	FileBlockedSkip   FileBlockedChoice = "skip"
	FileBlockedCancel FileBlockedChoice = "cancel"
)

type FileBlockedPrompter interface {
	PromptFileBlocked(ctx context.Context, req FileBlockedRequest) FileBlockedChoice
}

// Runner 是所有耗时任务的统一抽象。
// 实现必须响应 ctx 取消并向 emitter 汇报进度与日志。
type Runner interface {
	Run(ctx context.Context, emitter EventEmitter) error
}

// EventEmitter 向前端（或日志）广播事件。
type EventEmitter interface {
	Progress(p Progress)
	Log(level, msg string)
	Done(result any)
	Error(err error)
}
