// 唯一访问 Go 后端的入口。Vue 组件禁止直接使用 window.go.*。
// 如果 Wails 尚未注入（例如浏览器直接打开 index.html），抛出可读错误。

function app() {
    const g = window.go
    if (!g || !g.main || !g.main.App) {
        throw new Error('Wails 未就绪：请通过 wails dev 或打包后的 exe 运行，不要直接打开 index.html')
    }
    return g.main.App
}

export function chooseFolder(title = '选择文件夹') {
    return app().ChooseFolder(title)
}

export function chooseFile(title = '选择 Excel 文件') {
    return app().ChooseFile(title)
}

export function openPath(path) {
    return app().OpenPath(path)
}

export function cancelTask(taskId) {
    return app().CancelTask(taskId)
}

export function respondFileBlocked(promptId, action) {
    return app().RespondFileBlocked(promptId, action)
}
