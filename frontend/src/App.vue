<script setup>
import { ref, onMounted } from 'vue'
import ExtractView from './views/ExtractView.vue'
import SplitView from './views/SplitView.vue'
import Toast from './components/Toast.vue'
import { installTaskListeners, resetTask } from './stores/task.js'
import { restoreWindowState, installWindowStateSaver } from './stores/window.js'

const activeTab = ref('extract') // extract | split

onMounted(() => {
    installTaskListeners()
    // 窗口状态恢复走异步、不阻塞首屏；恢复完再装 resize 监听，避免恢复瞬间触发"假"保存
    restoreWindowState().finally(() => installWindowStateSaver())
})

function switchTab(tab) {
    if (tab === activeTab.value) return
    resetTask() // 切页时清空当前任务状态，避免跨页串扰
    activeTab.value = tab
}
</script>

<template>
    <div class="app">
        <header class="topbar">
            <div class="brand">Excel 大文件工具</div>
            <nav class="tabs">
                <button :class="['tab', { active: activeTab === 'extract' }]"
                        @click="switchTab('extract')">批量提取</button>
                <button :class="['tab', { active: activeTab === 'split' }]"
                        @click="switchTab('split')">单文件拆分</button>
            </nav>
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
