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
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    box-shadow: var(--shadow-card);
}
.progress-header { display: flex; align-items: center; gap: 10px; }

/* 状态徽章：Fluent 软色底 + 强色边，不是实色十里外 */
.status-badge {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: var(--r-sm);
    background: var(--surface-hover);
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    border: 1px solid var(--border);
}
.status-badge.running {
    background: var(--info-soft); color: var(--info); border-color: var(--info);
}
.status-badge.done {
    background: var(--success-soft); color: var(--success); border-color: var(--success);
}
.status-badge.error {
    background: var(--danger-soft); color: var(--danger); border-color: var(--danger);
}
.progress-stage { font-size: 13px; color: var(--text-secondary); font-weight: 500; }
.progress-pct {
    margin-left: auto;
    font-weight: 600;
    color: var(--text);
    font-variant-numeric: tabular-nums;
}
.progress-elapsed {
    font-size: 12px;
    color: var(--text-secondary);
    background: var(--surface-2);
    border: 1px solid var(--border);
    padding: 2px 8px;
    border-radius: 10px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
}
.progress-bar {
    width: 100%;
    height: 4px;
    background: var(--surface-hover);
    border-radius: 2px;
    overflow: hidden;
}
.progress-fill {
    height: 100%;
    background: var(--accent);
    transition: width 0.2s var(--ease);
}
.progress-msg {
    font-size: 12px;
    color: var(--text-tertiary);
    font-family: var(--font-mono);
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.log-window {
    max-height: 220px;
    overflow-y: auto;
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.55;
}
.log-line { padding: 1px 0; word-break: break-all; color: var(--text); }
.log-line.lv-warn { color: var(--warn); }
.log-line.lv-error { color: var(--danger); }
.log-line.lv-info { color: var(--text-secondary); }

.result {
    border-radius: var(--r-sm);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
}
.success-box {
    background: var(--success-soft);
    border: 1px solid var(--success);
}
.error-box {
    background: var(--danger-soft);
    border: 1px solid var(--danger);
}
.result-title { font-weight: 600; color: var(--text); }
.result-row { font-size: 13px; color: var(--text); }
.result-row b { font-weight: 600; color: var(--text-secondary); }
.output-list { margin-top: 4px; display: flex; flex-direction: column; gap: 2px; }
.output-item { display: flex; gap: 8px; align-items: center; }
.output-path {
    flex: 1;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--info);
    word-break: break-all;
}
.progress-actions { display: flex; gap: 8px; justify-content: flex-end; }

.modal-mask {
    position: fixed;
    inset: 0;
    z-index: var(--z-modal);
    background: rgba(0, 0, 0, 0.32);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
    backdrop-filter: blur(2px);
}
.file-blocked-dialog {
    width: min(560px, 100%);
    background: var(--surface);
    border: 1px solid var(--border);
    border-top: 3px solid var(--warn);
    border-radius: var(--r-lg);
    padding: 20px;
    box-shadow: var(--shadow-flyout);
    display: flex;
    flex-direction: column;
    gap: 12px;
}
.dialog-title {
    font-family: var(--font-display);
    font-size: 16px;
    font-weight: 600;
    color: var(--text);
    letter-spacing: -0.01em;
}
.dialog-text { color: var(--text); font-size: 13px; line-height: 1.55; }
.blocked-path {
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    padding: 8px 10px;
    color: var(--text);
    font-size: 12px;
    font-family: var(--font-mono);
    word-break: break-all;
}
.dialog-detail {
    max-height: 120px;
    overflow: auto;
    color: var(--text-tertiary);
    font-size: 12px;
    white-space: pre-wrap;
}
.dialog-actions { display: flex; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
</style>
