<script setup>
import { toastState, dismissToast } from '../stores/toast.js'
</script>

<template>
    <div class="toast-container" aria-live="polite">
        <transition-group name="toast-slide">
            <div
                v-for="t in toastState.items"
                :key="t.id"
                :class="['toast', 'toast-' + t.level]"
                @click="dismissToast(t.id)"
            >
                <span class="toast-icon" v-if="t.level === 'success'">✓</span>
                <span class="toast-icon" v-else-if="t.level === 'warn'">!</span>
                <span class="toast-icon" v-else-if="t.level === 'error'">✕</span>
                <span class="toast-icon" v-else>i</span>
                <span class="toast-msg">{{ t.msg }}</span>
                <button class="toast-close" @click.stop="dismissToast(t.id)">×</button>
            </div>
        </transition-group>
    </div>
</template>

<style scoped>
.toast-container {
    position: fixed;
    top: 16px;
    right: 16px;
    z-index: var(--z-toast);
    display: flex;
    flex-direction: column;
    gap: 8px;
    pointer-events: none;
}
.toast {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 260px;
    max-width: 420px;
    padding: 10px 14px;
    border-radius: var(--r-md);
    border: 1px solid var(--border);
    border-left: 3px solid var(--text-tertiary);
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--shadow-flyout);
    pointer-events: auto;
    cursor: pointer;
    font-size: 13px;
    line-height: 1.45;
}
.toast-info    { border-left-color: var(--info); }
.toast-success { border-left-color: var(--success); }
.toast-warn    { border-left-color: var(--warn); }
.toast-error   { border-left-color: var(--danger); }

.toast-icon {
    flex-shrink: 0;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--text-tertiary);
    color: #fff;
    text-align: center;
    line-height: 20px;
    font-weight: 700;
    font-size: 12px;
}
.toast-success .toast-icon { background: var(--success); }
.toast-warn    .toast-icon { background: var(--warn); }
.toast-error   .toast-icon { background: var(--danger); }
.toast-info    .toast-icon { background: var(--info); }

.toast-msg { flex: 1; word-break: break-word; color: var(--text); }

.toast-close {
    background: transparent;
    border: none;
    color: var(--text-tertiary);
    font-size: 18px;
    line-height: 1;
    cursor: pointer;
    padding: 0 4px;
    border-radius: var(--r-xs);
    transition: background var(--t-fast) var(--ease), color var(--t-fast) var(--ease);
}
.toast-close:hover { background: var(--surface-hover); color: var(--text); }

.toast-slide-enter-active,
.toast-slide-leave-active {
    transition: all 0.25s ease;
}
.toast-slide-enter-from {
    opacity: 0;
    transform: translateX(40px);
}
.toast-slide-leave-to {
    opacity: 0;
    transform: translateX(40px);
}
</style>
