// 高级筛选操作符 / 模式 / 预设格式常量。
// 后端对应 internal/filter/types.go + presets.go；改这里前请同步后端。

// 匹配模式
export const FILTER_MODE_ALL = 'all'
export const FILTER_MODE_ANY = 'any'

// --- 操作符 key（跟后端严格一致） ---
export const OP_EQ = 'eq'
export const OP_NE = 'ne'
export const OP_GT = 'gt'
export const OP_LT = 'lt'
export const OP_GE = 'ge'
export const OP_LE = 'le'
export const OP_BETWEEN = 'between'
export const OP_NOT_BETWEEN = 'not_between'
export const OP_CONTAINS = 'contains'
export const OP_NOT_CONTAINS = 'not_contains'
export const OP_STARTS_WITH = 'starts_with'
export const OP_ENDS_WITH = 'ends_with'
export const OP_IN = 'in'
export const OP_NOT_IN = 'not_in'
export const OP_EMPTY = 'empty'
export const OP_NOT_EMPTY = 'not_empty'
export const OP_DATE_BETWEEN = 'date_between'
export const OP_DATE_NOT_BETWEEN = 'date_not_between'
export const OP_MATCH_FORMAT = 'match_format'
export const OP_NOT_MATCH_FORMAT = 'not_match_format'

// --- 预设格式（跟后端 internal/filter/presets.go 一致） ---
export const FORMAT_PHONE_CN = 'phone_cn'
export const FORMAT_EMAIL = 'email'
export const FORMAT_URL = 'url'
export const FORMAT_ID_CARD_18 = 'id_card_18'
export const FORMAT_DATE = 'date'
export const FORMAT_DIGITS_ONLY = 'digits_only'
export const FORMAT_HAS_CHINESE = 'has_chinese'
export const FORMAT_POSTCODE_CN = 'postcode_cn'

// 操作符值的"形态"——告诉前端给这个 op 应渲染什么类型的输入：
//   - 'single':  单个文本输入框
//   - 'double':  两个文本输入框（min/max 或 from/to）
//   - 'date_double': 两个日期选择器
//   - 'none':    无值（empty/not_empty）
//   - 'format':  没有值，只需要选预设格式（match_format/not_match_format）
//   - 'list':    多个值（IN/NOT IN，单个 textarea，逗号/分号/换行分隔）
export const OP_VALUE_KIND = {
    [OP_EQ]: 'single',
    [OP_NE]: 'single',
    [OP_GT]: 'single',
    [OP_LT]: 'single',
    [OP_GE]: 'single',
    [OP_LE]: 'single',
    [OP_BETWEEN]: 'double',
    [OP_NOT_BETWEEN]: 'double',
    [OP_CONTAINS]: 'single',
    [OP_NOT_CONTAINS]: 'single',
    [OP_STARTS_WITH]: 'single',
    [OP_ENDS_WITH]: 'single',
    [OP_IN]: 'list',
    [OP_NOT_IN]: 'list',
    [OP_EMPTY]: 'none',
    [OP_NOT_EMPTY]: 'none',
    [OP_DATE_BETWEEN]: 'date_double',
    [OP_DATE_NOT_BETWEEN]: 'date_double',
    [OP_MATCH_FORMAT]: 'format',
    [OP_NOT_MATCH_FORMAT]: 'format',
}

// 操作符在下拉里展示的"中文人话"。按业务相关性分组并排序。
// 同一组用 group 字段区分，前端可分组显示。
export const OPERATOR_OPTIONS = [
    // 比较类（数值/字符串）
    { value: OP_EQ, label: '等于', group: '比较' },
    { value: OP_NE, label: '不等于', group: '比较' },
    { value: OP_GT, label: '大于', group: '比较' },
    { value: OP_LT, label: '小于', group: '比较' },
    { value: OP_GE, label: '大于等于', group: '比较' },
    { value: OP_LE, label: '小于等于', group: '比较' },
    { value: OP_BETWEEN, label: '区间内', group: '比较' },
    { value: OP_NOT_BETWEEN, label: '区间外', group: '比较' },

    // 文本类
    { value: OP_CONTAINS, label: '包含', group: '文本' },
    { value: OP_NOT_CONTAINS, label: '不包含', group: '文本' },
    { value: OP_STARTS_WITH, label: '开头是', group: '文本' },
    { value: OP_ENDS_WITH, label: '结尾是', group: '文本' },

    // 集合类
    { value: OP_IN, label: '在列表里', group: '集合' },
    { value: OP_NOT_IN, label: '不在列表里', group: '集合' },

    // 存在性
    { value: OP_EMPTY, label: '为空', group: '存在性' },
    { value: OP_NOT_EMPTY, label: '不为空', group: '存在性' },

    // 日期
    { value: OP_DATE_BETWEEN, label: '日期范围内', group: '日期' },
    { value: OP_DATE_NOT_BETWEEN, label: '日期范围外', group: '日期' },

    // 格式（背后是预设正则；UI 只暴露中文业务名，下面 FORMAT_OPTIONS 提供选择）
    { value: OP_MATCH_FORMAT, label: '是某种格式', group: '格式' },
    { value: OP_NOT_MATCH_FORMAT, label: '不是某种格式', group: '格式' },
]

// 预设格式下拉选项（仅在选了 match_format / not_match_format 时显示）
export const FORMAT_OPTIONS = [
    { value: FORMAT_PHONE_CN, label: '手机号' },
    { value: FORMAT_EMAIL, label: '邮箱' },
    { value: FORMAT_URL, label: '网址' },
    { value: FORMAT_ID_CARD_18, label: '18 位身份证' },
    { value: FORMAT_DATE, label: '日期 (YYYY-MM-DD / YYYY/MM/DD)' },
    { value: FORMAT_DIGITS_ONLY, label: '纯数字' },
    { value: FORMAT_HAS_CHINESE, label: '含中文' },
    { value: FORMAT_POSTCODE_CN, label: '6 位邮政编码' },
]

// 创建一个空白条件占位
export function newCondition(column = '') {
    return { column, op: OP_CONTAINS, value: '', value2: '', format: '' }
}

// 当前条件是否"非空"——即用户实际填了内容
export function isMeaningfulCondition(c) {
    if (!c || !c.column || !c.op) return false
    const kind = OP_VALUE_KIND[c.op]
    if (kind === 'none') return true
    if (kind === 'format') return !!c.format
    if (kind === 'double' || kind === 'date_double') return !!c.value && !!c.value2
    if (kind === 'list') return !!(c.value || '').trim()
    return !!(c.value || '').trim()
}

// 计算当前 spec 里有效条件数（用于角标提示）
export function countActiveConditions(spec) {
    if (!spec || !spec.conditions) return 0
    let n = 0
    for (const c of spec.conditions) if (isMeaningfulCondition(c)) n++
    return n
}
