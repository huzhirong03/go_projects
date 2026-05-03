// 窗口尺寸持久化：启动时恢复 size（不恢复 position），仅在窗口处于 normal
// 状态时保存。背景：在最大化或最小化状态下 WindowGetSize 可能返回屏幕全尺寸
// 或 0，存进去后下次 SetSize 会让窗口跑屏幕外，并破坏最大化按钮行为。
import {
    WindowSetSize, WindowGetSize, WindowIsNormal, WindowCenter,
} from '../../wailsjs/runtime/runtime'
import { LogStartup } from '../../wailsjs/go/main/App'
import { getViewConfig, saveViewConfig } from './config.js'

const VIEW_KEY = 'window'
const MIN_W = 720
const MIN_H = 480
// 上限取屏幕可用尺寸，避免上次保存的过大值把窗口画飞
function maxW() { return (window.screen?.availWidth || 4000) - 20 }
function maxH() { return (window.screen?.availHeight || 4000) - 40 }

let _saveTimer = null

function clamp(n, lo, hi) {
    if (typeof n !== 'number' || !isFinite(n)) return null
    return Math.max(lo, Math.min(hi, Math.round(n)))
}

/** 应用上次保存的窗口尺寸。仅恢复 size，不恢复 position（避免多显示器/异常坐标）。
 *  关键：SetSize 会保持左上角不变，结果是窗口"长大/缩小"而中心偏移。
 *  所以 SetSize 之后必须再 Center 一次，否则启动位置永远跟我们 Go 端 domReady
 *  的居中冲突（前端晚 ~12ms 调用，会盖掉 Go 端的居中）。 */
export async function restoreWindowState() {
    try {
        const saved = await getViewConfig(VIEW_KEY)
        // 诊断日志：通过 LogStartup 写到后端 startup.log，跟 Go 端时序对齐
        try {
            await LogStartup(`[restoreWindowState] saved=${JSON.stringify(saved)} screen=${window.screen?.availWidth}x${window.screen?.availHeight}`)
        } catch (_) {}
        const w = clamp(saved.width, MIN_W, maxW())
        const h = clamp(saved.height, MIN_H, maxH())
        if (w && h) {
            WindowSetSize(w, h)
            try { await LogStartup(`[restoreWindowState] WindowSetSize(${w}, ${h}) called`) } catch (_) {}
        } else {
            try { await LogStartup(`[restoreWindowState] WindowSetSize SKIPPED (w=${w}, h=${h})`) } catch (_) {}
        }
        // 不管有没有 SetSize，都做一次 Center：默认尺寸下也保证居中
        WindowCenter()
        try { await LogStartup(`[restoreWindowState] WindowCenter() called`) } catch (_) {}
    } catch (e) {
        console.warn('restoreWindowState failed:', e)
        try { await LogStartup(`[restoreWindowState] FAILED: ${e}`) } catch (_) {}
    }
}

async function snapshotAndSave() {
    try {
        // 关键：只在 normal 状态保存，避免最大化/最小化的尺寸被错误持久化
        const isNormal = await WindowIsNormal()
        if (!isNormal) return
        const size = await WindowGetSize()
        if (!size || size.w < MIN_W || size.h < MIN_H) return
        if (size.w > maxW() || size.h > maxH()) return
        saveViewConfig(VIEW_KEY, { width: size.w, height: size.h })
    } catch (e) {
        console.warn('save window state failed:', e)
    }
}

/** 装载 resize 监听，500ms 防抖保存。 */
export function installWindowStateSaver() {
    const onResize = () => {
        if (_saveTimer) clearTimeout(_saveTimer)
        _saveTimer = setTimeout(snapshotAndSave, 500)
    }
    window.addEventListener('resize', onResize)
    window.addEventListener('beforeunload', snapshotAndSave)
    window.addEventListener('pagehide', snapshotAndSave)
}
