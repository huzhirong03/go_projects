<script setup>
// 文件夹或文件选择器。mode="folder" | "file"。
import { chooseFolder, chooseFile } from '../api/index.js'
import { showToast } from '../stores/toast.js'

const props = defineProps({
    modelValue: { type: String, default: '' },
    mode: { type: String, default: 'folder' },
    placeholder: { type: String, default: '未选择' },
    label: { type: String, default: '' },
})
const emit = defineEmits(['update:modelValue'])

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
</script>

<template>
    <div class="path-picker">
        <label v-if="label" class="path-label">{{ label }}</label>
        <div class="path-row">
            <input class="path-input" type="text" :value="modelValue"
                   :placeholder="placeholder"
                   @input="emit('update:modelValue', $event.target.value)" />
            <button class="btn btn-secondary" type="button" @click="pick">
                {{ mode === 'file' ? '浏览文件' : '浏览文件夹' }}
            </button>
        </div>
    </div>
</template>

<style scoped>
.path-picker { display: flex; flex-direction: column; gap: 4px; }
.path-label { font-size: 13px; color: #a9b4c6; font-weight: 500; }
.path-row { display: flex; gap: 8px; }
.path-input { flex: 1; }
</style>
