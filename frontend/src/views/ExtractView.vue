<script setup>
import { reactive, watch, onMounted, toRaw, ref, nextTick, computed } from 'vue'
import PathPicker from '../components/PathPicker.vue'
import ProgressPanel from '../components/ProgressPanel.vue'
import SheetSelector from '../components/SheetSelector.vue'
import Collapsible from '../components/Collapsible.vue'
import AdvancedFilter from '../components/AdvancedFilter.vue'
import { previewFolder, startExtract } from '../api/extract.js'
import { task, startTask } from '../stores/task.js'
import { showToast } from '../stores/toast.js'
import { getViewConfig, saveViewConfig } from '../stores/config.js'
import { LogPrint } from '../../wailsjs/runtime/runtime'
import { LogStartup } from '../../wailsjs/go/main/App'
import {
    OUTPUT_PER_KEYWORD, OUTPUT_MERGED, OUTPUT_PER_SOURCE,
} from '../types/events.js'
import { FILTER_MODE_ALL, countActiveConditions, isMeaningfulCondition } from '../types/filter.js'

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
    // 高级筛选 V1.5+：在关键词命中后做二次条件过滤。
    //   advancedFilter: 当前编辑中的条件
    //   advancedFilterPresets: 用户保存的命名预设 [{name, mode, conditions}]
    advancedFilter: { mode: FILTER_MODE_ALL, conditions: [] },
    advancedFilterPresets: [],
    // 去重（V1.1+ / V1.2+）：未启用时不提交任何 dedup 字段，保持后端零回归。
    //   dedupEnabled:      仅 UI 层开关
    //   dedupColumn:       主列（列 1，必填）
    //   dedupColumn2/3:    次要列（可选，构成多列组合去重 key）
    //   dedupIgnoreSpace:  忽略前后空白
    //   dedupIgnoreCase:   忽略大小写（英文生效）
    dedupEnabled: false,
    dedupColumn: '',
    dedupColumn2: '',
    dedupColumn3: '',
    dedupIgnoreSpace: false,
    dedupIgnoreCase: false,
    // 折叠状态（默认全展开，去重区块默认折叠避免占屏）
    foldPaths: false,
    foldKeywords: false,
    foldRange: false,
    foldDedup: true,
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

// --- 高级筛选 ---
// 当前激活的条件数。
const activeFilterCount = computed(() => countActiveConditions(form.advancedFilter))
const hasActiveFilter = computed(() => activeFilterCount.value > 0)

// --- 去重 ---
// effectiveDedupColumns 返回实际参与去重的列名列表（按 UI 顺序，去重，过滤空值）。
// submit 时打包为 dedupColumns；UI 上用于动态提示。
const effectiveDedupColumns = computed(() => {
    if (!form.dedupEnabled) return []
    const raw = [form.dedupColumn, form.dedupColumn2, form.dedupColumn3]
    const seen = new Set()
    const out = []
    for (const c of raw) {
        const t = (c || '').trim()
        if (!t || seen.has(t)) continue
        seen.add(t)
        out.push(t)
    }
    return out
})
// 检测：同一列被选多次（方便 UI 提示）。
const dedupHasDuplicate = computed(() => {
    if (!form.dedupEnabled) return false
    const raw = [form.dedupColumn, form.dedupColumn2, form.dedupColumn3].filter(c => (c || '').trim())
    return new Set(raw).size !== raw.length
})
// 描述文本：单列就叫「à xxxá」，多列串一串「à a+b+cá」。
const dedupColsDesc = computed(() => {
    const cols = effectiveDedupColumns.value
    if (cols.length === 0) return ''
    if (cols.length === 1) return `「${cols[0]}」列`
    return `「${cols.join(' + ')}」列组合`
})
// dedupHint 根据当前策略动态显示去重范围说明。
const dedupHint = computed(() => {
    const desc = dedupColsDesc.value
    if (!desc) return ''
    switch (form.strategy) {
        case OUTPUT_MERGED:
            return `所有文件合并后，按${desc}全局去重`
        case OUTPUT_PER_KEYWORD:
            return `每个关键词的输出文件内独立按${desc}去重`
        case OUTPUT_PER_SOURCE:
            return `每个源文件的输出文件内独立按${desc}去重`
        default:
            return `按${desc}去重，保留首次出现的行`
    }
})
// inplace 时也要给提示
const dedupHintInplace = computed(() => {
    const desc = dedupColsDesc.value
    if (!desc) return ''
    return `新 Sheet 内独立按${desc}去重`
})
// 没关键词但有筛选 → per_keyword 没意义，自动降级 merged（UI 上 per_keyword 单选会灰掉）。
const filterOnlyMode = computed(() => !form.keywordsRaw.trim() && hasActiveFilter.value)
watch(filterOnlyMode, (v) => {
    if (v && form.strategy === OUTPUT_PER_KEYWORD) form.strategy = OUTPUT_MERGED
})
// 折叠状态本身**不**持久化；进入页面时按"智能默认"决定：
//   - 当前已有激活条件 → 折叠 + 角标提醒
//   - 无激活条件 → 展开（让用户发现这个功能）
// 用 ref 而不是 form 字段，避免被 PERSIST_KEYS 自动保存。
const advFilterOpen = ref(false)
// 命名预设临时输入：用户点"另存为"会弹一个内联文本框
const presetSaving = ref(false)
const presetNewName = ref('')
const presetSelected = ref('') // 当前下拉选中的预设名

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
    // 智能默认：恢复完配置后判定。无激活条件 → 展开；有 → 收起 + 靠角标提示。
    advFilterOpen.value = activeFilterCount.value === 0
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

