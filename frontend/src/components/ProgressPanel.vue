<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { task, resetTask, clearFileBlocked } from '../stores/task.js'
import { cancelTask, openPath, respondFileBlocked } from '../api/index.js'
import { showToast } from '../stores/toast.js'

const percent = computed(() => {
    if (!task.total || task.total <= 0) return 0
    return Math.min(100, Math.round((task.done / task.total) * 100))
})

const statusText = computed(() => {
    if (task.error) return '失败'
    if (task.result) return '完成'
    if (task.running) return '运行中'
    return '空闲'
})

// 计时器：运行中每 500ms 触发一次刷新，让 elapsed 的 computed 重新求值。
const nowTick = ref(Date.now())
let timer = null
watch(() => task.running, (running) => {
    if (running) {
        if (timer) return
        timer = setInterval(() => { nowTick.value = Date.now() }, 500)
    } else {
        if (timer) { clearInterval(timer); timer = null }
        nowTick.value = Date.now()
    }
}, { immediate: true })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })

function fmtElapsed(ms) {
    if (ms <= 0) return ''
    const s = Math.floor(ms / 1000)
    if (s < 60) return s + ' 秒'
    const m = Math.floor(s / 60)
    const rem = s % 60
    return `${m} 分 ${rem} 秒`
}

const elapsedText = computed(() => {
    if (!task.startedAt) return ''
    const end = task.endedAt || nowTick.value
    return fmtElapsed(end - task.startedAt)
})

async function doCancel() {
    if (!task.taskId) return
    await cancelTask(task.taskId)
}

function doReset() {
    resetTask()
}

async function openOutput(path) {
    try {
        await openPath(path)
    } catch (e) {
        showToast('打开失败：' + (e?.message || e), 'error')
    }
}

async function replyFileBlocked(action) {
    if (!task.fileBlocked?.promptId) return
    try {
        const ok = await respondFileBlocked(task.fileBlocked.promptId, action)
        if (!ok) showToast('操作已失效，请等待任务状态刷新', 'warn')
        clearFileBlocked()
    } catch (e) {
        showToast('操作失败：' + (e?.message || e), 'error')
    }
}
</script>

<template>
    <div class="progress-panel">
        <div class="progress-header">
            <div class="status-badge" :class="{
                running: task.running, done: !!task.result, error: !!task.error,
            }">
                {{ statusText }}
            </div>
            <div class="progress-stage" v-if="task.stage">{{ task.stage }}</div>
            <div class="progress-msg" v-if="task.progressMsg" :title="task.progressMsg">
                {{ task.progressMsg }}
            </div>
            <div class="progress-pct" v-if="task.running">{{ percent }}%</div>
            <div v-if="elapsedText" class="progress-elapsed" :title="task.running ? '已用时' : '总耗时'">
                ⏱ {{ elapsedText }}
            </div>
        </div>

        <div class="progress-bar" v-if="task.running">
            <div class="progress-fill" :style="{ width: percent + '%' }"></div>
        </div>

        <!-- 日志窗口 -->
        <div class="log-window" v-if="task.logs.length">
            <div v-for="(l, i) in task.logs" :key="i" :class="['log-line', 'lv-' + l.level]">
                <span class="log-msg">{{ l.msg }}</span>
            </div>
        </div>

        <!-- 错误 -->
        <div class="result error-box" v-if="task.error">
            <div class="result-title">执行出错</div>
            <div class="result-row"><b>代码：</b>{{ task.error.code || '(无)' }}</div>
            <div class="result-row"><b>信息：</b>{{ task.error.message }}</div>
        </div>

        <!-- 完成汇总 -->
        <div class="result success-box" v-if="task.result">
            <div class="result-title">完成</div>
            <div class="result-row" v-if="task.result.RowsMatched !== undefined">
                <b>命中行：</b>{{ task.result.RowsMatched }}
            </div>
            <div class="result-row" v-if="task.result.ImagesMigrated !== undefined">
                <b>迁移图片：</b>{{ task.result.ImagesMigrated }}
            </div>
            <div class="result-row" v-if="task.result.RowsScanned !== undefined">
                <b>扫描行：</b>{{ task.result.RowsScanned }}
            </div>
            <div class="result-row" v-if="task.result.PartsCreated !== undefined">
                <b>分片数：</b>{{ task.result.PartsCreated }}
            </div>
            <div class="result-row" v-if="task.result.OutputFiles && task.result.OutputFiles.length">
                <b>输出文件（{{ task.result.OutputFiles.length }} 个）：</b>
                <div class="output-list">
                    <div v-for="p in task.result.OutputFiles" :key="p" class="output-item">
                        <span class="output-path">{{ p }}</span>
                        <button class="btn btn-link" @click="openOutput(p)">打开</button>
                    </div>
                </div>
            </div>
        </div>

        <!-- 动作按钮 -->
        <div class="progress-actions">
            <button v-if="task.running" class="btn btn-danger" @click="doCancel">取消</button>
            <button v-if="!task.running && (task.result || task.error)" class="btn btn-secondary" @click="doReset">
                清空
            </button>
        </div>

        <div v-if="task.fileBlocked" class="modal-mask">
            <div class="file-blocked-dialog">
                <div class="dialog-title">文件正在被占用</div>
                <div class="dialog-text">
                    检测到文件暂时无法读取，可能正在被 Excel/WPS 打开。
                    请先保存并关闭该文件，然后点击“重试”继续。
                </div>
                <div class="blocked-path">{{ task.fileBlocked.path }}</div>
                <div class="dialog-detail">{{ task.fileBlocked.message }}</div>
                <div class="dialog-actions">
                    <button class="btn btn-primary" @click="replyFileBlocked('retry')">重试</button>
                    <button class="btn btn-secondary" @click="replyFileBlocked('skip')">跳过</button>
                    <button class="btn btn-danger" @click="replyFileBlocked('cancel')">取消任务</button>
                </div>
            </div>
        </div>
    </div>
