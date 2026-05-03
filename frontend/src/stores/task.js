// 轻量任务状态管理。用 Vue 3 reactive 代替 Pinia，V1.0 需求够用。
// 每次启动任务前必须 reset()，避免事件串用。

import { reactive, readonly } from 'vue'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime.js'
import {
    EVENT_PROGRESS, EVENT_LOG, EVENT_DONE, EVENT_ERROR
} from '../types/events.js'

const state = reactive({
    taskId: '',
    running: false,
    stage: '',
    done: 0,
    total: 0,
    progressMsg: '',
    logs: [],        // { level, msg, ts }
    result: null,    // 完成后回填
    error: null,     // { code, message } 失败后回填
})

let subscribed = false

function onProgress(ev) {
    if (!state.taskId || ev.taskId !== state.taskId) return
    state.stage = ev.stage
    state.done = ev.done
    state.total = ev.total
    state.progressMsg = ev.message
}

function onLog(ev) {
    if (!state.taskId || ev.taskId !== state.taskId) return
    state.logs.push({ level: ev.level, msg: ev.msg, ts: Date.now() })
    if (state.logs.length > 500) state.logs.splice(0, state.logs.length - 500)
}

function onDone(ev) {
    if (!state.taskId || ev.taskId !== state.taskId) return
    state.result = ev.result
    state.running = false
}

function onError(ev) {
    if (!state.taskId || ev.taskId !== state.taskId) return
    state.error = { code: ev.code, message: ev.message }
    state.running = false
}

/** 全局订阅一次即可；多次调用幂等。 */
export function installTaskListeners() {
    if (subscribed) return
    EventsOn(EVENT_PROGRESS, onProgress)
    EventsOn(EVENT_LOG, onLog)
    EventsOn(EVENT_DONE, onDone)
    EventsOn(EVENT_ERROR, onError)
    subscribed = true
}

/** 热拔插组件时可用，但 V1.0 一般全程保留。 */
export function uninstallTaskListeners() {
    if (!subscribed) return
    EventsOff(EVENT_PROGRESS)
    EventsOff(EVENT_LOG)
    EventsOff(EVENT_DONE)
    EventsOff(EVENT_ERROR)
    subscribed = false
}

/** 启动新任务前调用：清历史 + 记录 taskId + 标记运行中。 */
export function startTask(taskId) {
    state.taskId = taskId
    state.running = true
    state.stage = 'scanning'
    state.done = 0
    state.total = 0
    state.progressMsg = ''
    state.logs = []
    state.result = null
    state.error = null
}

/** 重置状态（不取消后端，仅前端清空）。 */
export function resetTask() {
    state.taskId = ''
    state.running = false
    state.stage = ''
    state.done = 0
    state.total = 0
    state.progressMsg = ''
    state.logs = []
    state.result = null
    state.error = null
}

export const task = readonly(state)
