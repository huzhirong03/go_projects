package service

import (
	"os"
	"path/filepath"
)

// 日志目录策略（和 config_service.go 的"绿色优先 + fallback"思路一致）：
//
//  1. 优先 exe 同目录的 logs/ 子目录 —— 真·绿色：
//     - 用户搬走整个软件文件夹时日志一起带走
//     - 删软件时不会留垃圾在 C 盘
//
//  2. exe 同目录不可写（Program Files / 只读盘 / dev 模式）
//     → fallback 到 <UserCacheDir>/excel-master/logs/
//
// Windows fallback: %LOCALAPPDATA%\excel-master\logs\
// macOS   fallback: ~/Library/Caches/excel-master/logs/
// Linux   fallback: ~/.cache/excel-master/logs/

const logsDirName = "logs"

// portableLogsDir 返回 exe 同目录的 logs/ 路径；exe 路径拿不到时返回空串。
func portableLogsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Join(filepath.Dir(exe), logsDirName)
}

// fallbackLogsDir 返回 UserCacheDir 下的日志目录路径。
func fallbackLogsDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "excel-master", logsDirName), nil
}

// LogsDir 决定本次写日志用哪个路径。会保证目录存在（mkdir -p）。
//
// 决策树：
//   - 已存在的 portable logs/ 目录 → 直接用（用户已绿色化）
//   - portable 目录可创建/可写 → 用 portable（首次落地为绿色）
//   - 都不行 → fallback 到 UserCacheDir
//
// 任何路径无效时返回错误。
func LogsDir() (string, error) {
	if portable := portableLogsDir(); portable != "" {
		// 试试创建 portable 目录；成功就用它
		if err := os.MkdirAll(portable, 0o755); err == nil {
			// 再用探针确认确实可写（防只读盘）
			if dirIsWritable(portable) {
				return portable, nil
			}
		}
	}
	fb, err := fallbackLogsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(fb, 0o755); err != nil {
		return "", err
	}
	return fb, nil
}
