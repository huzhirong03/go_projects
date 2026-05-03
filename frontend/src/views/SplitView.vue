<script setup>
import { reactive, watch, computed, onMounted, toRaw } from 'vue'
import PathPicker from '../components/PathPicker.vue'
import ProgressPanel from '../components/ProgressPanel.vue'
import SheetSelector from '../components/SheetSelector.vue'
import { startSplit, previewFile } from '../api/split.js'
import { task, startTask } from '../stores/task.js'
import { showToast } from '../stores/toast.js'
import { getViewConfig, saveViewConfig } from '../stores/config.js'
import {
    SPLIT_BY_SHEET, SPLIT_BY_ROWS, SPLIT_BY_COLUMN, SPLIT_BY_KEYWORD,
    OUTPUT_PER_KEYWORD, OUTPUT_MERGED,
} from '../types/events.js'

const defaults = {
    sourcePath: '',
    outputDir: '',
    mode: SPLIT_BY_SHEET,
    rowsPerFile: 50000,
    splitColumn: '',
    headerRow: 1,
    preserveImages: true,

    // by_keyword 模式专用
    keywordsRaw: '',
    exact: true,
    contains: true,
    pinyin: true,
    searchAllCols: true,
    searchColumns: [],
    strategy: OUTPUT_PER_KEYWORD,
}

const form = reactive({ ...defaults, sheetNames: [] })

const PERSIST_KEYS = Object.keys(defaults)

const previewState = reactive({
    loading: false,
    sheets: [],
    columns: [],
    error: '',
})

// 启动时恢复上次的配置 + 装载防抖自动保存
onMounted(async () => {
    try {
        const saved = await getViewConfig('split')
        for (const k of PERSIST_KEYS) {
            if (saved[k] !== undefined) form[k] = saved[k]
        }
    } catch (e) {
        console.warn('恢复 split 配置失败:', e)
    }
    watch(
        () => PERSIST_KEYS.map(k => form[k]),
        () => {
            const snap = {}
            for (const k of PERSIST_KEYS) snap[k] = toRaw(form[k])
            saveViewConfig('split', snap)
        },
        { deep: true },
    )
})

// 当前模式是否需要表头（by_sheet 不需要）。
const needsHeader = computed(() => form.mode !== SPLIT_BY_SHEET)

// 选完文件 / 改了 headerRow → 自动预扫描。
watch(() => [form.sourcePath, form.headerRow], async ([path]) => {
    if (!path) {
        previewState.sheets = []
        previewState.columns = []
        form.searchColumns = []
        return
    }
    previewState.loading = true
    previewState.error = ''
    try {
        const r = await previewFile(path, form.headerRow)
        previewState.sheets = r.sheets || []
        previewState.columns = r.columns || []
        form.searchColumns = form.searchColumns.filter(c => previewState.columns.includes(c))
    } catch (e) {
        previewState.error = e.message || String(e)
        previewState.sheets = []
        previewState.columns = []
        showToast('预扫描失败：' + (e.message || e), 'error')
    } finally {
        previewState.loading = false
    }
})

function toggleSearchCol(col) {
    const i = form.searchColumns.indexOf(col)
    if (i >= 0) form.searchColumns.splice(i, 1)
    else form.searchColumns.push(col)
}

async function submit() {
    if (!form.sourcePath) return showToast('请先选择源文件', 'warn')
    if (!form.outputDir) return showToast('请先选择输出目录', 'warn')
    if (previewState.sheets.length > 1 && form.sheetNames.length === 0) {
        return showToast('请至少勾选一个要处理的 Sheet', 'warn')
    }
    if (form.mode === SPLIT_BY_ROWS && form.rowsPerFile <= 0) {
        return showToast('每份行数必须大于 0', 'warn')
    }
    if (form.mode === SPLIT_BY_COLUMN && !form.splitColumn.trim()) {
        return showToast('请填写拆分列名', 'warn')
    }
    if (form.mode === SPLIT_BY_KEYWORD) {
        if (!form.keywordsRaw.trim()) return showToast('请输入至少一个关键词', 'warn')
        if (!form.exact && !form.contains && !form.pinyin) {
            return showToast('请至少选择一种匹配模式', 'warn')
        }
    }

    try {
        const handle = await startSplit({
            sourcePath: form.sourcePath,
            mode: form.mode,
            rowsPerFile: form.rowsPerFile,
            splitColumn: form.splitColumn,
            outputDir: form.outputDir,
            headerRow: form.headerRow,
            preserveImages: form.preserveImages,
            sheetNames: previewState.sheets.length === form.sheetNames.length
                ? []
                : form.sheetNames,
            // by_keyword 字段（其他模式忽略）
            keywordsRaw: form.keywordsRaw,
            exact: form.exact,
            contains: form.contains,
            pinyin: form.pinyin,
            searchAllCols: form.searchAllCols,
            searchColumns: form.searchAllCols ? [] : form.searchColumns,
            strategy: form.strategy,
        })
        startTask(handle.taskId)
    } catch (e) {
        showToast('启动失败：' + (e.message || e), 'error')
    }
}
</script>

