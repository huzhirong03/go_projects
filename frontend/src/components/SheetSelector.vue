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
            <label class="field-label">{{ label }}</label>
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
        <div class="hint">{{ hintAll }}</div>
    </div>
</template>

<style scoped>
.sheet-selector { display: flex; flex-direction: column; gap: 8px; }

.single {
    background: #0f172a;
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px dashed #334155;
}
.single-text { font-size: 13px; color: #94a3b8; }
.single-text b { color: #cbd5e1; }

.multi .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px;
}
.field-label { font-size: 13px; color: #a9b4c6; font-weight: 500; }
.actions { display: flex; gap: 6px; align-items: center; }
.btn-mini {
    background: #334155;
    color: #e2e8f0;
    border: none;
    border-radius: 4px;
    padding: 3px 10px;
    font-size: 12px;
    cursor: pointer;
}
.btn-mini:hover { background: #475569; }
.counter { font-size: 12px; color: #64748b; padding-left: 6px; }

.sheet-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    background: #0f172a;
    padding: 10px;
    border-radius: 6px;
    max-height: 140px;
    overflow-y: auto;
}
.chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    background: #1e293b;
    border-radius: 14px;
    font-size: 12px;
    color: #94a3b8;
    cursor: pointer;
    border: 1px solid transparent;
    transition: all 0.15s ease;
}
.chip:hover { background: #334155; color: #e2e8f0; }
.chip.active {
    background: #1e3a8a;
    color: #e2e8f0;
    border-color: #3b82f6;
}
.chip-name { white-space: nowrap; }

.hint { font-size: 12px; color: #64748b; }
</style>