</template>

<style scoped>
.progress-panel {
    background: #1f2738;
    border: 1px solid #2d3748;
    border-radius: 8px;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
}
.progress-header { display: flex; align-items: center; gap: 10px; }
.status-badge {
    font-size: 12px;
    padding: 2px 10px;
    border-radius: 12px;
    background: #334155;
    color: #cbd5e1;
}
.status-badge.running { background: #2563eb; color: white; }
.status-badge.done { background: #16a34a; color: white; }
.status-badge.error { background: #dc2626; color: white; }
.progress-stage { font-size: 13px; color: #94a3b8; }
.progress-pct { margin-left: auto; font-weight: 600; color: #e2e8f0; }
.progress-elapsed {
    font-size: 12px;
    color: #cbd5e1;
    background: #0f172a;
    padding: 2px 8px;
    border-radius: 10px;
    white-space: nowrap;
}
.progress-bar {
    width: 100%;
    height: 8px;
    background: #0f172a;
    border-radius: 4px;
    overflow: hidden;
}
.progress-fill {
    height: 100%;
    background: linear-gradient(90deg, #3b82f6, #06b6d4);
    transition: width 0.2s ease;
}
.progress-msg {
    font-size: 12px;
    color: #94a3b8;
    font-family: monospace;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.log-window {
    max-height: 200px;
    overflow-y: auto;
    background: #0f172a;
    border-radius: 6px;
    padding: 8px;
    font-family: Consolas, monospace;
    font-size: 12px;
}
.log-line { padding: 2px 4px; word-break: break-all; }
.log-line.lv-warn { color: #fbbf24; }
.log-line.lv-error { color: #f87171; }
.log-line.lv-info { color: #cbd5e1; }

.result {
    border-radius: 6px;
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
}
.success-box { background: #052e1a; border: 1px solid #16a34a; }
.error-box { background: #450a0a; border: 1px solid #dc2626; }
.result-title { font-weight: 600; color: #e2e8f0; }
.result-row { font-size: 13px; color: #cbd5e1; }
.output-list { margin-top: 4px; display: flex; flex-direction: column; gap: 2px; }
.output-item { display: flex; gap: 8px; align-items: center; }
.output-path { flex: 1; font-family: monospace; font-size: 12px; color: #93c5fd; word-break: break-all; }
.progress-actions { display: flex; gap: 8px; justify-content: flex-end; }
.modal-mask {
    position: fixed;
    inset: 0;
    z-index: 50;
    background: rgba(15, 23, 42, 0.72);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
}
.file-blocked-dialog {
    width: min(560px, 100%);
    background: #1f2738;
    border: 1px solid #f59e0b;
    border-radius: 10px;
    padding: 18px;
    box-shadow: 0 20px 60px rgba(0,0,0,.35);
    display: flex;
    flex-direction: column;
    gap: 12px;
}
.dialog-title { font-size: 16px; font-weight: 700; color: #fde68a; }
.dialog-text { color: #e2e8f0; font-size: 13px; line-height: 1.6; }
.blocked-path {
    background: #0f172a;
    border-radius: 6px;
    padding: 8px;
    color: #93c5fd;
    font-size: 12px;
    font-family: Consolas, monospace;
    word-break: break-all;
}
.dialog-detail {
    max-height: 120px;
    overflow: auto;
    color: #94a3b8;
    font-size: 12px;
    white-space: pre-wrap;
}
.dialog-actions { display: flex; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
</style>
