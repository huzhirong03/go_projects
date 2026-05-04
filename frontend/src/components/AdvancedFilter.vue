<script setup>
// 高级筛选编辑器组件。
//
// Props:
//   - modelValue: { mode: 'all'|'any', conditions: [{column, op, value, value2, format}] }
//   - columns:    可选的列名数组（从父级 previewState 传入）
// Emits:
//   - update:modelValue: 当用户增/删/改条件时
//
// 使用方：父级把它放在一个折叠面板里，自己处理面板的开合状态 + 角标显示。
// 这个组件只负责"条件编辑 UI"。

import { computed } from 'vue'
import {
    FILTER_MODE_ALL, FILTER_MODE_ANY,
    OP_VALUE_KIND, OPERATOR_OPTIONS, FORMAT_OPTIONS,
    newCondition, isMeaningfulCondition,
} from '../types/filter.js'

const props = defineProps({
    modelValue: {
        type: Object,
        required: true,
    },
    columns: {
        type: Array,
        default: () => [],
    },
})
const emit = defineEmits(['update:modelValue'])

// 计算属性：把 modelValue 当 reactive 用，emit 出深拷贝避免父级直接持有引用产生 lint 警告。
const spec = computed({
    get() {
        return props.modelValue || { mode: FILTER_MODE_ALL, conditions: [] }
    },
    set(v) {
        emit('update:modelValue', v)
    },
})

function setMode(m) {
    emit('update:modelValue', { ...spec.value, mode: m })
}

function addCondition() {
    const newSpec = {
        ...spec.value,
        conditions: [...(spec.value.conditions || []), newCondition()],
    }
    emit('update:modelValue', newSpec)
}

function removeCondition(idx) {
    const next = (spec.value.conditions || []).filter((_, i) => i !== idx)
    emit('update:modelValue', { ...spec.value, conditions: next })
}

function updateCondition(idx, patch) {
    const next = [...(spec.value.conditions || [])]
    next[idx] = { ...next[idx], ...patch }
    emit('update:modelValue', { ...spec.value, conditions: next })
}

function clearAll() {
    emit('update:modelValue', { mode: FILTER_MODE_ALL, conditions: [] })
}

// 操作符变更时，重置 value/value2/format 避免残留
function onOpChange(idx, newOp) {
    const cur = spec.value.conditions[idx]
    updateCondition(idx, { op: newOp, value: '', value2: '', format: cur.format || '' })
}

// 操作符按 group 分组（同 group 在 <optgroup> 里）
const groupedOperators = computed(() => {
    const groups = {}
    for (const opt of OPERATOR_OPTIONS) {
        if (!groups[opt.group]) groups[opt.group] = []
        groups[opt.group].push(opt)
    }
    return groups
})

// 暴露给父组件用：当前有效条件数（角标用）
defineExpose({
    countActive: () => (spec.value.conditions || []).filter(isMeaningfulCondition).length,
})

// 当前操作符的"值形态"
function valueKind(c) { return OP_VALUE_KIND[c.op] || 'single' }

// 返回当前选中操作符的 hint，用于在条件行下方展示说明。
function currentOpHint(c) {
    const op = OPERATOR_OPTIONS.find(o => o.value === c.op)
    return op ? op.hint : ''
}
</script>

