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
    top: 20px;
    right: 20px;
    z-index: 9999;
    display: flex;
    flex-direction: column;
    gap: 8px;
    pointer-events: none;
}
.toast {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 240px;
    max-width: 420px;
    padding: 10px 14px;
    border-radius: 8px;
    border: 1px solid #2d3748;
    background: #1f2738;
    color: #e2e8f0;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    pointer-events: auto;
    cursor: pointer;
    font-size: 13px;
    line-height: 1.4;
}
.toast-info { border-color: #3b82f6; }
.toast-success { border-color: #16a34a; background: #052e1a; }
.toast-warn { border-color: #f59e0b; background: #3a2410; }
.toast-error { border-color: #dc2626; background: #450a0a; }

.toast-icon {
    flex-shrink: 0;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: #334155;
    color: white;
    text-align: center;
    line-height: 22px;
    font-weight: 700;
    font-size: 13px;
}
.toast-success .toast-icon { background: #16a34a; }
.toast-warn .toast-icon { background: #f59e0b; }
.toast-error .toast-icon { background: #dc2626; }
.toast-info .toast-icon { background: #2563eb; }

.toast-msg { flex: 1; word-break: break-word; }

.toast-close {
    background: transparent;
    border: none;
    color: inherit;
    opacity: 0.6;
    font-size: 18px;
    line-height: 1;
    cursor: pointer;
    padding: 0 2px;
}
.toast-close:hover { opacity: 1; }

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