// --- 高级筛选预设管理 ---
function applyPreset(name) {
    const p = (form.advancedFilterPresets || []).find(x => x.name === name)
    if (!p) return
    // 深拷贝避免共享引用
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
        // 只保存有意义的条件，过滤占位空行
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

// 把当前条件转成提交给后端的 DTO；空（占位行 / 无条件）一律返回 null。
function buildAdvancedFilterDTO() {
    const sp = form.advancedFilter
    if (!sp) return null
    const conds = (sp.conditions || []).filter(isMeaningfulCondition).map(c => ({ ...c }))
    if (conds.length === 0) return null
    return { mode: sp.mode || FILTER_MODE_ALL, conditions: conds }
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
    // 关键词或高级筛选必须至少有一个；两个都为空 = 无规则。
    if (!form.keywordsRaw.trim() && !hasActiveFilter.value) {
        return showToast('请至少填写一个关键词或一条高级筛选条件', 'warn')
    }
    if (form.keywordsRaw.trim() && !form.exact && !form.contains) {
        return showToast('请至少选择一种匹配模式', 'warn')
    }
    if (previewState.sheets.length > 1 && form.sheetNames.length === 0) {
        return showToast('请至少勾选一个要处理的 Sheet', 'warn')
    }
    // 去重启用但未选列 → 拦截（列 1 必填）
    if (form.dedupEnabled && !form.dedupColumn) {
        return showToast('启用去重时必须选择至少一列作为去重依据', 'warn')
    }
    // 多列组合时禁止重复选同一列
    if (form.dedupEnabled && dedupHasDuplicate.value) {
        return showToast('去重多列组合里不能重复选择同一列', 'warn')
    }

    try {
        const handle = await startExtract({
            folderPath: sourcePath.value,
            filenamePrefix: form.filenamePrefix || '',
            keywordsRaw: form.keywordsRaw,
            exact: form.exact,
            contains: form.contains,
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
            advancedFilter: buildAdvancedFilterDTO(),
            // 未启用时传空/空数组/false，后端按零回归路径走
            dedupColumn: form.dedupEnabled ? form.dedupColumn : '',
            dedupColumns: form.dedupEnabled ? effectiveDedupColumns.value : [],
            dedupIgnoreSpace: form.dedupEnabled && form.dedupIgnoreSpace,
            dedupIgnoreCase: form.dedupEnabled && form.dedupIgnoreCase,
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
                <label v-if="isInplace" class="keep-images" style="margin-left:auto"
                       title="备份命名：原文件名_备份_年月日_时分秒.xlsx（同目录）。双击可直接用 Excel 打开；多次跑不会互相覆盖。">
                    <input type="checkbox" v-model="form.backupSource" /> 写回前先备份源文件
                </label>
            </div>
        </Collapsible>

        <Collapsible title="关键词与匹配" :open="!form.foldKeywords" @update:open="v => form.foldKeywords = !v">
            <div class="row-2col">
                <div class="field">
                    <label class="field-label">关键词（逗号/空格/顿号分隔）</label>
                    <textarea v-model="form.keywordsRaw" rows="2"
                              placeholder="例如：口红, 眼影, 粉底"></textarea>
                </div>
                <div class="field">
                    <label class="field-label">匹配模式</label>
                    <div class="inline-group match-group">
                        <label><input type="checkbox" v-model="form.exact" /> 精准</label>
                        <label><input type="checkbox" v-model="form.contains" /> 包含</label>
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

        <Collapsible title="去重" :open="!form.foldDedup" @update:open="v => form.foldDedup = !v">
            <template #head-extra>
                <span v-if="form.dedupEnabled && effectiveDedupColumns.length" class="adv-badge"
                      :title="isInplace ? dedupHintInplace : dedupHint">
                    ✓ 按{{ dedupColsDesc }}去重
                </span>
            </template>
            <div class="field">
                <label class="keep-images">
                    <input type="checkbox" v-model="form.dedupEnabled" />
                    启用去重（按指定列组合去重，保留首次出现的行）
                </label>
            </div>
            <template v-if="form.dedupEnabled">
                <div class="field">
                    <label class="field-label">去重列 1</label>
                    <select v-model="form.dedupColumn" class="name-select">
                        <option value="">— 选择一列（必选）—</option>
                        <option v-for="c in previewState.columns" :key="c" :value="c">{{ c }}</option>
                    </select>
                    <span v-if="!form.dedupColumn" class="field-hint"
                          style="margin-left:10px;color:var(--warn,#d97706)">
                        必须至少选一列
                    </span>
                </div>
                <div class="field">
                    <label class="field-label">去重列 2</label>
                    <select v-model="form.dedupColumn2" class="name-select">
                        <option value="">— （可选）—</option>
                        <option v-for="c in previewState.columns" :key="c" :value="c">{{ c }}</option>
                    </select>
                </div>
                <div class="field">
                    <label class="field-label">去重列 3</label>
                    <select v-model="form.dedupColumn3" class="name-select">
                        <option value="">— （可选）—</option>
                        <option v-for="c in previewState.columns" :key="c" :value="c">{{ c }}</option>
                    </select>
                </div>
                <div v-if="dedupHasDuplicate" class="field">
                    <span class="field-hint" style="color:var(--warn,#d97706)">
                        ⚠ 同一列被选了多次，会被自动去重；请避免重复选择。
                    </span>
                </div>
                <div class="field">
                    <label class="field-label">归一化</label>
                    <div class="inline-group">
                        <label><input type="checkbox" v-model="form.dedupIgnoreSpace" /> 忽略前后空白</label>
                        <label><input type="checkbox" v-model="form.dedupIgnoreCase" /> 忽略大小写</label>
                    </div>
                    <span class="field-hint" style="margin-left:10px">
                        💡 只去首尾空白（不去中间）；大小写仅对英文字母生效
                    </span>
                </div>
                <div v-if="dedupHint || dedupHintInplace" class="field">
                    <span class="field-hint" style="color:var(--primary,#3b82f6)">
                        💡 {{ isInplace ? dedupHintInplace : dedupHint }}
                    </span>
                </div>
            </template>
            <div class="field">
                <span class="field-hint">
                    想看重复值而不删除？用 Excel 「开始 → 条件格式 → 突出显示单元格规则 → 重复值」更方便。
                </span>
            </div>
        </Collapsible>

        <Collapsible title="高级筛选" :open="advFilterOpen" @update:open="v => advFilterOpen = v">
            <template #head-extra>
                <span v-if="hasActiveFilter" class="adv-badge"
                      :title="`${activeFilterCount} 个条件激活中`">
                    ⚠ {{ activeFilterCount }} 个条件激活
                </span>
            </template>
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
        </Collapsible>

        <div class="strip strip-output">
            <div class="strip-row">
                <span class="strip-title">策略</span>
                <div class="inline-group radio-group">
                    <label :class="{ disabled: filterOnlyMode }"
                           :title="filterOnlyMode ? '没填关键词时无法按关键词分组' : ''">
                        <input type="radio" :value="OUTPUT_PER_KEYWORD" v-model="form.strategy"
                               :disabled="filterOnlyMode" />
                        每个关键词一个文件
                    </label>
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
.radio-group label.disabled,
.radio-group label.disabled input { cursor: not-allowed; opacity: 0.5; }

/* 高级筛选角标 */
.adv-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1px 8px;
    margin-left: 6px;
    background: #fff4e0;
    color: #c97a08;
    border: 1px solid #f0c989;
    border-radius: 10px;
    font-size: 11px;
    font-weight: 600;
    user-select: none;
}

/* 预设管理条 */
.adv-presets {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    padding: 4px 0 10px;
    border-bottom: 1px dashed var(--border, #e0e0e0);
    margin-bottom: 8px;
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
