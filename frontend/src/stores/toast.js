// 极简 toast 通知系统：reactive 列表 + 自动消失。
// V1.1 取代项目里的所有 alert / confirm。
//
// 用法：
//   import { showToast } from '@/stores/toast'
//   showToast('操作成功', 'success')
//   showToast('请先选择文件夹', 'warn')
//   showToast(err.message, 'error', 6000)

import { reactive } from 'vue'

let nextId = 1

export const toastState = reactive({
    items: [], // { id, level, msg, ts }
})

/**
 * @param {string} msg 消息内容
 * @param {'info'|'success'|'warn'|'error'} level
 * @param {number} durationMs 自动消失的毫秒数；error 默认 6 秒，其他 3 秒
 */
export function showToast(msg, level = 'info', durationMs) {
    if (!msg) return
    const id = nextId++
    const item = { id, level, msg: String(msg), ts: Date.now() }
    toastState.items.push(item)
    const dur = durationMs ?? (level === 'error' ? 6000 : 3000)
    setTimeout(() => dismissToast(id), dur)
}

export function dismissToast(id) {
    const i = toastState.items.findIndex(x => x.id === id)
    if (i >= 0) toastState.items.splice(i, 1)
}
