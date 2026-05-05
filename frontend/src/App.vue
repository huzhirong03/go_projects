<script setup>
import { ref, onMounted } from 'vue'
import ExtractView from './views/ExtractView.vue'
import SplitView from './views/SplitView.vue'
import Toast from './components/Toast.vue'
import { installTaskListeners, resetTask } from './stores/task.js'
import { restoreWindowState, installWindowStateSaver } from './stores/window.js'
import { LogPrint } from '../wailsjs/runtime/runtime'
import { LogStartup } from '../wailsjs/go/main/App'
import { openLogFolder } from './api/logs.js'
import { showToast } from './stores/toast.js'

// timing 双写：前端 console + 后端 startup.log
function logT(msg) {
    LogPrint(`[STARTUP-FE] ${msg}`)
    LogStartup(msg).catch(() => {}) // App binding 还没就绪时安全跳过
}

// 启动 timing：脚本加载到这里花了多久
const _feT0 = performance.now()
logT(`App.vue script setup at ${_feT0.toFixed(0)}ms (since page navigation)`)

const activeTab = ref('extract') // extract | split

onMounted(() => {
    const t1 = performance.now()
    logT(`App.vue onMounted at ${t1.toFixed(0)}ms (delta-from-setup ${(t1 - _feT0).toFixed(0)}ms)`)
    installTaskListeners()
    const tWin = performance.now()
    restoreWindowState().finally(() => {
        logT(`restoreWindowState (LoadConfig + WindowSetSize) took ${(performance.now() - tWin).toFixed(0)}ms`)
        installWindowStateSaver()
    })
})

function switchTab(tab) {
    if (tab === activeTab.value) return
    resetTask() // 切页时清空当前任务状态，避免跨页串扰
    activeTab.value = tab
}

// 打开日志文件夹：学员遇到 bug 时点这里把 task-*.log 发给开发者排查
async function handleOpenLogFolder() {
    try {
        await openLogFolder()
    } catch (e) {
        showToast(`打开日志文件夹失败：${e?.message || e}`, 'error')
    }
}
</script>

<template>
    <div class="app">
        <header class="topbar">
            <div class="brand">Excel 拆合大师 v1.5.1 · 大荣老师出品</div>
            <nav class="tabs">
                <button :class="['tab', { active: activeTab === 'extract' }]"
                        @click="switchTab('extract')">批量提取</button>
                <button :class="['tab', { active: activeTab === 'split' }]"
                        @click="switchTab('split')">单文件拆分</button>
            </nav>
            <div class="topbar-spacer"></div>
            <button class="topbar-action"
                    title="打开日志文件夹（遇到问题时把最新的 task-*.log 发给开发者）"
                    @click="handleOpenLogFolder">📂 日志</button>
        </header>
        <main class="main-content">
            <ExtractView v-if="activeTab === 'extract'" />
            <SplitView v-else-if="activeTab === 'split'" />
        </main>
        <Toast />
    </div>
</template>

<style scoped>
.app { display: flex; flex-direction: column; min-height: 100vh; }

.topbar {
    height: 48px;
    padding: 0 20px;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: 20px;
    box-shadow: var(--shadow-card);
    position: sticky;
    top: 0;
    z-index: var(--z-sticky);
}
.brand {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    font-weight: 600;
    color: var(--text);
    letter-spacing: -0.01em;
}
.brand::before {
    /* Excel 绿小色块作 logo */
    content: "";
    display: inline-block;
    width: 16px;
    height: 16px;
    background: var(--accent);
    border-radius: var(--r-xs);
    box-shadow: inset 0 0 0 1px rgba(0,0,0,0.06);
}

.tabs { display: flex; gap: 2px; }
.topbar-spacer { flex: 1; } /* 把日志按钮顶到最右 */
.topbar-action {
    height: 30px;
    padding: 0 12px;
    background: var(--surface-hover, transparent);
    color: var(--text-secondary);
    border: 1px solid var(--border);
    border-radius: var(--r-sm, 6px);
    cursor: pointer;
    font-family: inherit;
    font-size: 13px;
    line-height: 30px;
    transition: color var(--t-fast) var(--ease), background var(--t-fast) var(--ease), border-color var(--t-fast) var(--ease);
}
.topbar-action:hover { color: var(--text); background: var(--surface); border-color: var(--accent); }

.tab {
    position: relative;
    height: 48px;
    padding: 0 14px;
    background: transparent;
    border: none;
    color: var(--text-secondary);
    cursor: pointer;
    font-family: inherit;
    font-size: 14px;
    font-weight: 500;
    transition: color var(--t-fast) var(--ease), background var(--t-fast) var(--ease);
}
.tab:hover { color: var(--text); background: var(--surface-hover); }
.tab.active { color: var(--text); font-weight: 600; }
.tab.active::after {
    /* Fluent 风格：底部 accent 短线指示 */
    content: "";
    position: absolute;
    left: 14px; right: 14px; bottom: 0;
    height: 2px;
    background: var(--accent);
    border-radius: 2px 2px 0 0;
}

.main-content {
    flex: 1;
    padding: 14px 18px;
    width: 100%;
    max-width: 1280px;
    margin: 0 auto;
    box-sizing: border-box;
}
</style>
