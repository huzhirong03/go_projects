<script setup>
import { reactive, watch, onMounted, toRaw } from 'vue'
import PathPicker from '../components/PathPicker.vue'
import ProgressPanel from '../components/ProgressPanel.vue'
import SheetSelector from '../components/SheetSelector.vue'
import { previewFolder, startExtract } from '../api/extract.js'
import { task, startTask } from '../stores/task.js'
import { showToast } from '../stores/toast.js'
import { getViewConfig, saveViewConfig } from '../stores/config.js'
import {
    OUTPUT_PER_KEYWORD, OUTPUT_MERGED, OUTPUT_PER_SOURCE,
} from '../types/events.js'

// 默认值（首次启动 / "恢复默认"时使用）。
const defaults = {
    folderPath: '',
    outputDir: '',
    keywordsRaw: '',
    exact: true,
    contains: true,
    pinyin: true,
    headerRow: 1,
    searchAllCols: true,
    searchColumns: [],
    strategy: OUTPUT_PER_KEYWORD,
    preserveImages: true,
}

const form = reactive({ ...defaults, sheetNames: [] })

// 哪些字段需要持久化到磁盘。sheetNames 不保存（每个文件 Sheet 不一样）。
const PERSIST_KEYS = Object.keys(defaults)

const previewState = reactive({
    loading: false,
    firstFile: '',
    columns: [],
    sheets: [],
    error: '',
})

// 启动时恢复上次的配置，然后再启用自动保存。
let watchHandle = null
onMounted(async () => {
    try {
        const saved = await getViewConfig('extract')
        for (const k of PERSIST_KEYS) {
            if (saved[k] !== undefined) form[k] = saved[k]
        }
    } catch (e) {
        console.warn('恢复 extract 配置失败:', e)
    }
    // 在恢复完成后再装载 watch，避免恢复瞬间触发"假"保存
    watchHandle = watch(
        () => PERSIST_KEYS.map(k => form[k]),
        () => {
            const snap = {}
            for (const k of PERSIST_KEYS) snap[k] = toRaw(form[k])
            saveViewConfig('extract', snap)
        },
        { deep: true },
    )
})

// 选完文件夹（或改了 headerRow）→ 自动预扫描，无需用户点按钮。
watch(() => [form.folderPath, form.headerRow], async ([folder]) => {
    if (!folder) {
        previewState.firstFile = ''
        previewState.columns = []
        previewState.sheets = []
        form.searchColumns = []
        return
    }
    previewState.loading = true
    previewState.error = ''
    try {
        const r = await previewFolder(folder, form.headerRow)
        previewState.firstFile = r.firstFile || ''
        previewState.columns = r.columns || []
        previewState.sheets = r.sheets || []
        // 已勾选的搜索列若不再存在，则去掉
        form.searchColumns = form.searchColumns.filter(c => previewState.columns.includes(c))
    } catch (e) {
        previewState.error = e.message || String(e)
        previewState.columns = []
        previewState.sheets = []
        showToast('预扫描失败：' + (e.message || e), 'error')
    } finally {
        previewState.loading = false
    }
})

function toggleColumn(col) {
    const i = form.searchColumns.indexOf(col)
    if (i >= 0) form.searchColumns.splice(i, 1)
    else form.searchColumns.push(col)
}

async function submit() {
    if (!form.folderPath) return showToast('请先选择源文件夹', 'warn')
    if (!form.outputDir) return showToast('请先选择输出目录', 'warn')
    if (!form.keywordsRaw.trim()) return showToast('请输入至少一个关键词', 'warn')
    if (!form.exact && !form.contains && !form.pinyin) {
        return showToast('请至少选择一种匹配模式', 'warn')
    }
    if (previewState.sheets.length > 1 && form.sheetNames.length === 0) {
        return showToast('请至少勾选一个要处理的 Sheet', 'warn')
    }

    try {
        const handle = await startExtract({
            folderPath: form.folderPath,
            keywordsRaw: form.keywordsRaw,
            exact: form.exact,
            contains: form.contains,
            pinyin: form.pinyin,
            searchAllCols: form.searchAllCols,
            searchColumns: form.searchAllCols ? [] : form.searchColumns,
            strategy: form.strategy,
            outputDir: form.outputDir,
            headerRow: form.headerRow,
            preserveImages: form.preserveImages,
            // 全选时传空数组让后端走默认（处理全部）；非全选才传具体名称。
            sheetNames: previewState.sheets.length === form.sheetNames.length
                ? []
                : form.sheetNames,
        })
        startTask(handle.taskId)
    } catch (e) {
        showToast('启动失败：' + (e.message || e), 'error')
    }
}
</script>

