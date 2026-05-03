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
    height: 56px;
    padding: 0 20px;
    background: #111827;
    border-bottom: 1px solid #1f2937;
    display: flex;
    align-items: center;
    gap: 24px;
}
.brand { font-size: 16px; font-weight: 600; color: #f1f5f9; }
.tabs { display: flex; gap: 4px; }
.tab {
    padding: 8px 16px;
    background: transparent;
    border: none;
    color: #94a3b8;
    cursor: pointer;
    border-radius: 6px;
    font-size: 14px;
}
.tab:hover { background: #1f2937; color: #e2e8f0; }
.tab.active { background: #2563eb; color: white; }

.main-content {
    flex: 1;
    padding: 20px;
    width: 100%;
    box-sizing: border-box;
}
</style>
