//go:build windows

package main

import "syscall"

// hiddenCmdAttr 返回 Windows 下让 exec.Command 不弹黑色控制台窗口的属性。
//
// 背景：Go 在 Windows 上跑 exec.Command("rundll32", ...) / exec.Command("cmd", ...)
// 等命令时，默认会给子进程分配一个新控制台窗口 → 学员看到"闪一下黑框"。
// 虽然是瞬间的但对 GUI 应用来说很不专业。
//
// 两个关键 flag：
//   - HideWindow: true                    — 告诉 Go 创建时不要显示窗口
//   - CreationFlags: CREATE_NO_WINDOW     — 告诉 Windows 根本别给这个子进程分配控制台
//
// 仅在 Windows build 生效；其他系统的 app_other.go 返回 nil。
func hiddenCmdAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW，Windows API 常量
	}
}
