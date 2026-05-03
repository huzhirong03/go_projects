// 配置 store：单例缓存 + 防抖保存 + 按视图分桶。
//
// 用法：
//   import { getViewConfig, saveViewConfig } from '@/stores/config'
//
//   onMounted(async () => {
//       const saved = await getViewConfig('extract')
//       Object.assign(form, saved)
//       watch(form, () => saveViewConfig('extract', toRaw(form)), { deep: true })
//   })
//
// 设计要点：
//   - 整个 config 对象一次读、一次写；按视图 viewKey 分桶（'extract' / 'split'）
//   - saveViewConfig 内部 500ms 防抖；连续的字段变化合并为一次 IO
//   - 加载失败/解析失败静默降级，永不阻塞 UI

import { loadConfig as apiLoad, saveConfig as apiSave } from '../api/config.js'

let _cache = null
let _loadingPromise = null
let _saveTimer = null

async function _ensureLoaded() {
    if (_cache !== null) return _cache
    if (!_loadingPromise) {
        _loadingPromise = apiLoad()
            .then(c => { _cache = c || {}; return _cache })
            .catch(e => { console.warn('LoadConfig failed:', e); _cache = {}; return _cache })
    }
    return _loadingPromise
}

/** 拿某个视图的已保存配置（不存在返回 {}）。 */
export async function getViewConfig(viewKey) {
    const c = await _ensureLoaded()
    return c[viewKey] ? { ...c[viewKey] } : {}
}

/** 保存某个视图的配置（防抖 500ms 后落盘）。 */
export function saveViewConfig(viewKey, snapshot) {
    if (_cache === null) _cache = {}
    _cache[viewKey] = JSON.parse(JSON.stringify(snapshot))
    if (_saveTimer) clearTimeout(_saveTimer)
    _saveTimer = setTimeout(() => {
        _saveTimer = null
        apiSave(_cache).catch(e => console.warn('SaveConfig failed:', e))
    }, 500)
}

/**
 * 立刻把挂起的保存 flush 到磁盘。用于窗口关闭前，防止 500ms 防抖未触发导致配置丢失。
 * 返回 Promise（调用方可 await，但 pagehide/beforeunload 里通常没法等）。
 */
export function flushConfig() {
    if (_saveTimer) {
        clearTimeout(_saveTimer)
        _saveTimer = null
    }
    if (_cache === null) return Promise.resolve()
    return apiSave(_cache).catch(e => console.warn('SaveConfig flush failed:', e))
}

// 窗口关闭前 flush。pagehide 在 Wails/WebView2 上比 beforeunload 更可靠。
if (typeof window !== 'undefined') {
    window.addEventListener('pagehide', () => { flushConfig() })
    window.addEventListener('beforeunload', () => { flushConfig() })
}

/** 清空所有视图配置（"恢复默认"按钮可用）。 */
export async function resetAllConfig() {
    _cache = {}
    if (_saveTimer) clearTimeout(_saveTimer)
    return apiSave({})
}
