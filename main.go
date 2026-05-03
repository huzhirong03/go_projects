package main

import (
	"embed"
	"io"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// procStart 是进程入口时间，用于启动性能诊断。
var procStart = time.Now()

// initLogTee 把 log.Printf 同时写到 stderr + 当前目录 startup.log。
// 用于把启动 timing 日志保留下来供事后分析（dev 模式下 PowerShell 关闭就丢）。
func initLogTee() *os.File {
	f, err := os.OpenFile("startup.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
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
		Title:  "excel-master",
		Width:  1000,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
