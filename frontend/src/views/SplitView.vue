<script setup>
import { reactive, watch, computed, onMounted, toRaw, ref, nextTick } from 'vue'
import PathPicker from '../components/PathPicker.vue'
import ProgressPanel from '../components/ProgressPanel.vue'
import SheetSelector from '../components/SheetSelector.vue'
import Collapsible from '../components/Collapsible.vue'
import AdvancedFilter from '../components/AdvancedFilter.vue'
import { startSplit, previewFile } from '../api/split.js'
import { task, startTask } from '../stores/task.js'
import { showToast } from '../stores/toast.js'
import { getViewConfig, saveViewConfig } from '../stores/config.js'
import {
    SPLIT_BY_SHEET, SPLIT_BY_ROWS, SPLIT_BY_COLUMN, SPLIT_BY_KEYWORD,
    OUTPUT_PER_KEYWORD, OUTPUT_MERGED,
} from '../types/events.js'
import { FILTER_MODE_ALL, countActiveConditions, isMeaningfulCondition } from '../types/filter.js'

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
    // 高级筛选 V1.5+：仅 by_keyword 模式生效
    advancedFilter: { mode: FILTER_MODE_ALL, conditions: [] },
    advancedFilterPresets: [],
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
    // 智能默认：无激活条件 → 展开；有 → 收起 + 角标提示
    advFilterOpen.value = activeFilterCount.value === 0
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

// 单文件拆分始终显示"输出目标"条带，避免用户没选源时以为功能缺失。
// inplace 实际可用：选了 xlsx 源 + 不是 by_sheet 模式（by_sheet 下源 sheet 已经是结果，
// 再追加新 sheet 没意义；CSV 源不支持 inplace）。
const inplaceAvailable = computed(() =>
    !!form.sourcePath && !/\.csv$/i.test(form.sourcePath) && form.mode !== SPLIT_BY_SHEET
)
const inplaceDisabledReason = computed(() => {
    if (!form.sourcePath) return '请先选择源文件'
    if (/\.csv$/i.test(form.sourcePath)) return 'CSV 源不支持写回；请用新文件输出'
    if (form.mode === SPLIT_BY_SHEET) return '按 Sheet 拆分时不可用：源里每个 Sheet 已经是结果。请改用按行数 / 列值 / 关键词。'
    return ''
})
const isInplace = computed(() => inplaceAvailable.value && form.outputTarget === 'inplace_sheets')

watch(inplaceAvailable, (v) => {
    if (!v && form.outputTarget === 'inplace_sheets') form.outputTarget = 'new_files'
})

// --- 高级筛选（仅 by_keyword 模式） ---
const advFilterAvailable = computed(() => form.mode === SPLIT_BY_KEYWORD)
const activeFilterCount = computed(() => countActiveConditions(form.advancedFilter))
const hasActiveFilter = computed(() => activeFilterCount.value > 0)
// 没关键词但有筛选 → per_keyword 没意义，自动降级 merged
const filterOnlyMode = computed(() =>
    advFilterAvailable.value && !form.keywordsRaw.trim() && hasActiveFilter.value
)
watch(filterOnlyMode, (v) => {
    if (v && form.strategy === OUTPUT_PER_KEYWORD) form.strategy = OUTPUT_MERGED
})
const advFilterOpen = ref(false)
const presetSaving = ref(false)
const presetNewName = ref('')
const presetSelected = ref('')

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

