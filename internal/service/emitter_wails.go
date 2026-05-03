package service

import (
	"context"
	"fmt"
	"sync/atomic"

	"excel-master/internal/core"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// NewWailsEmitterFactory 基于 Wails runtime 构造发射器工厂。
// 每个 emit 都会把 taskID 作为首个 payload，便于前端按任务分流。
//
// 事件名常量：core.EventProgress / EventLog / EventDone / EventError。
// 前端 frontend/src/types/events.js 必须与之镜像。
func NewWailsEmitterFactory(ctx context.Context) EmitterFactory {
	var seq uint64
	return func(taskID string, broker *filePromptBroker) Emitter {
		return &wailsEmitter{ctx: ctx, taskID: taskID, broker: broker, seq: &seq}
	}
}

type wailsEmitter struct {
	ctx    context.Context
	taskID string
	broker *filePromptBroker
	seq    *uint64
}

type progressEvent struct {
	TaskID  string `json:"taskId"`
	Stage   string `json:"stage"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Message string `json:"message"`
}

type logEvent struct {
	TaskID string `json:"taskId"`
	Level  string `json:"level"`
	Msg    string `json:"msg"`
}

type doneEvent struct {
	TaskID string `json:"taskId"`
	Result any    `json:"result"`
}

type errorEvent struct {
	TaskID  string `json:"taskId"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type fileBlockedEvent struct {
	TaskID   string `json:"taskId"`
	PromptID string `json:"promptId"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

func (w *wailsEmitter) Progress(p core.Progress) {
	runtime.EventsEmit(w.ctx, core.EventProgress, progressEvent{
		TaskID: w.taskID, Stage: p.Stage, Done: p.Done, Total: p.Total, Message: p.Message,
	})
}

func (w *wailsEmitter) Log(level, msg string) {
	runtime.EventsEmit(w.ctx, core.EventLog, logEvent{
		TaskID: w.taskID, Level: level, Msg: msg,
	})
}

func (w *wailsEmitter) Done(result any) {
	runtime.EventsEmit(w.ctx, core.EventDone, doneEvent{
		TaskID: w.taskID, Result: result,
	})
}

func (w *wailsEmitter) Error(err error) {
	ev := errorEvent{TaskID: w.taskID, Message: err.Error()}
	if appErr, ok := err.(*core.AppError); ok {
		ev.Code = appErr.Code
		ev.Message = appErr.Message
	}
	runtime.EventsEmit(w.ctx, core.EventError, ev)
}

func (w *wailsEmitter) PromptFileBlocked(ctx context.Context, req core.FileBlockedRequest) core.FileBlockedChoice {
	if w.broker == nil || w.seq == nil {
		return core.FileBlockedCancel
	}
	if req.PromptID == "" {
		req.PromptID = fmt.Sprintf("%s-file-%d", w.taskID, atomic.AddUint64(w.seq, 1))
	}
	ch := w.broker.register(req.PromptID)
	defer w.broker.unregister(req.PromptID)
	runtime.EventsEmit(w.ctx, core.EventFileBlocked, fileBlockedEvent{
		TaskID: w.taskID, PromptID: req.PromptID, Path: req.Path, Message: req.Message,
	})
	select {
	case choice := <-ch:
		return choice
	case <-ctx.Done():
		return core.FileBlockedCancel
	}
}
