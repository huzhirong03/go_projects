<script setup>
import { reactive, watch, onMounted, toRaw, ref, nextTick, computed } from 'vue'
import PathPicker from '../components/PathPicker.vue'
import ProgressPanel from '../components/ProgressPanel.vue'
import SheetSelector from '../components/SheetSelector.vue'
import Collapsible from '../components/Collapsible.vue'
import { previewFolder, startExtract } from '../api/extract.js'
import { task, startTask } from '../stores/task.js'
import { showToast } from '../stores/toast.js'
import { getViewConfig, saveViewConfig } from '../stores/config.js'
import { LogPrint } from '../../wailsjs/runtime/runtime'
import { LogStartup } from '../../wailsjs/go/main/App'
import {
    OUTPUT_PER_KEYWORD, OUTPUT_MERGED, OUTPUT_PER_SOURCE,
} from '../types/events.js'

function logT(msg) {
    LogPrint(`[STARTUP-FE] ${msg}`)
    LogStartup(msg).catch(() => {})
}

// 默认值（首次启动 / "恢复默认"时使用）。
const defaults = {
    sourceMode: 'folder',      // folder | file
    folderPath: '',            // 文件夹模式下的路径
    filePath: '',              // 单文件模式下的路径，两者互不干扰，切换后不丢
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
    filenamePrefix: '',        // '' = 默认；'搜索_' = 搜索_关键词
    // CSV 可选参数；空 = 后端自动推断
    csvEncoding: '',
    csvDelimiter: '',
    // 输出目标；new_files = 默认，inplace_sheets = 写回源文件新 Sheet
    // 仅单文件 + xlsx 源生效；其它场景后端自动回退 new_files
    outputTarget: 'new_files',
    backupSource: false,
    // 折叠状态（默认全展开）
    foldPaths: false,
    foldKeywords: false,
    foldRange: false,
}

const form = reactive({ ...defaults, sheetNames: [] })
const progressEl = ref(null) // ProgressPanel 容器（ref 保留备用）

// 当前生效的源路径：根据 sourceMode 转发到 folderPath / filePath。
// PathPicker 用 v-model="sourcePath"，这样切换模式后另一路径仍保留在 form 里。
const sourcePath = computed({
    get: () => form.sourceMode === 'file' ? form.filePath : form.folderPath,
    set: (v) => {
        if (form.sourceMode === 'file') form.filePath = v
        else form.folderPath = v
    },
})

// 源中是否涉及 CSV（单文件看后缀；文件夹看预扫描结果里有没有 .csv 馈送者）。
// 这里保守估计：单文件模式靠后缀、文件夹模式靠 firstFile 后缀。
const hasCSVSource = computed(() => {
    if (form.sourceMode === 'file') return /\.csv$/i.test(form.filePath || '')
    return /\.csv$/i.test(previewState.firstFile || '')
})

// inplace 只在“单文件 + xlsx”时可用；文件夹 / CSV 都隐藏该选项。
const inplaceAvailable = computed(() =>
    form.sourceMode === 'file' && form.filePath && !/\.csv$/i.test(form.filePath)
)
const isInplace = computed(() => inplaceAvailable.value && form.outputTarget === 'inplace_sheets')

// inplace 下隐藏 per_source（单文件语义等同 merged）。若用户之前选了 per_source，
// 切到 inplace 时自动回退为 per_keyword。
watch(isInplace, (v) => {
    if (v && form.strategy === OUTPUT_PER_SOURCE) form.strategy = OUTPUT_PER_KEYWORD
})
// 当源切换为不可用 inplace 的场景时，重置输出目标，避免以隐藏状态裹挨提交。
watch(inplaceAvailable, (v) => {
    if (!v && form.outputTarget === 'inplace_sheets') form.outputTarget = 'new_files'
})

