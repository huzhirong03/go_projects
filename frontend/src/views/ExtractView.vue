<script setup>
import { reactive, watch, onMounted, toRaw } from 'vue'
import PathPicker from '../components/PathPicker.vue'
import ProgressPanel from '../components/ProgressPanel.vue'
import SheetSelector from '../components/SheetSelector.vue'
import Collapsible from '../components/Collapsible.vue'
import { previewFolder, startExtract } from '../api/extract.js'
import { task, startTask } from '../stores/task.js'
import { showToast } from '../stores/toast.js'
import { getViewConfig, saveViewConfig } from '../stores/config.js'
import {
    OUTPUT_PER_KEYWORD, OUTPUT_MERGED, OUTPUT_PER_SOURCE,
} from '../types/events.js'

// 默认值（首次启动 / "恢复默认"时使用）。
const defaults = {
    sourceMode: 'folder',      // folder | file
    folderPath: '',            // 兼作单文件路径（源数据路径）
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
    // 折叠状态（默认全展开）
    foldPaths: false,
    foldKeywords: false,
    foldRange: false,
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
        })
        startTask(handle.taskId)
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
                <PathPicker v-model="form.folderPath"
                            v-model:mode="form.sourceMode"
                            :allow-switch="true"
                            label="源数据"
                            :placeholder="form.sourceMode === 'file' ? '选一个 .xlsx 文件' : '选含多个 Excel 的文件夹'" />
                <PathPicker v-model="form.outputDir" mode="folder"
                            label="输出目录" placeholder="结果会写到这个目录" />
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
                    <label><input type="radio" :value="OUTPUT_PER_SOURCE" v-model="form.strategy" />
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
        </div>

        <div class="actions">
            <button class="btn btn-primary" :disabled="task.running" @click="submit">
                {{ task.running ? '运行中…' : '开始提取' }}
            </button>
        </div>

        <ProgressPanel />
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
    margin-left: auto;
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
.radio-group { gap: 12px; }

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
