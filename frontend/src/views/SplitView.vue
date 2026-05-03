<script setup>
import { reactive, watch, computed, onMounted, toRaw, ref, nextTick } from 'vue'
import PathPicker from '../components/PathPicker.vue'
import ProgressPanel from '../components/ProgressPanel.vue'
import SheetSelector from '../components/SheetSelector.vue'
import Collapsible from '../components/Collapsible.vue'
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
    // CSV 可选参数（仅 by_keyword + .csv 生效，后端同步序）
    csvEncoding: '',
    csvDelimiter: '',
    // 输出目标；new_files = 默认，inplace_sheets = 写回源文件新 Sheet
    outputTarget: 'new_files',
    backupSource: false,
    pinyin: true,
    searchAllCols: true,
    searchColumns: [],
    strategy: OUTPUT_PER_KEYWORD,
    foldPaths: false,
    foldMode: false,
}

const form = reactive({ ...defaults, sheetNames: [] })
const progressEl = ref(null)

// 任务完成（成功或失败）时滚到底部，详见 ExtractView 同名 watch 注释。
watch(() => [task.result, task.error], ([r, e]) => {
    if (r || e) scrollToBottom()
})

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
// CSV 源识别：单文件拆分看后缀即可。
const hasCSVSource = computed(() => /\.csv$/i.test(form.sourcePath || ''))

// inplace 可用性：xlsx 源且不是 by_sheet 模式（by_sheet inplace 语义为 0）
const inplaceAvailable = computed(() =>
    !!form.sourcePath && !/\.csv$/i.test(form.sourcePath) && form.mode !== SPLIT_BY_SHEET
)
const isInplace = computed(() => inplaceAvailable.value && form.outputTarget === 'inplace_sheets')

watch(inplaceAvailable, (v) => {
    if (!v && form.outputTarget === 'inplace_sheets') form.outputTarget = 'new_files'
})
// SplitView 的策略只有 per_keyword / merged，不需要降级处理。

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

// 任务完成后滚到页面底部：见 ExtractView 同名函数注释
function scrollToBottom() {
    nextTick(() => {
        window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' })
    })
}

async function submit() {
    if (!form.sourcePath) return showToast('请先选择源文件', 'warn')
    if (!isInplace.value && !form.outputDir) return showToast('请先选择输出目录', 'warn')
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
            csvEncoding: form.csvEncoding,
            csvDelimiter: form.csvDelimiter,
            outputTarget: isInplace.value ? 'inplace_sheets' : 'new_files',
            backupSource: isInplace.value && form.backupSource,
        })
        startTask(handle.taskId)
        // 任务完成后由 watch(task.result/error) 自动滚到底部
    } catch (e) {
        showToast('启动失败：' + (e.message || e), 'error')
    }
}
</script>

<template>
    <div class="view">
        <div class="view-header">
            <h2 class="view-title">单文件拆分</h2>
            <span class="view-desc">把一个大 Excel 按 Sheet / 行数 / 列值 / 关键词拆成多个文件，格式与图片全保留。</span>
        </div>

        <Collapsible title="路径配置" :open="!form.foldPaths" @update:open="v => form.foldPaths = !v">
            <div class="row-2col">
                <PathPicker v-model="form.sourcePath" mode="file"
                            label="源文件" placeholder="选择要拆分的 .xlsx / .csv" />
                <PathPicker v-if="!isInplace" v-model="form.outputDir" mode="folder"
                            label="输出目录" placeholder="拆分结果输出到此" />
                <div v-else class="inplace-hint">
                    <label class="field-label">输出目录</label>
                    <span class="field-hint">结果会以新 Sheet 形式写回源文件，无需输出目录</span>
                </div>
            </div>
            <div v-if="inplaceAvailable" class="strip strip-inline" style="margin-top:8px">
                <span class="strip-title">输出目标</span>
                <div class="seg" role="tablist">
                    <button type="button" :class="['seg-btn', { active: form.outputTarget === 'new_files' }]"
                            @click="form.outputTarget = 'new_files'">📄 新文件</button>
                    <button type="button" :class="['seg-btn', { active: form.outputTarget === 'inplace_sheets' }]"
                            @click="form.outputTarget = 'inplace_sheets'">📑 写回源文件新 Sheet</button>
                </div>
                <label v-if="isInplace" class="keep-images" style="margin-left:auto">
                    <input type="checkbox" v-model="form.backupSource" /> 写回前先备份源文件 (.bak)
                </label>
            </div>
            <div v-else-if="form.mode === SPLIT_BY_SHEET" class="field-hint" style="margin-top:6px">
                按 Sheet 拆分仅支持输出新文件
            </div>
        </Collapsible>

        <Collapsible title="拆分方式" :open="!form.foldMode" @update:open="v => form.foldMode = !v">
            <div class="field">
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

        </Collapsible>

        <div class="strip">
            <span class="strip-title">输出选项</span>
            <template v-if="needsHeader">
                <label class="field-label">表头行号</label>
                <input type="number" min="0" v-model.number="form.headerRow" style="width:64px" />
                <span class="field-hint">每份复制该行作表头</span>
            </template>
            <label class="keep-images"><input type="checkbox" v-model="form.preserveImages" /> 保留图片</label>
        </div>

        <div v-if="hasCSVSource && form.mode === SPLIT_BY_KEYWORD" class="strip csv-strip">
            <span class="strip-title">CSV</span>
            <label class="csv-field">编码
                <select v-model="form.csvEncoding" class="name-select">
                    <option value="">自动识别</option>
                    <option value="utf-8">UTF-8</option>
                    <option value="gbk">GBK</option>
                    <option value="gb18030">GB18030</option>
                    <option value="big5">Big5</option>
                    <option value="utf-16">UTF-16</option>
                </select>
            </label>
            <label class="csv-field">分隔符
                <select v-model="form.csvDelimiter" class="name-select">
                    <option value="">自动推断</option>
                    <option value=",">逗号 ,</option>
                    <option value=";">分号 ;</option>
                    <option value="\t">制表符 \t</option>
                    <option value="|">竖线 |</option>
                </select>
            </label>
        </div>

        <div class="actions">
            <button class="btn btn-primary" :disabled="task.running" @click="submit">
                {{ task.running ? '运行中…' : '开始拆分' }}
            </button>
        </div>

        <div ref="progressEl"><ProgressPanel /></div>
    </div>
