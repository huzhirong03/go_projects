// 事件名常量必须与 Go internal/core/events.go 一一对应。
// 改动这里之前请先检查后端。

export const EVENT_PROGRESS = 'task:progress'
export const EVENT_LOG = 'task:log'
export const EVENT_DONE = 'task:done'
export const EVENT_ERROR = 'task:error'

export const LOG_INFO = 'info'
export const LOG_WARN = 'warn'
export const LOG_ERROR = 'error'

// 输出策略
export const OUTPUT_PER_KEYWORD = 'per_keyword'
export const OUTPUT_MERGED = 'merged'
export const OUTPUT_PER_SOURCE = 'per_source'

// 拆分模式
export const SPLIT_BY_SHEET = 'by_sheet'
export const SPLIT_BY_ROWS = 'by_rows'
export const SPLIT_BY_COLUMN = 'by_column'
export const SPLIT_BY_KEYWORD = 'by_keyword'
