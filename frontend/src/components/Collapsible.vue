<script setup>
// 通用可折叠区块。基于 <details>/<summary> 原生实现，支持 v-model:open 双向绑定。
import { computed } from 'vue'

const props = defineProps({
    title: { type: String, required: true },
    open: { type: Boolean, default: true },
})
const emit = defineEmits(['update:open'])

const isOpen = computed({
    get: () => props.open,
    set: v => emit('update:open', v),
})

function onToggle(e) {
    isOpen.value = e.target.open
}
</script>

<template>
    <details class="collapsible" :open="isOpen" @toggle="onToggle">
        <summary class="collapsible-head">
            <span class="chevron" aria-hidden="true">▶</span>
            <span class="title">{{ title }}</span>
            <slot name="head-extra" />
        </summary>
        <div class="collapsible-body">
            <slot />
        </div>
    </details>
</template>

<style scoped>
.collapsible {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    overflow: hidden;
    box-shadow: var(--shadow-card);
}
.collapsible-head {
    list-style: none;
    cursor: pointer;
    padding: 8px 12px;
    color: var(--text);
    font-size: 14px;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 8px;
    user-select: none;
    background: var(--surface);
    transition: background var(--t-fast) var(--ease);
}
.collapsible-head::-webkit-details-marker { display: none; }
.collapsible-head:hover { background: var(--surface-hover); }
.collapsible-head:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
}
.chevron {
    display: inline-block;
    transition: transform var(--t-base) var(--ease);
    color: var(--text-tertiary);
    font-size: 9px;
    width: 12px;
}
.collapsible[open] .chevron { transform: rotate(90deg); }
.collapsible-body {
    padding: 10px 12px 12px;
    border-top: 1px solid var(--divider);
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--surface);
}
.title { flex: 1; letter-spacing: -0.005em; }
</style>