</template>

<style scoped>
.view { display: flex; flex-direction: column; gap: 10px; }
.view-header {
    display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap;
}
.view-title {
    margin: 0;
    font-family: var(--font-display);
    font-size: 18px;
    font-weight: 600;
    color: var(--text);
    letter-spacing: -0.015em;
}
.view-desc { color: var(--text-secondary); font-size: 13px; }

.label-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.row-2col {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 12px;
    align-items: start;
}

.strip {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    padding: 10px 14px;
    display: flex;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
    color: var(--text);
    box-shadow: var(--shadow-card);
}
.strip-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
}
.keep-images {
    margin-left: auto;
    color: var(--text-secondary);
    display: inline-flex; align-items: center; gap: 6px;
    cursor: pointer; font-size: 13px;
}
.csv-field {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-secondary);
    font-size: 13px;
}
.name-select {
    height: 28px;
    padding: 0 8px;
    font-size: 13px;
    min-width: 130px;
}
.strip-inline { padding: 8px 14px; }
.seg {
    display: inline-flex;
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-sm);
    padding: 2px;
}
.seg-btn {
    appearance: none; background: transparent; border: none;
    color: var(--text-secondary);
    font: inherit; font-size: 12px; font-weight: 500;
    padding: 4px 12px; border-radius: 3px; cursor: pointer;
    display: inline-flex; align-items: center; gap: 4px;
}
.seg-btn:hover { background: var(--surface-hover); color: var(--text); }
.seg-btn.active {
    background: var(--accent-soft);
    color: var(--accent-soft-fg);
    font-weight: 600;
    box-shadow: inset 0 0 0 1px var(--accent);
}
.inplace-hint {
    display: flex; flex-direction: column; gap: 6px; padding-top: 26px;
}

.card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    box-shadow: var(--shadow-card);
}

.field { display: flex; flex-direction: column; gap: 6px; }
.field-label {
    font-size: 13px;
    color: var(--text);
    font-weight: 600;
}
.field-hint { font-size: 12px; color: var(--text-tertiary); margin-left: 6px; }
.inline-group {
    display: flex; gap: 16px; flex-wrap: wrap;
    color: var(--text);
}
.inline-group label {
    display: inline-flex; align-items: center; gap: 6px;
    cursor: pointer; font-size: 13px;
}
.radio-group { gap: 12px; }
.actions { display: flex; justify-content: flex-end; padding-top: 4px; }

.kw-block {
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
}
.column-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 4px;
}
.chip {
    background: var(--surface);
    border: 1px solid var(--border-strong);
    padding: 3px 10px;
    border-radius: 12px;
    font-size: 12px;
    color: var(--text-secondary);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
    transition: background var(--t-fast) var(--ease),
                border-color var(--t-fast) var(--ease),
                color var(--t-fast) var(--ease);
}
.chip:hover { background: var(--surface-hover); color: var(--text); }
.chip:has(input:checked) {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent-soft-fg);
    font-weight: 600;
}
</style>
