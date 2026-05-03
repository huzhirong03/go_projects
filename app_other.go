//go:build !windows

package main

import "syscall"

// hiddenCmdAttr 在非 Windows 平台返回 nil（Linux/macOS 的 exec.Command
// 默认不会弹控制台窗口，不需要做任何事）。
//
// 保留这个空实现是为了让 app.go 的调用代码跨平台编译不炸。
func hiddenCmdAttr() *syscall.SysProcAttr {
	return nil
}
