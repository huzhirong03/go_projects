// 单文件拆分 API 封装。

function app() {
    const g = window.go
    if (!g || !g.main || !g.main.App) {
        throw new Error('Wails 未就绪')
    }
    return g.main.App
}

/**
 * 启动拆分任务。
 * @param {object} req SplitRequest
 * @returns {Promise<{taskId: string}>}
 */
export function startSplit(req) {
    return app().StartSplit(req)
}

/**
 * 预览单个 xlsx：返回所有 Sheet 名 + 第一个 Sheet 的表头。
 * @param {string} path 文件绝对路径
 * @param {number} headerRow 表头行号（1-based）；0 表示无表头
 * @returns {Promise<{path: string, sheets: string[], columns: string[]}>}
 */
export function previewFile(path, headerRow) {
    return app().PreviewFile(path, headerRow)
}
