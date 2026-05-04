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
/*
 * 外壳：更大圆角 + 更明显阴影 + 左侧 3px accent 竖条
 * 竖条用 box-shadow inset 实现，不占布局尺寸；折叠时灰、展开时绿。
 */
.collapsible {
    background: var(--surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-lg);
    overflow: hidden;
    /* 折叠态：4px 灰竖条 + 普通阴影 */
    box-shadow: var(--shadow-card), inset 4px 0 0 #9fb09f;
    transition: box-shadow var(--t-fast) var(--ease), border-color var(--t-fast) var(--ease);
}
.collapsible:hover {
    border-color: #8ea18e;
}
.collapsible[open] {
    /* 展开态：4px accent 竖条 + 更强阴影 */
    box-shadow: var(--shadow-elev), inset 4px 0 0 var(--accent);
    border-color: #8ea18e;
}

/*
 * 头部：直接用更深的薄荷灰绿做底色，跟页面 bg (#eef3ee) 拉出明显明度差。
 * 色值就地写：避免污染全局 --surface-hover（按钮 / 其他组件 hover 态也在用）。
 *   折叠：#c9d4c9（比 bg 深 ~6 级灰阶）
 *   hover：#b8c4b8（再深一档）
 *   展开：#bfcbbf（比折叠略深，跟 body 白底形成"header 深 / body 浅"的二档对比）
 */
.collapsible-head {
    list-style: none;
    cursor: pointer;
    padding: 11px 14px 11px 18px;
    color: var(--text);
    font-size: 14px;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 10px;
    user-select: none;
    background: #c9d4c9;
    transition: background var(--t-fast) var(--ease);
}
.collapsible-head::-webkit-details-marker { display: none; }
.collapsible-head:hover { background: #b8c4b8; }
.collapsible[open] > .collapsible-head {
    background: #bfcbbf;
    border-bottom: 1px solid #9fb09f;
}
.collapsible-head:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
}

.chevron {
    display: inline-block;
    transition: transform var(--t-base) var(--ease), color var(--t-fast) var(--ease);
    color: var(--text-secondary);
    font-size: 10px;
    width: 12px;
}
.collapsible[open] .chevron {
    transform: rotate(90deg);
    color: var(--accent);
}

.collapsible-body {
    padding: 12px 14px 14px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--surface);
}
.title {
    flex: 1;
    letter-spacing: -0.005em;
    font-size: 14px;
}
</style>
