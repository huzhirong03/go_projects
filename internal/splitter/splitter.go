package splitter

import (
	"context"

	"excel-master/internal/core"
)

// Split 按 task.Mode 分发到对应的实现。是 splitter 包对外的统一入口。
func Split(ctx context.Context, task core.SplitTask, emitter core.EventEmitter) (*Result, error) {
	switch task.Mode {
	case core.SplitBySheet:
		return SplitBySheet(ctx, task, emitter)
	case core.SplitByRows:
		return SplitByRows(ctx, task, emitter)
	case core.SplitByColumn:
		return SplitByColumn(ctx, task, emitter)
	case core.SplitByKeyword:
		return SplitByKeyword(ctx, task, emitter)
	default:
		return nil, core.New("INVALID_MODE", "未知拆分模式: "+string(task.Mode))
	}
}