<template>
    <div class="view">
        <h2 class="view-title">批量提取</h2>
        <p class="view-desc">从文件夹中扫描所有 Excel，按关键词提取命中行到新文件，图片跟随行。</p>

        <div class="card">
            <PathPicker v-model="form.folderPath" mode="folder"
                        label="源文件夹" placeholder="选择含多个 Excel 的文件夹" />

            <PathPicker v-model="form.outputDir" mode="folder"
                        label="输出目录" placeholder="结果会写到这个目录" />

            <div class="field">
                <label class="field-label">关键词（逗号/空格/顿号分隔）</label>
                <textarea v-model="form.keywordsRaw" rows="2"
                          placeholder="例如：口红, 眼影, fd"></textarea>
            </div>

            <div class="field">
                <label class="field-label">匹配模式</label>
                <div class="inline-group">
                    <label><input type="checkbox" v-model="form.exact" /> 精准</label>
                    <label><input type="checkbox" v-model="form.contains" /> 包含</label>
                    <label><input type="checkbox" v-model="form.pinyin" /> 拼音（含首字母）</label>
                </div>
            </div>

            <div class="field">
                <label class="field-label">表头行号</label>
                <input type="number" min="0" v-model.number="form.headerRow" style="width:80px" />
                <span class="field-hint">0 表示无表头</span>
            </div>

            <!-- Sheet 选择（仅多 Sheet 时渲染勾选 UI）-->
            <SheetSelector v-model="form.sheetNames" :sheets="previewState.sheets" />

            <div class="field">
                <label class="field-label">搜索范围</label>
                <div class="inline-group">
                    <label><input type="radio" :value="true" v-model="form.searchAllCols" /> 全列搜索</label>
                    <label><input type="radio" :value="false" v-model="form.searchAllCols" /> 指定列</label>
                </div>
                <div v-if="!form.searchAllCols" class="column-selector">
                    <span v-if="previewState.firstFile" class="field-hint">
                        列名来自 {{ previewState.firstFile }}
                        <span v-if="previewState.loading">（读取中…）</span>
                    </span>
                    <span v-else class="field-hint">请先选择源文件夹</span>
                    <div v-if="previewState.columns.length" class="column-chips">
                        <label v-for="c in previewState.columns" :key="c" class="chip">
                            <input type="checkbox" :checked="form.searchColumns.includes(c)"
                                   @change="toggleColumn(c)" />
                            {{ c }}
                        </label>
                    </div>
                </div>
            </div>

            <div class="field">
                <label class="field-label">输出策略</label>
                <div class="inline-group radio-group">
                    <label><input type="radio" :value="OUTPUT_PER_KEYWORD" v-model="form.strategy" />
                        每个关键词一个文件</label>
                    <label><input type="radio" :value="OUTPUT_MERGED" v-model="form.strategy" />
                        合成一个文件</label>
                    <label><input type="radio" :value="OUTPUT_PER_SOURCE" v-model="form.strategy" />
                        每个源文件一个</label>
                </div>
            </div>

            <div class="field">
                <label><input type="checkbox" v-model="form.preserveImages" /> 保留图片（跟随行）</label>
            </div>

            <div class="actions">
                <button class="btn btn-primary" :disabled="task.running" @click="submit">
                    {{ task.running ? '运行中…' : '开始提取' }}
                </button>
            </div>
        </div>

        <ProgressPanel />
    </div>
</template>

<style scoped>
.view { display: flex; flex-direction: column; gap: 16px; }
.view-title { margin: 0; font-size: 20px; color: #f1f5f9; }
.view-desc { margin: 0; color: #94a3b8; font-size: 13px; }
.card {
    background: #1f2738;
    border: 1px solid #2d3748;
    border-radius: 8px;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
}
.field { display: flex; flex-direction: column; gap: 6px; }
.field-label { font-size: 13px; color: #a9b4c6; font-weight: 500; }
.field-hint { font-size: 12px; color: #64748b; margin-left: 6px; }
.inline-group { display: flex; gap: 18px; flex-wrap: wrap; color: #cbd5e1; }
.inline-group label { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.radio-group { gap: 10px; }
.column-selector {
    margin-top: 8px;
    padding: 10px;
    background: #0f172a;
    border-radius: 6px;
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.column-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.chip {
    background: #334155;
    padding: 4px 10px;
    border-radius: 14px;
    font-size: 12px;
    color: #e2e8f0;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
}
.error-msg { color: #f87171; font-size: 12px; }
.actions { display: flex; justify-content: flex-end; }
</style>
