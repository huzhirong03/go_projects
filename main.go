package main

import (
	"embed"
	"excel-master/internal/core"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// procStart 是进程入口时间，用于启动性能诊断。
var procStart = time.Now()

// startupLogPath 算 startup.log 的真实落点：
//   - 优先 exe 同目录（绿色 exe 用户搬走整包就带走日志）
//   - exe 同目录不可写（Program Files / 只读盘）→ fallback 到系统 TEMP
//   - 拿不到 exe 路径或 TEMP 都不行 → 返回空串，让调用方放弃日志（不阻塞启动）
func startupLogPath() string {
	exe, err := os.Executable()
	if err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		dir := filepath.Dir(exe)
		// 探针写一次：能写就用 exe 同目录
		probe, err := os.CreateTemp(dir, ".excel-master-logprobe-*")
		if err == nil {
			_ = probe.Close()
			_ = os.Remove(probe.Name())
			return filepath.Join(dir, "startup.log")
		}
	}
	// fallback：系统 TEMP，避免污染 cwd（cwd 不可控）
	return filepath.Join(os.TempDir(), "excel-master-startup.log")
}

// initLogTee 把 log.Printf 同时写到 stderr + startup.log。
// 日志路径见 startupLogPath。每次启动 truncate 不堆积。
// 失败（拿不到任何可写位置）时静默返回 nil，不阻塞启动。
func initLogTee() *os.File {
	p := startupLogPath()
	if p == "" {
		return nil
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("[STARTUP] log file: %s", p)
	return f
}

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if f := initLogTee(); f != nil {
		defer f.Close()
	}
	log.Printf("[STARTUP] main() begin, procStart=%v", procStart.Format("15:04:05.000"))
	// Create an instance of the app structure
	app := NewApp()
	log.Printf("[STARTUP] NewApp ok, +%v", time.Since(procStart))

	// Create application with options
	log.Printf("[STARTUP] before wails.Run, +%v", time.Since(procStart))
	err := wails.Run(&options.App{
		Title:  core.AppName + " " + core.Version,
		Width:  1000,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// 跟前端 --bg: #eef3ee 对齐，避免启动闪白/闪深色
		BackgroundColour: &options.RGBA{R: 238, G: 243, B: 238, A: 255},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady, // 窗口实际显示后再居中，否则 SetPosition 不稳
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
