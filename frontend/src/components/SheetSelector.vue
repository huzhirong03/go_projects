<script setup>
// Sheet 多选组件：
// - 当 sheets 长度 == 0：完全不渲染（让父组件控制留白）
// - 当 sheets 长度 == 1：渲染只读说明文字（无勾选 UI），保持 modelValue 同步
// - 当 sheets 长度 > 1：渲染勾选列表 + 全选/反选
// modelValue 是 string[] —— 当前勾选的 sheet 名。
import { computed, watch } from 'vue'

const props = defineProps({
    modelValue: { type: Array, default: () => [] },
    sheets: { type: Array, default: () => [] },
    label: { type: String, default: '参与处理的 Sheet' },
    hintAll: { type: String, default: '默认全部勾选；勾掉某个 Sheet 即跳过它。' },
})

const emit = defineEmits(['update:modelValue'])

const isMulti = computed(() => props.sheets.length > 1)

// 当 sheets 列表变化（用户重选了文件/文件夹），自动把 modelValue 重置为"全选"。
watch(() => props.sheets, (newSheets) => {
    if (newSheets.length === 0) {
        if (props.modelValue.length) emit('update:modelValue', [])
        return
    }
    // 默认全选
    emit('update:modelValue', [...newSheets])
}, { immediate: true })

function toggle(name) {
    const cur = props.modelValue
    const i = cur.indexOf(name)
    if (i >= 0) {
        emit('update:modelValue', cur.filter(x => x !== name))
    } else {
        emit('update:modelValue', [...cur, name])
    }
}

function selectAll() {
    emit('update:modelValue', [...props.sheets])
}

function selectNone() {
    emit('update:modelValue', [])
}

function isChecked(name) {
    return props.modelValue.includes(name)
}
</script>

<template>
    <!-- 单 Sheet：极简提示 -->
    <div v-if="sheets.length === 1" class="sheet-selector single">
        <span class="single-text">📄 仅 1 个 Sheet：<b>{{ sheets[0] }}</b></span>
    </div>

    <!-- 多 Sheet：完整 UI -->
    <div v-else-if="isMulti" class="sheet-selector multi">
        <div class="header">
            <div class="label-row">
                <label class="field-label">{{ label }}</label>
                <span class="hint">{{ hintAll }}</span>
            </div>
            <div class="actions">
                <button class="btn-mini" type="button" @click="selectAll">全选</button>
                <button class="btn-mini" type="button" @click="selectNone">反选</button>
                <span class="counter">{{ modelValue.length }}/{{ sheets.length }}</span>
            </div>
        </div>
        <div class="sheet-chips">
            <label v-for="s in sheets" :key="s"
                   :class="['chip', { active: isChecked(s) }]">
                <input type="checkbox" :checked="isChecked(s)" @change="toggle(s)" />
                <span class="chip-name">{{ s }}</span>
            </label>
        </div>
    </div>
</template>

<style scoped>
.sheet-selector { display: flex; flex-direction: column; gap: 8px; }

.single {
    background: var(--surface-2);
    padding: 8px 12px;
    border-radius: var(--r-sm);
    border: 1px dashed var(--border-strong);
}
.single-text { font-size: 13px; color: var(--text-secondary); }
.single-text b { color: var(--text); font-weight: 600; }

.multi .header {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
}
.field-label { font-size: 13px; color: var(--text); font-weight: 600; }
.label-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.actions { display: flex; gap: 6px; align-items: center; }
.btn-mini {
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-sm);
    padding: 2px 10px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: background var(--t-fast) var(--ease);
}
.btn-mini:hover { background: var(--surface-hover); }
.btn-mini:active { background: var(--surface-pressed); }
.counter {
    font-size: 12px;
    color: var(--text-tertiary);
    padding-left: 6px;
    font-variant-numeric: tabular-nums;
}

.sheet-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    background: var(--surface-2);
    border: 1px solid var(--border);
    padding: 10px;
    border-radius: var(--r-sm);
    max-height: 140px;
    overflow-y: auto;
}
.chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: 12px;
    font-size: 12px;
    color: var(--text-secondary);
    cursor: pointer;
    transition: background var(--t-fast) var(--ease),
                border-color var(--t-fast) var(--ease),
                color var(--t-fast) var(--ease);
}
.chip:hover { background: var(--surface-hover); color: var(--text); }
.chip.active {
    background: var(--accent-soft);
    color: var(--accent-soft-fg);
    border-color: var(--accent);
    font-weight: 600;
}
.chip-name { white-space: nowrap; }

.hint { font-size: 12px; color: var(--text-tertiary); }
</style>
