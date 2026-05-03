package splitter

import (
	"context"

	"excel-master/internal/core"
	"excel-master/internal/pipeline"
)

// Split 按 task.Mode 分发到对应的实现。是 splitter 包对外的统一入口。
func Split(ctx context.Context, task core.SplitTask, emitter core.EventEmitter) (*Result, error) {
	// 大文件前置警告：把"源文件多大 / 大概等多久"提前告诉用户，
	// 避免学员看到 UI 没动以为程序卡死。stat 失败的文件被静默忽略，不阻断业务。
	if emitter != nil && task.SourcePath != "" {
		pipeline.SizeBanner(emitter, []string{task.SourcePath})
	}
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
