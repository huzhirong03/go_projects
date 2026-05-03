package core

// 事件名常量。前端 frontend/src/types/events.js 必须镜像这些常量，
// 禁止在任何地方硬编码字符串字面量。
const (
	EventProgress    = "task:progress"
	EventLog         = "task:log"
	EventDone        = "task:done"
	EventError       = "task:error"
	EventFileBlocked = "task:file-blocked"
)

// 日志级别常量。
const (
	LogInfo  = "info"
	LogWarn  = "warn"
	LogError = "error"
)