// 任务完成（成功或失败）时，自动平滑滚到底部，让用户看到结果总结/错误。
// 用 watch 而不是在 submit 里立刻滚——结果还没出来滚也是空的。
watch(() => [task.result, task.error], ([r, e]) => {
    if (r || e) scrollToBottom()
})

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
    const _t0 = performance.now()
    logT(`ExtractView onMounted at ${_t0.toFixed(0)}ms`)
    try {
        const tCfg = performance.now()
        const saved = await getViewConfig('extract')
        logT(`ExtractView getViewConfig('extract') took ${(performance.now() - tCfg).toFixed(0)}ms`)
        for (const k of PERSIST_KEYS) {
            if (saved[k] !== undefined) form[k] = saved[k]
        }
        // 字段清洗：folderPath 应该只放文件夹路径，filePath 只放文件路径。
        // 旧版本只有一个 folderPath 字段，升级后可能 folderPath 里残留文件路径。
        // 凡是看起来像 Excel 文件的就搬到 filePath，并清空 folderPath，避免切到文件夹模式时显示文件路径。
        const looksLikeFile = (p) => /\.(xlsx|xls|xlsm|csv)$/i.test(p || '')
        if (looksLikeFile(form.folderPath)) {
            if (!form.filePath) form.filePath = form.folderPath
            form.folderPath = ''
        }
    } catch (e) {
        console.warn('恢复 extract 配置失败:', e)
    }
    logT(`ExtractView mount complete at +${(performance.now() - _t0).toFixed(0)}ms`)
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

