package service

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 日志保留策略（按优先级从高到低）：
//
//  1. 最新 taskLogKeepRecent 个 task-*.log 永远保底（不论 mtime 多老、不论总大小多大）。
//     原因：学员跑错任务后，可能过几天才想起来要把日志发给开发者排查。
//     保底"最近 N 个"比"按时间"更符合真实使用模式：跑完 20 个任务后才发日志的场景
//     比"跑完 7 天后才发"更常见。
//
//  2. 保底之外的老文件，mtime > taskLogMaxAge 就删。
//     3 天给学员留了周末弹性：周五跑错任务，周一回来发日志还在。
//
//  3. 删完时间规则后，如果总大小仍 > taskLogQuotaSoftMB，继续按 mtime 从最老往前删，
//     直到总大小 ≤ taskLogQuotaTargetMB（10% buffer，避免刚清就又触发）。
//     目的：单日跑了大量 10MB 级任务时兜底，防目录炸开。
//
// 配合 tasklog.go 的单文件 10MB 硬上限，四道防线把日志体积稳稳控制住。
const (
	taskLogKeepRecent       = 20                 // 规则①：保底最新 N 个
	taskLogMaxAge           = 3 * 24 * time.Hour // 规则②：3 天
	taskLogQuotaSoftMB      = 100                // 规则③：触发配额清理的总大小阈值
	taskLogQuotaTargetMB    = 80                 // 规则③：清理目标，留 20MB buffer
	taskLogQuotaSoftBytes   = taskLogQuotaSoftMB << 20
	taskLogQuotaTargetBytes = taskLogQuotaTargetMB << 20
)

// taskLogEntry 是一份 task-*.log 的快照，便于按 mtime 排序。
type taskLogEntry struct {
	path  string
	size  int64
	mtime time.Time
}

// CleanupOldTaskLogs 按上述三条规则清理 task-*.log。幂等 + 并发安全（依赖 os.Remove 原子性）。
//
// 返回删除的文件数 + 释放的字节数，给启动日志 / 调试日志记一笔便于审计。
// 任何错误静默忽略（清理是 best-effort，不应阻塞启动或业务路径）。
func CleanupOldTaskLogs() (deleted int, freed int64) {
	dir, err := LogsDir()
	if err != nil {
		return 0, 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}

	// 收集所有符合命名规则的 task-*.log + 元信息
	logs := make([]taskLogEntry, 0, len(entries))
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
		logs = append(logs, taskLogEntry{
			path:  filepath.Join(dir, name),
			size:  info.Size(),
			mtime: info.ModTime(),
		})
	}
	if len(logs) == 0 {
		return 0, 0
	}

	// 按 mtime 降序：最新的排前面，便于切出"保底最新 N 个"
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].mtime.After(logs[j].mtime)
	})

	// 规则①：最新 taskLogKeepRecent 个绝不动
	kept := logs
	if len(logs) > taskLogKeepRecent {
		kept = logs[:taskLogKeepRecent]
		old := logs[taskLogKeepRecent:]

		// 规则②：非保底的老文件里，mtime > 1 天就删
		cutoff := time.Now().Add(-taskLogMaxAge)
		survivors := make([]taskLogEntry, 0, len(old))
		for _, e := range old {
			if e.mtime.Before(cutoff) {
				if err := os.Remove(e.path); err == nil {
					deleted++
					freed += e.size
				}
				continue
			}
			survivors = append(survivors, e)
		}

		// 规则③：保底 + 时间清剩下的合起来看总大小，超配额就按 mtime 从最老往前删
		all := append([]taskLogEntry{}, kept...)
		all = append(all, survivors...)
		var totalSize int64
		for _, e := range all {
			totalSize += e.size
		}
		if totalSize > taskLogQuotaSoftBytes {
			// survivors 已经按 mtime 降序（继承自原排序），reverse 成升序从最老删
			sort.Slice(survivors, func(i, j int) bool {
				return survivors[i].mtime.Before(survivors[j].mtime)
			})
			for _, e := range survivors {
				if totalSize <= taskLogQuotaTargetBytes {
					break
				}
				if err := os.Remove(e.path); err == nil {
					deleted++
					freed += e.size
					totalSize -= e.size
				}
			}
		}
	}

	return deleted, freed
}
