// 前端配置持久化的 API 封装。
// 后端把 JSON 字符串原样落盘到 %APPDATA%/excel-master/config.json，
// 不解析具体字段，前端字段改动无需后端配合。

function app() {
    const g = window.go
    if (!g || !g.main || !g.main.App) {
        throw new Error('Wails 未就绪')
    }
    return g.main.App
}

/** 读取配置；后端在文件不存在/损坏时返回 "{}"。 */
export async function loadConfig() {
    const raw = await app().LoadConfig()
    try {
        return JSON.parse(raw || '{}')
    } catch {
        return {}
    }
}

/** 写入配置（整体覆盖）。 */
export async function saveConfig(obj) {
    return app().SaveConfig(JSON.stringify(obj))
}

/** 返回配置文件的绝对路径（用于 UI 显示"配置位置"或排错）。 */
export function configPath() {
    return app().ConfigPath()
}
