package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskLog 是单个任务的日志落盘对象。线程安全。
//
// 用法（在 service 层 goroutine 顶部）：
//
//	tl, _ := OpenTaskLog(taskID, "extract")
//	defer tl.Close()
//	emitter := wrapEmitterWithTaskLog(realEmitter, tl)
//	... 业务逻辑用 emitter ...
//
// 即使 OpenTaskLog 失败（磁盘满 / 权限问题），返回的 *TaskLog 仍可调用 Write/Close
// 而不 panic（内部 file 为 nil 时静默丢弃），不阻塞主流程。
type TaskLog struct {
	mu   sync.Mutex
	file *os.File // 可能为 nil（open 失败时），调用方不需要判空
	path string   // 完整文件路径，给前端"打开此日志"用
}

// OpenTaskLog 在日志目录创建一个 task-{taskID}-{kind}.log 文件并返回 TaskLog。
// kind 用于标识任务类型（"extract" / "split"）。
//
// 失败时返回的 *TaskLog 不为 nil，但内部 file 是 nil（写操作会被静默丢弃）。
// 这种"幽灵 logger"设计避免 service 层每次 Write 都要判空，简化使用。
func OpenTaskLog(taskID, kind string) (*TaskLog, error) {
	dir, err := LogsDir()
	if err != nil {
		return &TaskLog{}, err // 返回空 TaskLog 让调用方不用判空
	}
	// 文件名：task-{taskID}-{kind}-{timestamp}.log
	// taskID 已经带 ms 时间戳 + 序号，但加 yyyymmdd_HHMM 前缀更直观
	name := fmt.Sprintf("task-%s-%s-%s.log",
		time.Now().Format("20060102_150405"), kind, sanitizeTaskID(taskID))
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return &TaskLog{path: path}, err
	}
	tl := &TaskLog{file: f, path: path}
	tl.writeHeader(taskID, kind)
	return tl, nil
}

// Path 返回日志文件绝对路径。日志 open 失败时仍可能返回路径（用于错误提示）。
func (tl *TaskLog) Path() string {
	if tl == nil {
		return ""
	}
	return tl.path
}

// writeHeader 在文件开头写一段任务元信息，方便后续翻日志一眼看清任务身份。
func (tl *TaskLog) writeHeader(taskID, kind string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if tl.file == nil {
		return
	}
	fmt.Fprintf(tl.file, "==================================================\n")
	fmt.Fprintf(tl.file, "Task ID:    %s\n", taskID)
	fmt.Fprintf(tl.file, "Task Kind:  %s\n", kind)
	fmt.Fprintf(tl.file, "Started At: %s\n", time.Now().Format("2006-01-02 15:04:05.000 -07:00"))
	fmt.Fprintf(tl.file, "==================================================\n")
}

// Write 写一行带时间戳和级别的日志。
//   - level 取 core.LogInfo / LogWarn / LogError 字符串，简化为 INFO/WARN/ERROR
//   - msg 可以含换行（多行 message 会原样落盘）
//
// 失败静默忽略（不阻塞业务）。
func (tl *TaskLog) Write(level, msg string) {
	if tl == nil {
		return
	}
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if tl.file == nil {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	_, _ = fmt.Fprintf(tl.file, "%s [%s] %s\n", ts, normalizeLevel(level), msg)
}

// WriteProgress 给 Progress 事件留一个独立入口，便于日后改格式（比如不写 progress
// 只写关键 stage 切换）。当前实现：完整记录每条 Progress，便于回放。
func (tl *TaskLog) WriteProgress(stage string, done, total int64, msg string) {
	if tl == nil {
		return
	}
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if tl.file == nil {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	_, _ = fmt.Fprintf(tl.file, "%s [PROG] %s %d/%d %s\n", ts, stage, done, total, msg)
}

// Close 写一段任务结尾元信息并关闭文件。
// 多次调用安全（第二次开始静默忽略）。
func (tl *TaskLog) Close() error {
	if tl == nil {
		return nil
	}
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if tl.file == nil {
		return nil
	}
	fmt.Fprintf(tl.file, "==================================================\n")
	fmt.Fprintf(tl.file, "Ended At:   %s\n", time.Now().Format("2006-01-02 15:04:05.000 -07:00"))
	fmt.Fprintf(tl.file, "==================================================\n")
	err := tl.file.Close()
	tl.file = nil
	return err
}

// normalizeLevel 把 core 包的小写级别字符串变成大写 5 字符内对齐，
// 便于日志列对齐扫读。
func normalizeLevel(level string) string {
	switch level {
	case "warn":
		return "WARN "
	case "error":
		return "ERROR"
	case "info":
		return "INFO "
	default:
		// 未知级别也保留（可能未来扩展），但截断到 5 字符避免错位
		if len(level) > 5 {
			return level[:5]
		}
		return level
	}
}

// sanitizeTaskID 把 taskID 里 Windows 文件名禁用字符替换掉。
// taskID 由 service.newTaskID() 生成，理论上只含字母数字横线，但防御性处理。
func sanitizeTaskID(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
