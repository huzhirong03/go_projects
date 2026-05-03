package service

import (
	"context"

	"excel-master/internal/core"
)

// fileTeeEmitter 是装饰器：包装一个 inner emitter，把所有 emit 调用同时
// 写到磁盘日志（TaskLog）。前端事件不变，磁盘留 1 份完整记录。
//
// 设计要点：
//   - inner 必须存在（生产是 wailsEmitter，测试可注入 buffered）
//   - tl 可以是 nil 或 open 失败的 TaskLog（Write 会被静默忽略），调用方不用判空
//   - 不持有 ctx：取消语义跟着 inner 走
type fileTeeEmitter struct {
	inner core.EventEmitter
	tl    *TaskLog
}

// wrapEmitterWithTaskLog 给 emitter 套一层文件 tap。
// 即使 tl 是 open 失败的 zero-value TaskLog，返回的装饰器仍能正常工作
// （磁盘写入失败时不会回退到错误，UI 事件不受影响）。
func wrapEmitterWithTaskLog(inner core.EventEmitter, tl *TaskLog) core.EventEmitter {
	if inner == nil {
		return nil
	}
	if tl == nil {
		return inner
	}
	return &fileTeeEmitter{inner: inner, tl: tl}
}

func (e *fileTeeEmitter) Progress(p core.Progress) {
	e.tl.WriteProgress(p.Stage, p.Done, p.Total, p.Message)
	e.inner.Progress(p)
}

func (e *fileTeeEmitter) Log(level, msg string) {
	e.tl.Write(level, msg)
	e.inner.Log(level, msg)
}

func (e *fileTeeEmitter) Done(result any) {
	e.tl.Write(core.LogInfo, "[DONE] task completed successfully")
	e.inner.Done(result)
}

func (e *fileTeeEmitter) Error(err error) {
	if err != nil {
		e.tl.Write(core.LogError, err.Error())
	}
	e.inner.Error(err)
}

// PromptFileBlocked 把"文件被占用"交互转发给 inner，磁盘只记录一行摘要
// （不阻塞等待结果）。inner 可能不实现这个方法，所以做类型断言。
func (e *fileTeeEmitter) PromptFileBlocked(ctx context.Context, req core.FileBlockedRequest) core.FileBlockedChoice {
	e.tl.Write(core.LogWarn, "[file-blocked] "+req.Path+" - "+req.Message)
	if p, ok := e.inner.(interface {
		PromptFileBlocked(context.Context, core.FileBlockedRequest) core.FileBlockedChoice
	}); ok {
		choice := p.PromptFileBlocked(ctx, req)
		e.tl.Write(core.LogInfo, "[file-blocked] user chose: "+string(choice))
		return choice
	}
	return core.FileBlockedCancel
}
