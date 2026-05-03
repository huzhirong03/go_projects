package main

import (
	"context"
	"os/exec"

	"excel-master/internal/service"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是 Wails 桥接层。严禁在这里写业务代码，每个方法最多 5 行。
// 实现逻辑全部在 internal/service/。
type App struct {
	ctx context.Context
	svc *service.Service
}

// NewApp creates a new App application struct
func NewApp() *App { return &App{} }

// startup 在 Wails 启动时注入 runtime context，用于 EventsEmit。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.svc = service.NewService(service.NewWailsEmitterFactory(ctx))
}

// PreviewFolder 前端在选完文件夹后调一次，拿第一个 xlsx 的表头 + 所有 Sheet 名。
func (a *App) PreviewFolder(folder string, headerRow int) (*service.HeaderPreview, error) {
	return a.svc.PreviewFolder(folder, headerRow)
}

// PreviewFile 前端在选完单文件后调一次，拿 Sheet 名 + 第一个 Sheet 的表头。
func (a *App) PreviewFile(path string, headerRow int) (*service.FilePreview, error) {
	return a.svc.PreviewFile(path, headerRow)
}

// StartExtract 启动批量提取任务，立刻返回 TaskHandle。事件通过 runtime EventsEmit 推送。
func (a *App) StartExtract(req service.ExtractRequest) (*service.TaskHandle, error) {
	return a.svc.StartExtract(req)
}

// StartSplit 启动单文件拆分任务，立刻返回 TaskHandle。
func (a *App) StartSplit(req service.SplitRequest) (*service.TaskHandle, error) {
	return a.svc.StartSplit(req)
}

// CancelTask 取消指定 TaskID 的任务。
func (a *App) CancelTask(taskID string) bool { return a.svc.CancelTask(taskID) }

// ChooseFolder 弹出原生文件夹选择器，返回用户选中的绝对路径（取消返回空串）。
func (a *App) ChooseFolder(title string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

// ChooseFile 弹出原生文件选择器，默认过滤 .xlsx。
func (a *App) ChooseFile(title string) (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: []runtime.FileFilter{{DisplayName: "Excel 文件 (*.xlsx)", Pattern: "*.xlsx"}},
	})
}

// OpenPath 调用 Windows 文件协议处理器，用系统默认程序打开 path（文件或文件夹）。
// 例如：xlsx → Excel/WPS；目录 → 资源管理器。等价于双击。
// 之前的 BrowserOpenURL 不行：它只能在浏览器里打开 file:// URL，浏览器不会用 Excel 打开 xlsx。
func (a *App) OpenPath(path string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
}

// LoadConfig 读取持久化的前端配置（JSON 字符串）。
func (a *App) LoadConfig() (string, error) { return a.svc.LoadConfig() }

// SaveConfig 写入前端配置（JSON 字符串）。
func (a *App) SaveConfig(data string) error { return a.svc.SaveConfig(data) }

// ConfigPath 返回配置文件的绝对路径，便于前端在 toast 里展示给用户。
func (a *App) ConfigPath() (string, error) { return a.svc.ConfigPath() }
