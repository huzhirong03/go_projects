// 批量提取 API 封装。Vue 组件只调这里，不直接 window.go。

function app() {
    const g = window.go
    if (!g || !g.main || !g.main.App) {
        throw new Error('Wails 未就绪')
    }
    return g.main.App
}

/**
 * 预览文件夹：返回第一个 xlsx 的表头 + 所有 Sheet 名并集。
 * 用于用户勾选搜索列与勾选要处理的 Sheet。
 * @param {string} folder 绝对路径
 * @param {number} headerRow 表头行号（1-based）
 * @returns {Promise<{firstFile: string, columns: string[], sheets: string[]}>}
 */
export function previewFolder(folder, headerRow) {
    return app().PreviewFolder(folder, headerRow)
}

/**
 * 启动批量提取任务，立刻返回 { taskId }。进度通过事件推送。
 * @param {object} req ExtractRequest（字段见 internal/service/types.go）
 * @returns {Promise<{taskId: string}>}
 */
export function startExtract(req) {
    return app().StartExtract(req)
}