// 选完源（文件夹或单文件）或改了 headerRow → 自动预扫描。
watch(() => [sourcePath.value, form.headerRow], async ([folder]) => {
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

// scrollToBottom 把页面平滑滚到底部。仅在任务完成（task.result 或 task.error 出现）后调用，
// 让用户视线自然落到"完成总结"卡片或错误提示上。
function scrollToBottom() {
    nextTick(() => {
        window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' })
    })
}

async function submit() {
    if (!sourcePath.value) {
        return showToast(form.sourceMode === 'file' ? '请先选择源文件' : '请先选择源文件夹', 'warn')
    }
    if (!isInplace.value && !form.outputDir) return showToast('请先选择输出目录', 'warn')
    if (!form.keywordsRaw.trim()) return showToast('请输入至少一个关键词', 'warn')
    if (!form.exact && !form.contains && !form.pinyin) {
        return showToast('请至少选择一种匹配模式', 'warn')
    }
    if (previewState.sheets.length > 1 && form.sheetNames.length === 0) {
        return showToast('请至少勾选一个要处理的 Sheet', 'warn')
    }

    try {
        const handle = await startExtract({
            folderPath: sourcePath.value,
            filenamePrefix: form.filenamePrefix || '',
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
            csvEncoding: form.csvEncoding,
            csvDelimiter: form.csvDelimiter,
            outputTarget: isInplace.value ? 'inplace_sheets' : 'new_files',
            backupSource: isInplace.value && form.backupSource,
        })
        startTask(handle.taskId)
        // 注意：不在这里立刻滚动，结果还没生成滚下去也是空的。
        // 任务完成后由 watch(task.result/error) 触发 scrollToBottom。
    } catch (e) {
        showToast('启动失败：' + (e.message || e), 'error')
    }
}
</script>

<template>
    <div class="view">
        <div class="view-header">
            <h2 class="view-title">批量提取</h2>
            <span class="view-desc">从文件夹中扫描所有 Excel，按关键词提取命中行到新文件，图片跟随行。</span>
        </div>

        <Collapsible title="路径配置" :open="!form.foldPaths" @update:open="v => form.foldPaths = !v">
            <div class="row-2col">
                <PathPicker v-model="sourcePath"
                            v-model:mode="form.sourceMode"
                            :allow-switch="true"
                            label="源数据"
                            :placeholder="form.sourceMode === 'file' ? '选一个 .xlsx / .csv 文件' : '选含多个 Excel / CSV 的文件夹'" />
                <PathPicker v-if="!isInplace" v-model="form.outputDir" mode="folder"
                            label="输出目录" placeholder="结果会写到这个目录" />
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
        </Collapsible>

        <Collapsible title="关键词与匹配" :open="!form.foldKeywords" @update:open="v => form.foldKeywords = !v">
            <div class="row-2col">
                <div class="field">
                    <label class="field-label">关键词（逗号/空格/顿号分隔）</label>
                    <textarea v-model="form.keywordsRaw" rows="2"
                              placeholder="例如：口红, 眼影, fd"></textarea>
                </div>
                <div class="field">
                    <label class="field-label">匹配模式</label>
                    <div class="inline-group match-group">
                        <label><input type="checkbox" v-model="form.exact" /> 精准</label>
                        <label><input type="checkbox" v-model="form.contains" /> 包含</label>
                        <label><input type="checkbox" v-model="form.pinyin" /> 拼音（含首字母）</label>
                    </div>
                </div>
            </div>
        </Collapsible>

        <Collapsible title="数据范围" :open="!form.foldRange" @update:open="v => form.foldRange = !v">
            <div class="field">
                <div class="label-row">
                    <label class="field-label">表头行号</label>
                    <span class="field-hint">0 表示无表头</span>
                </div>
                <input type="number" min="0" v-model.number="form.headerRow" style="width:80px" />
            </div>
            <SheetSelector v-model="form.sheetNames" :sheets="previewState.sheets" />
            <div class="field">
                <div class="label-row">
                    <label class="field-label">搜索范围</label>
                    <div class="inline-group">
                        <label><input type="radio" :value="true" v-model="form.searchAllCols" /> 全列搜索</label>
                        <label><input type="radio" :value="false" v-model="form.searchAllCols" /> 指定列</label>
                    </div>
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
        </Collapsible>

        <div class="strip strip-output">
            <div class="strip-row">
                <span class="strip-title">策略</span>
                <div class="inline-group radio-group">
                    <label><input type="radio" :value="OUTPUT_PER_KEYWORD" v-model="form.strategy" />
                        每个关键词一个文件</label>
                    <label><input type="radio" :value="OUTPUT_MERGED" v-model="form.strategy" />
                        合成一个文件</label>
                    <label v-if="!isInplace"><input type="radio" :value="OUTPUT_PER_SOURCE" v-model="form.strategy" />
                        每个源文件一个</label>
                </div>
            </div>
            <div class="strip-row">
                <span class="strip-title">命名</span>
                <select v-model="form.filenamePrefix" class="name-select">
                    <option value="">默认</option>
                    <option value="搜索_">搜索_关键词</option>
                </select>
                <label class="keep-images"><input type="checkbox" v-model="form.preserveImages" /> 保留图片（跟随行）</label>
            </div>
            <div v-if="hasCSVSource" class="strip-row csv-row">
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
        </div>

        <div class="actions">
            <button class="btn btn-primary" :disabled="task.running" @click="submit">
                {{ task.running ? '运行中…' : '开始提取' }}
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
.keep-images {
    color: var(--text-secondary);
    display: inline-flex; align-items: center; gap: 6px;
    cursor: pointer;
    font-size: 13px;
}
.row-2col {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 12px;
    align-items: start;
}
.match-group { padding-top: 2px; }

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
.strip-output {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
}
.strip-row {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
}
.name-select {
    height: 28px;
    padding: 0 8px;
    font-size: 13px;
    min-width: 160px;
}
.csv-field {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-secondary);
    font-size: 13px;
}
.csv-field .name-select { min-width: 130px; }

/* 输出目标分段控件（与 PathPicker 用同一套设计） */
.strip-inline { padding: 8px 14px; }
.seg {
    display: inline-flex;
    background: var(--surface-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--r-sm);
    padding: 2px;
}
.seg-btn {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--text-secondary);
    font: inherit;
    font-size: 12px;
    font-weight: 500;
    padding: 4px 12px;
    border-radius: 3px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
}
.seg-btn:hover { background: var(--surface-hover); color: var(--text); }
.seg-btn.active {
    background: var(--accent-soft);
    color: var(--accent-soft-fg);
    font-weight: 600;
    box-shadow: inset 0 0 0 1px var(--accent);
}
.inplace-hint {
    display: flex; flex-direction: column; gap: 6px;
    padding-top: 26px;
}
.strip-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
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
/* 策略 radio 间距特意加大：让"合成一个文件"在视觉上更接近下行的"保留图片"列。 */
.radio-group { gap: 32px; }

.column-selector {
    margin-top: 8px;
    padding: 10px;
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.column-chips { display: flex; flex-wrap: wrap; gap: 6px; }
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

.error-msg { color: var(--danger); font-size: 12px; }
.actions { display: flex; justify-content: flex-end; padding-top: 4px; }
</style>
