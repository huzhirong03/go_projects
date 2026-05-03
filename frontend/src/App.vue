<script setup>
import { ref, onMounted } from 'vue'
import ExtractView from './views/ExtractView.vue'
import SplitView from './views/SplitView.vue'
import Toast from './components/Toast.vue'
import { installTaskListeners, resetTask } from './stores/task.js'
import { resetAllConfig } from './stores/config.js'
import { configPath } from './api/config.js'
import { showToast } from './stores/toast.js'

const activeTab = ref('extract') // extract | split

onMounted(() => {
    installTaskListeners()
})

function switchTab(tab) {
    if (tab === activeTab.value) return
    resetTask() // 切页时清空当前任务状态，避免跨页串扰
    activeTab.value = tab
}

async function showConfigPath() {
    try {
        const p = await configPath()
        showToast('配置文件：' + p, 'info', 6000)
    } catch (e) {
        showToast('获取配置路径失败：' + (e.message || e), 'error')
    }
}

async function resetConfig() {
    if (!confirm('确定要清空所有已保存的配置吗？\n\n注意：当前页面字段不会立即清空，刷新页面后生效。')) return
    try {
        await resetAllConfig()
        showToast('配置已清空，刷新窗口后生效', 'success')
    } catch (e) {
        showToast('清空失败：' + (e.message || e), 'error')
    }
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
            <div class="topbar-spacer"></div>
            <div class="topbar-actions">
                <button class="topbar-btn" type="button" @click="showConfigPath"
                        title="显示当前配置文件位置">配置位置</button>
                <button class="topbar-btn" type="button" @click="resetConfig"
                        title="清空已保存的配置（输入路径、勾选项等）">重置配置</button>
            </div>
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

.topbar-spacer { flex: 1; }
.topbar-actions { display: flex; gap: 6px; }
.topbar-btn {
    padding: 5px 12px;
    background: #1f2937;
    border: 1px solid #334155;
    color: #94a3b8;
    cursor: pointer;
    border-radius: 4px;
    font-size: 12px;
}
.topbar-btn:hover { background: #334155; color: #e2e8f0; }

.main-content {
    flex: 1;
    padding: 20px;
    max-width: 960px;
    margin: 0 auto;
    width: 100%;
    box-sizing: border-box;
}
</style>
