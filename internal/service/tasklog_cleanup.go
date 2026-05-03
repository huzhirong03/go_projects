package service

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 日志保留策略：
//   - task-*.log 保留 maxAgeDays 天，到期自动清掉
//   - 不限制总大小（用户磁盘空间是用户的事，软件只控制自己产生的垃圾）
//   - 启动时清一次，之后不清（避免影响业务路径性能）
//
// 7 天是经验值：覆盖一周内的 bug 反馈窗口，又不会让用户磁盘爆掉。
const taskLogMaxAgeDays = 7

// CleanupOldTaskLogs 清掉日志目录里超过 maxAge 的 task-*.log。
// 任何错误静默忽略（清理是 best-effort，不应阻塞启动）。
//
// 返回删除的文件数 + 释放的字节数，给启动日志记一笔便于审计。
func CleanupOldTaskLogs() (deleted int, freed int64) {
	dir, err := LogsDir()
	if err != nil {
		return 0, 0
	}
	cutoff := time.Now().Add(-time.Duration(taskLogMaxAgeDays) * 24 * time.Hour)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		// 只处理我们自己生成的 task-*.log，避免误删用户手动放的文件
		if !strings.HasPrefix(name, "task-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		// 用 ModTime 判断"最后写入时间"，比 BirthTime 更可靠（Linux/Windows 都支持）
		if info.ModTime().After(cutoff) {
			continue
		}
		full := filepath.Join(dir, name)
		size := info.Size()
		if err := os.Remove(full); err == nil {
			deleted++
			freed += size
		}
	}
	return deleted, freed
}