<template>
    <div class="adv-filter">
        <div class="adv-filter-header">
            <div class="match-mode">
                <span class="field-label">匹配模式：</span>
                <label>
                    <input type="radio" :value="FILTER_MODE_ALL" :checked="spec.mode === FILTER_MODE_ALL"
                           @change="setMode(FILTER_MODE_ALL)" />
                    全部满足
                </label>
                <label>
                    <input type="radio" :value="FILTER_MODE_ANY" :checked="spec.mode === FILTER_MODE_ANY"
                           @change="setMode(FILTER_MODE_ANY)" />
                    任一满足
                </label>
            </div>
            <button v-if="(spec.conditions || []).length > 0" type="button"
                    class="btn-clear" @click="clearAll">清空</button>
        </div>

        <div class="cond-list">
            <template v-for="(c, i) in spec.conditions" :key="i">
            <div class="cond-row">
                <!-- 列名：优先下拉，回退手动输入 -->
                <input v-if="columns.length === 0" type="text" class="col-input"
                       placeholder="列名" :value="c.column"
                       @input="updateCondition(i, { column: $event.target.value })" />
                <select v-else class="col-input" :value="c.column"
                        @change="updateCondition(i, { column: $event.target.value })">
                    <option value="">— 选择列 —</option>
                    <option v-for="col in columns" :key="col" :value="col">{{ col }}</option>
                </select>

                <!-- 操作符（每个 option 带 title 原生 tooltip：hover ~1s 弹出说明） -->
                <select class="op-input" :value="c.op"
                        @change="onOpChange(i, $event.target.value)"
                        :title="currentOpHint(c)">
                    <optgroup v-for="(opts, group) in groupedOperators" :key="group" :label="group">
                        <option v-for="o in opts" :key="o.value" :value="o.value" :title="o.hint">{{ o.label }}</option>
                    </optgroup>
                </select>

                <!-- 值的形态根据 op 切换 -->
                <template v-if="valueKind(c) === 'single'">
                    <input type="text" class="val-input" placeholder="值" :value="c.value"
                           @input="updateCondition(i, { value: $event.target.value })" />
                </template>
                <template v-else-if="valueKind(c) === 'double'">
                    <input type="text" class="val-input val-half" placeholder="最小值" :value="c.value"
                           @input="updateCondition(i, { value: $event.target.value })" />
                    <span class="val-sep">~</span>
                    <input type="text" class="val-input val-half" placeholder="最大值" :value="c.value2"
                           @input="updateCondition(i, { value2: $event.target.value })" />
                </template>
                <template v-else-if="valueKind(c) === 'date_double'">
                    <input type="date" class="val-input val-half" :value="c.value"
                           @input="updateCondition(i, { value: $event.target.value })" />
                    <span class="val-sep">~</span>
                    <input type="date" class="val-input val-half" :value="c.value2"
                           @input="updateCondition(i, { value2: $event.target.value })" />
                </template>
                <template v-else-if="valueKind(c) === 'list'">
                    <input type="text" class="val-input val-list" placeholder="多个值，用逗号分隔" :value="c.value"
                           @input="updateCondition(i, { value: $event.target.value })" />
                </template>
                <template v-else-if="valueKind(c) === 'format'">
                    <select class="val-input val-format" :value="c.format"
                            @change="updateCondition(i, { format: $event.target.value })">
                        <option value="">— 选择格式 —</option>
                        <option v-for="o in FORMAT_OPTIONS" :key="o.value" :value="o.value" :title="o.hint">{{ o.label }}</option>
                    </select>
                </template>
                <template v-else-if="valueKind(c) === 'none'">
                    <span class="val-placeholder">（无需填值）</span>
                </template>

                <button type="button" class="btn-del" @click="removeCondition(i)" title="删除此条件">✕</button>
            </div>
            <!-- 当前 op 说明行：永久可见，不依赖 hover，与下拉 tooltip 双保险 -->
            <div v-if="currentOpHint(c)" class="cond-hint">⤷ {{ currentOpHint(c) }}</div>
            </template>

            <button type="button" class="btn-add" @click="addCondition">+ 添加条件</button>
        </div>

        <div v-if="(spec.conditions || []).length === 0" class="adv-filter-empty">
            还没添加任何条件。条件为空时，等同于不启用高级筛选；旧的关键词逻辑不变。
        </div>
    </div>
</template>

<style scoped>
.adv-filter {
    padding: 4px 0;
}

.adv-filter-header {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 4px 0 10px;
    border-bottom: 1px dashed var(--border, #e0e0e0);
    margin-bottom: 10px;
}
.match-mode {
    display: flex;
    align-items: center;
    gap: 12px;
}
.match-mode label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
}
.btn-clear {
    margin-left: auto;
    padding: 4px 10px;
    font-size: 12px;
    background: transparent;
    color: var(--accent, #0078d4);
    border: 1px solid var(--accent, #0078d4);
    border-radius: 4px;
    cursor: pointer;
}
.btn-clear:hover {
    background: var(--accent-hover-bg, #e6f2fb);
}

.cond-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
}
.cond-hint {
    /* 当前操作符的永久可见说明：缩进对齐操作符位置，淺色辅助字 */
    padding: 0 0 4px 130px;
    margin-top: -2px;
    font-size: 12px;
    color: var(--text-secondary);
    line-height: 1.4;
    user-select: none;
}
.cond-row {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
}
.col-input {
    min-width: 130px;
    flex: 0 0 auto;
}
.op-input {
    min-width: 130px;
    flex: 0 0 auto;
}
.val-input {
    flex: 1 1 180px;
    min-width: 120px;
}
.val-half {
    flex: 1 1 110px;
    min-width: 80px;
}
.val-list { flex: 1 1 240px; }
.val-format { flex: 1 1 200px; }
.val-sep {
    color: var(--text-secondary, #666);
    padding: 0 4px;
}
.val-placeholder {
    flex: 1 1 180px;
    color: var(--text-secondary, #888);
    font-size: 13px;
    font-style: italic;
}

.btn-del {
    flex: 0 0 auto;
    width: 28px;
    height: 28px;
    padding: 0;
    background: transparent;
    color: var(--text-secondary, #999);
    border: 1px solid var(--border, #ddd);
    border-radius: 4px;
    cursor: pointer;
    line-height: 1;
}
.btn-del:hover {
    color: var(--danger, #c43f3f);
    border-color: var(--danger, #c43f3f);
}

.btn-add {
    align-self: flex-start;
    margin-top: 6px;
    padding: 6px 14px;
    background: transparent;
    color: var(--accent, #0078d4);
    border: 1px dashed var(--accent, #0078d4);
    border-radius: 4px;
    cursor: pointer;
    font-size: 13px;
}
.btn-add:hover {
    background: var(--accent-hover-bg, #e6f2fb);
}

.adv-filter-empty {
    margin-top: 8px;
    padding: 8px 10px;
    font-size: 12px;
    color: var(--text-secondary, #888);
    background: var(--strip-bg, #fafafa);
    border: 1px dashed var(--border, #e0e0e0);
    border-radius: 4px;
}
</style>
