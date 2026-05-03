// 日志相关 API：打开日志文件夹 / 拿日志路径。
// 后端在 internal/service/tasklog_path.go 决定路径（exe 同目录 / fallback UserCacheDir）。

function app() {
    const g = window.go
    if (!g || !g.main || !g.main.App) {
        throw new Error('Wails 未就绪')
    }
    return g.main.App
}

/** 用系统资源管理器打开日志文件夹（学员把最新 task-*.log 发给开发者用）。 */
export async function openLogFolder() {
    return app().OpenLogFolder()
}

/** 拿日志目录绝对路径，给 toast 提示用。 */
export async function logsDirPath() {
    return app().LogsDirPath()
}
