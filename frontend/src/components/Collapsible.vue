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
    background: #1f2738;
    border: 1px solid #2d3748;
    border-radius: 8px;
    overflow: hidden;
}
.collapsible-head {
    list-style: none;
    cursor: pointer;
    padding: 10px 14px;
    color: #e2e8f0;
    font-size: 14px;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 8px;
    user-select: none;
    background: #1f2738;
}
.collapsible-head::-webkit-details-marker { display: none; }
.collapsible-head:hover { background: #283045; }
.chevron {
    display: inline-block;
    transition: transform 0.15s ease;
    color: #94a3b8;
    font-size: 10px;
    width: 12px;
}
.collapsible[open] .chevron { transform: rotate(90deg); }
.collapsible-body {
    padding: 12px 14px 14px;
    border-top: 1px solid #2d3748;
    display: flex;
    flex-direction: column;
    gap: 12px;
}
.title { flex: 1; }
</style>
