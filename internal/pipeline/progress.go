// Package pipeline 提供任务调度与事件汇聚能力。
// 所有耗时任务都把进度、日志、完成、错误事件写入 EventEmitter，
// 由上层（service 层）决定把事件广播到控制台还是 Wails 前端。
package pipeline

import (
	"sync"

	"excel-master/internal/core"
	"excel-master/pkg/logger"
)

// NoopEmitter 丢弃所有事件，主要用于测试。
type NoopEmitter struct{}

func (NoopEmitter) Progress(core.Progress) {}
func (NoopEmitter) Log(string, string)     {}
func (NoopEmitter) Done(any)               {}
func (NoopEmitter) Error(error)            {}

// LogEmitter 把事件打到 pkg/logger，适合命令行 demo 和开发期。
type LogEmitter struct{}

func (LogEmitter) Progress(p core.Progress) {
	logger.Info("[进度] %s %d/%d %s", p.Stage, p.Done, p.Total, p.Message)
}

func (LogEmitter) Log(level, msg string) {
	switch level {
	case core.LogWarn:
		logger.Warn(msg)
	case core.LogError:
		logger.Error(msg)
	default:
		logger.Info(msg)
	}
}

func (LogEmitter) Done(result any) {
	logger.Info("[完成] %+v", result)
}

func (LogEmitter) Error(err error) {
	logger.Error("[任务错误] %v", err)
}

// BufferedEmitter 在内存里缓存所有事件，便于集成测试断言。
// 字段以小写存储，外部通过 getter 读取，避免与接口方法名冲突。
type BufferedEmitter struct {
	mu       sync.Mutex
	progress []core.Progress
	logs     []LogEntry
	result   any
	err      error
	doneCh   chan struct{}
	once     sync.Once
}

type LogEntry struct {
	Level string
	Msg   string
}

func NewBufferedEmitter() *BufferedEmitter {
	return &BufferedEmitter{doneCh: make(chan struct{})}
}

// 实现 core.EventEmitter。

func (b *BufferedEmitter) Progress(p core.Progress) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.progress = append(b.progress, p)
}

func (b *BufferedEmitter) Log(level, msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logs = append(b.logs, LogEntry{Level: level, Msg: msg})
}

func (b *BufferedEmitter) Done(result any) {
	b.mu.Lock()
	b.result = result
	b.mu.Unlock()
	b.closeOnce()
}

func (b *BufferedEmitter) Error(err error) {
	b.mu.Lock()
	b.err = err
	b.mu.Unlock()
	b.closeOnce()
}

func (b *BufferedEmitter) closeOnce() {
	b.once.Do(func() { close(b.doneCh) })
}

// Snapshot 返回当前事件快照（拷贝）。
func (b *BufferedEmitter) Snapshot() ([]core.Progress, []LogEntry, any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	pr := make([]core.Progress, len(b.progress))
	copy(pr, b.progress)
	lg := make([]LogEntry, len(b.logs))
	copy(lg, b.logs)
	return pr, lg, b.result, b.err
}

// Wait 阻塞直到 Done 或 Error 被调用。
func (b *BufferedEmitter) Wait() { <-b.doneCh }
