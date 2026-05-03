package main

import (
	"embed"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"excel-master/internal/core"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
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

// webview2FixedRuntimePath 返回 WebView2 Fixed Version Runtime 的路径。
// 设计：
//   - exe 同目录有 webview2_runtime/ 子目录 → 返回它的绝对路径（绿色发布）
//   - 没有 → 返回空串，让 Wails 用系统默认 WebView2 Runtime（dev 模式 / 用户已装）
//
// 这样同一份 exe：
//   - 打包发给学员（带 webview2_runtime/）→ 真绿色，零依赖
//   - 开发期 wails dev → 用系统 WebView2，不需要带 runtime
//   - 用户自己装了 WebView2 又删了 runtime/ 文件夹 → 自动 fallback 到系统
func webview2FixedRuntimePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	candidate := filepath.Join(filepath.Dir(exe), "webview2_runtime")
	// 必须能 stat 到 msedgewebview2.exe 才算有效
	// （只要文件夹存在不够：可能解压失败留下空目录）
	if _, err := os.Stat(filepath.Join(candidate, "msedgewebview2.exe")); err != nil {
		return ""
	}
	return candidate
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

	// WebView2 Fixed Version：如果 exe 同目录有 runtime 文件夹就用它（真绿色），
	// 否则空串让 Wails 走系统默认 WebView2（dev 模式 / 用户已装的场景）。
	wv2Path := webview2FixedRuntimePath()
	if wv2Path != "" {
		log.Printf("[STARTUP] using bundled WebView2 runtime: %s", wv2Path)
	} else {
		log.Printf("[STARTUP] using system WebView2 (no bundled runtime found)")
	}

	// Create application with options
	log.Printf("[STARTUP] before wails.Run, +%v", time.Since(procStart))
	err := wails.Run(&options.App{
		Title:  core.AppName + " " + core.Version + " · " + core.BrandTagline,
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
		Windows: &windows.Options{
			// WebView2 Fixed Version Runtime 路径（空串 = 用系统默认）
			WebviewBrowserPath: wv2Path,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
