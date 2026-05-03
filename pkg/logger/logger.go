// Package logger 提供统一的日志门面。
// 同时输出到标准错误流和可选的事件发射器（用于 Wails 前端展示）。
package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"excel-master/internal/core"
)

// Sink 代表日志的一个输出端。
type Sink interface {
	Write(level, msg string)
}

// Logger 是全局日志器。
type Logger struct {
	mu      sync.Mutex
	emitter core.EventEmitter // 可选，用于向前端发事件
	std     *log.Logger
}

var defaultLogger = &Logger{
	std: log.New(os.Stderr, "", 0),
}

// Default 返回默认 logger。
func Default() *Logger { return defaultLogger }

// SetEmitter 注入 EventEmitter（通常由 service 层在任务开始时注入）。
// 传 nil 可清空。
func (l *Logger) SetEmitter(e core.EventEmitter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.emitter = e
}

func (l *Logger) emit(level, msg string) {
	l.mu.Lock()
	e := l.emitter
	l.mu.Unlock()
	ts := time.Now().Format("15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] %s", ts, level, msg)
	l.std.Println(line)
	if e != nil {
		e.Log(level, msg)
	}
}

// Info 记录普通信息。
func (l *Logger) Info(format string, args ...any) {
	l.emit(core.LogInfo, fmt.Sprintf(format, args...))
}

// Warn 记录警告。
func (l *Logger) Warn(format string, args ...any) {
	l.emit(core.LogWarn, fmt.Sprintf(format, args...))
}

// Error 记录错误。
func (l *Logger) Error(format string, args ...any) {
	l.emit(core.LogError, fmt.Sprintf(format, args...))
}

// 便捷全局调用，免去传 Default()。
func Info(format string, args ...any)  { defaultLogger.Info(format, args...) }
func Warn(format string, args ...any)  { defaultLogger.Warn(format, args...) }
func Error(format string, args ...any) { defaultLogger.Error(format, args...) }