// --- 高级筛选预设管理（仅 by_keyword 模式有效，跟 ExtractView 行为一致） ---
function applyPreset(name) {
    const p = (form.advancedFilterPresets || []).find(x => x.name === name)
    if (!p) return
    form.advancedFilter = {
        mode: p.mode || FILTER_MODE_ALL,
        conditions: (p.conditions || []).map(c => ({ ...c })),
    }
    presetSelected.value = name
    showToast(`已应用预设：${name}`, 'info')
}
function startSavePreset() {
    if (!hasActiveFilter.value) {
        showToast('当前没有任何条件，无需保存', 'warn')
        return
    }
    presetSaving.value = true
    presetNewName.value = presetSelected.value || ''
}
function cancelSavePreset() {
    presetSaving.value = false
    presetNewName.value = ''
}
function commitSavePreset() {
    const name = (presetNewName.value || '').trim()
    if (!name) {
        showToast('预设名不能为空', 'warn')
        return
    }
    const list = [...(form.advancedFilterPresets || [])]
    const idx = list.findIndex(p => p.name === name)
    const snap = {
        name,
        mode: form.advancedFilter.mode,
        conditions: (form.advancedFilter.conditions || [])
            .filter(isMeaningfulCondition)
            .map(c => ({ ...c })),
    }
    if (idx >= 0) list[idx] = snap; else list.push(snap)
    form.advancedFilterPresets = list
    presetSelected.value = name
    presetSaving.value = false
    presetNewName.value = ''
    showToast(idx >= 0 ? `已更新预设：${name}` : `已保存预设：${name}`, 'info')
}
function deletePreset() {
    const name = presetSelected.value
    if (!name) return
    if (!confirm(`确认删除预设 "${name}"？`)) return
    form.advancedFilterPresets = (form.advancedFilterPresets || []).filter(p => p.name !== name)
    presetSelected.value = ''
    showToast(`已删除预设：${name}`, 'info')
}
function buildAdvancedFilterDTO() {
    if (!advFilterAvailable.value) return null
    const sp = form.advancedFilter
    if (!sp) return null
    const conds = (sp.conditions || []).filter(isMeaningfulCondition).map(c => ({ ...c }))
    if (conds.length === 0) return null
    return { mode: sp.mode || FILTER_MODE_ALL, conditions: conds }
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
        // 关键词或高级筛选至少有一个
        if (!form.keywordsRaw.trim() && !hasActiveFilter.value) {
            return showToast('请至少填写一个关键词或一条高级筛选条件', 'warn')
        }
        if (form.keywordsRaw.trim() && !form.exact && !form.contains && !form.pinyin) {
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
            advancedFilter: buildAdvancedFilterDTO(),
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
            <div class="strip strip-inline" style="margin-top:8px">
                <span class="strip-title">输出目标</span>
                <div class="seg" role="tablist">
                    <button type="button" :class="['seg-btn', { active: form.outputTarget === 'new_files' }]"
                            @click="form.outputTarget = 'new_files'">📄 新文件</button>
                    <button type="button"
                            :class="['seg-btn', { active: form.outputTarget === 'inplace_sheets', disabled: !inplaceAvailable }]"
                            :disabled="!inplaceAvailable"
                            :title="inplaceDisabledReason"
                            @click="inplaceAvailable && (form.outputTarget = 'inplace_sheets')">📑 写回源文件新 Sheet</button>
                </div>
                <label v-if="isInplace" class="keep-images" style="margin-left:auto">
                    <input type="checkbox" v-model="form.backupSource" /> 写回前先备份源文件 (.bak)
                </label>
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
                        <label :class="{ disabled: filterOnlyMode }"
                               :title="filterOnlyMode ? '没填关键词时无法按关键词分组' : ''">
                            <input type="radio" :value="OUTPUT_PER_KEYWORD" v-model="form.strategy"
                                   :disabled="filterOnlyMode" />
                            每个关键词一个文件
                        </label>
                        <label><input type="radio" :value="OUTPUT_MERGED" v-model="form.strategy" />
                            合成一个文件</label>
                    </div>
                </div>

                <!-- 高级筛选：仅 by_keyword 模式显示 -->
                <div class="field adv-filter-field">
                    <details class="adv-filter-details" :open="advFilterOpen"
                             @toggle="e => advFilterOpen = e.target.open">
                        <summary class="adv-filter-summary">
                            <span class="chevron-sm" aria-hidden="true">▶</span>
                            <span class="adv-title">高级筛选</span>
                            <span v-if="hasActiveFilter" class="adv-badge"
                                  :title="`${activeFilterCount} 个条件激活中`">
                                ⚠ {{ activeFilterCount }} 个条件激活
                            </span>
                        </summary>
                        <div class="adv-filter-body">
                            <div class="adv-presets">
                                <span class="field-label" style="margin-right:8px">预设：</span>
                                <select v-model="presetSelected" class="name-select"
                                        @change="presetSelected && applyPreset(presetSelected)">
                                    <option value="">— 选择预设 —</option>
                                    <option v-for="p in form.advancedFilterPresets" :key="p.name" :value="p.name">{{ p.name }}</option>
                                </select>
                                <button v-if="!presetSaving" type="button" class="btn-mini" @click="startSavePreset">💾 另存为</button>
                                <button v-if="presetSelected" type="button" class="btn-mini btn-danger" @click="deletePreset">🗑 删除</button>
                                <template v-if="presetSaving">
                                    <input type="text" v-model="presetNewName" placeholder="预设名"
                                           class="name-input" style="width:160px"
                                           @keyup.enter="commitSavePreset" @keyup.esc="cancelSavePreset" />
                                    <button type="button" class="btn-mini btn-primary" @click="commitSavePreset">保存</button>
                                    <button type="button" class="btn-mini" @click="cancelSavePreset">取消</button>
                                </template>
                            </div>
                            <AdvancedFilter v-model="form.advancedFilter" :columns="previewState.columns" />
                        </div>
                    </details>
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
.seg-btn:hover:not(:disabled) { background: var(--surface-hover); color: var(--text); }
.seg-btn:disabled { opacity: 0.4; cursor: not-allowed; }
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
.radio-group label.disabled,
.radio-group label.disabled input { cursor: not-allowed; opacity: 0.5; }
.actions { display: flex; justify-content: flex-end; padding-top: 4px; }

/* 高级筛选内嵌区（比独立 Collapsible 轻量） */
.adv-filter-field {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    padding: 6px 10px;
}
.adv-filter-summary {
    list-style: none;
    cursor: pointer;
    padding: 4px 0;
    display: flex;
    align-items: center;
    gap: 8px;
    user-select: none;
    font-size: 13px;
    font-weight: 600;
    color: var(--text);
}
.adv-filter-summary::-webkit-details-marker { display: none; }
.chevron-sm {
    display: inline-block;
    transition: transform var(--t-base) var(--ease);
    color: var(--text-tertiary);
    font-size: 9px;
    width: 12px;
}
.adv-filter-details[open] .chevron-sm { transform: rotate(90deg); }
.adv-title { flex: 0 0 auto; }
.adv-filter-body {
    padding-top: 6px;
    border-top: 1px dashed var(--border);
    margin-top: 4px;
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.adv-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1px 8px;
    background: #fff4e0;
    color: #c97a08;
    border: 1px solid #f0c989;
    border-radius: 10px;
    font-size: 11px;
    font-weight: 600;
    user-select: none;
}
.adv-presets {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
}
.btn-mini {
    padding: 3px 10px;
    font-size: 12px;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    color: var(--text-secondary);
    border-radius: 4px;
    cursor: pointer;
}
.btn-mini:hover { background: var(--surface-hover); color: var(--text); }
.btn-mini.btn-primary {
    background: var(--accent);
    color: #fff;
    border-color: var(--accent);
}
.btn-mini.btn-danger {
    color: var(--danger, #c43f3f);
    border-color: var(--danger, #c43f3f);
}
.name-input {
    height: 26px;
    padding: 0 8px;
    font-size: 13px;
    border: 1px solid var(--border-strong);
    border-radius: 4px;
}

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