<template>
    <div class="view">
        <h2 class="view-title">单文件拆分</h2>
        <p class="view-desc">把一个大 Excel 按 Sheet / 行数 / 列值 / 关键词拆成多个文件，格式与图片全保留。</p>

        <div class="card">
            <PathPicker v-model="form.sourcePath" mode="file"
                        label="源文件" placeholder="选择要拆分的 .xlsx" />

            <PathPicker v-model="form.outputDir" mode="folder"
                        label="输出目录" placeholder="拆分结果输出到此" />

            <div class="field">
                <label class="field-label">拆分方式</label>
                <div class="inline-group radio-group">
                    <label><input type="radio" :value="SPLIT_BY_SHEET" v-model="form.mode" />
                        按 Sheet</label>
                    <label><input type="radio" :value="SPLIT_BY_ROWS" v-model="form.mode" />
                        按行数</label>
                    <label><input type="radio" :value="SPLIT_BY_COLUMN" v-model="form.mode" />
                        按列值</label>
                    <label><input type="radio" :value="SPLIT_BY_KEYWORD" v-model="form.mode" />
                        按关键词</label>
                </div>
            </div>

            <!-- Sheet 选择（仅多 Sheet 时渲染勾选 UI）-->
            <SheetSelector v-model="form.sheetNames" :sheets="previewState.sheets" />

            <!-- 模式专属字段：按行数 -->
            <div v-if="form.mode === SPLIT_BY_ROWS" class="field">
                <label class="field-label">每份行数</label>
                <input type="number" min="1" v-model.number="form.rowsPerFile" style="width:140px" />
            </div>

            <!-- 模式专属字段：按列值 -->
            <div v-if="form.mode === SPLIT_BY_COLUMN" class="field">
                <label class="field-label">拆分列名</label>
                <input type="text" v-model="form.splitColumn" placeholder="如：类别 / 品牌"
                       list="split-cols" style="width:240px" />
                <datalist id="split-cols">
                    <option v-for="c in previewState.columns" :key="c" :value="c" />
                </datalist>
                <span class="field-hint">相同列值的行会进同一个文件（多 Sheet 时每个 Sheet 各产出独立文件）</span>
            </div>

            <!-- 模式专属字段：按关键词 -->
            <div v-if="form.mode === SPLIT_BY_KEYWORD" class="kw-block">
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
                    <label class="field-label">搜索范围</label>
                    <div class="inline-group">
                        <label><input type="radio" :value="true" v-model="form.searchAllCols" /> 全列搜索</label>
                        <label><input type="radio" :value="false" v-model="form.searchAllCols" /> 指定列</label>
                    </div>
                    <div v-if="!form.searchAllCols && previewState.columns.length" class="column-chips">
                        <label v-for="c in previewState.columns" :key="c" class="chip">
                            <input type="checkbox" :checked="form.searchColumns.includes(c)"
                                   @change="toggleSearchCol(c)" />
                            {{ c }}
                        </label>
                    </div>
                    <span v-else-if="!form.searchAllCols" class="field-hint">请先选择源文件读取列名</span>
                </div>
                <div class="field">
                    <label class="field-label">输出策略</label>
                    <div class="inline-group radio-group">
                        <label><input type="radio" :value="OUTPUT_PER_KEYWORD" v-model="form.strategy" />
                            每个关键词一个文件</label>
                        <label><input type="radio" :value="OUTPUT_MERGED" v-model="form.strategy" />
                            合成一个文件</label>
                    </div>
                </div>
            </div>

            <div class="field" v-if="needsHeader">
                <label class="field-label">表头行号</label>
                <input type="number" min="0" v-model.number="form.headerRow" style="width:80px" />
                <span class="field-hint">每份都会复制这一行作为表头</span>
            </div>

            <div class="field">
                <label><input type="checkbox" v-model="form.preserveImages" /> 保留图片</label>
            </div>

            <div class="actions">
                <button class="btn btn-primary" :disabled="task.running" @click="submit">
                    {{ task.running ? '运行中…' : '开始拆分' }}
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
.actions { display: flex; justify-content: flex-end; }

.kw-block {
    background: #0f172a;
    border-radius: 6px;
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    border: 1px solid #1e293b;
}
.column-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 4px;
}
.chip {
    background: #334155;
    padding: 3px 10px;
    border-radius: 14px;
    font-size: 12px;
    color: #e2e8f0;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
}
</style>
