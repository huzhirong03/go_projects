<script setup>
// 文件夹或文件选择器。
//   props.mode = "folder" | "file"
//   props.allowSwitch = true 时显示一个 [📁文件夹|📄文件] 分段控件，
//     允许用户切换源类型；切换会通过 update:mode 事件向上同步。
import { chooseFolder, chooseFile } from '../api/index.js'
import { showToast } from '../stores/toast.js'

const props = defineProps({
    modelValue: { type: String, default: '' },
    mode: { type: String, default: 'folder' }, // folder | file
    placeholder: { type: String, default: '未选择' },
    label: { type: String, default: '' },
    allowSwitch: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue', 'update:mode'])

async function pick() {
    try {
        const p = props.mode === 'file'
            ? await chooseFile(props.label || '选择文件')
            : await chooseFolder(props.label || '选择文件夹')
        if (p) emit('update:modelValue', p)
    } catch (e) {
        console.error(e)
        showToast(e.message || String(e), 'error')
    }
}

function setMode(m) {
    if (m === props.mode) return
    emit('update:mode', m)
    // 切类型时清空旧路径，避免文件路径残留在文件夹模式
    if (props.modelValue) emit('update:modelValue', '')
}
</script>

<template>
    <div class="path-picker">
        <div v-if="label || allowSwitch" class="path-head">
            <label v-if="label" class="path-label">{{ label }}</label>
            <div v-if="allowSwitch" class="seg" role="tablist" aria-label="源类型">
                <button type="button" :class="['seg-btn', { active: mode === 'folder' }]"
                        @click="setMode('folder')" :aria-pressed="mode === 'folder'">
                    <span class="seg-icon">📁</span>文件夹
                </button>
                <button type="button" :class="['seg-btn', { active: mode === 'file' }]"
                        @click="setMode('file')" :aria-pressed="mode === 'file'">
                    <span class="seg-icon">📄</span>单文件
                </button>
            </div>
        </div>
        <div class="path-row">
            <input class="path-input" type="text" :value="modelValue"
                   :placeholder="placeholder"
                   @input="emit('update:modelValue', $event.target.value)" />
            <button class="btn btn-secondary" type="button" @click="pick">
                浏览
            </button>
        </div>
    </div>
</template>

<style scoped>
.path-picker { display: flex; flex-direction: column; gap: 6px; }
.path-head {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    /* 固定高度 = 分段控件的高度，保证左右两列 PathPicker 下方输入框对齐 */
    min-height: 26px;
}
.path-label {
    font-size: 13px;
    color: var(--text);
    font-weight: 600;
}
.path-row { display: flex; gap: 6px; }
.path-input {
    flex: 1;
    font-family: var(--font-mono);
    font-size: 13px;
}

/* 分段控件（Fluent SegmentedControl 简化版） */
.seg {
    display: inline-flex;
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-sm);
    padding: 2px;
    gap: 0;
}
.seg-btn {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--text-secondary);
    font-family: inherit;
    font-size: 12px;
    font-weight: 500;
    padding: 3px 10px;
    border-radius: 3px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    transition: background var(--t-fast) var(--ease),
                color var(--t-fast) var(--ease);
}
.seg-btn:hover { background: var(--surface-hover); color: var(--text); }
.seg-btn.active {
    background: var(--accent-soft);
    color: var(--accent-soft-fg);
    font-weight: 600;
    box-shadow: inset 0 0 0 1px var(--accent);
}
.seg-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
.seg-icon { font-size: 12px; line-height: 1; }
</style>
