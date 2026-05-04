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
// hint 字段：一句话说明 + 例子（控制在 50 字以内，适配原生 tooltip）
// 前端给 <option :title="o.hint"> ，hover 选项 ~1 秒后浏览器会弹出提示。
export const OPERATOR_OPTIONS = [
    // 比较类（数值/字符串）
    { value: OP_EQ, label: '等于', group: '比较',
      hint: '列值精确等于输入值。例：总分 等于 500' },
    { value: OP_NE, label: '不等于', group: '比较',
      hint: '列值不等于输入值。例：状态 不等于 已取消' },
    { value: OP_GT, label: '大于', group: '比较',
      hint: '列值 > 输入值（仅数值列）。例：价格 大于 100' },
    { value: OP_LT, label: '小于', group: '比较',
      hint: '列值 < 输入值（仅数值列）。例：库存 小于 10' },
    { value: OP_GE, label: '大于等于', group: '比较',
      hint: '列值 ≥ 输入值。例：年龄 大于等于 18' },
    { value: OP_LE, label: '小于等于', group: '比较',
      hint: '列值 ≤ 输入值。例：价格 小于等于 999' },
    { value: OP_BETWEEN, label: '区间内', group: '比较',
      hint: '列值在 [最小, 最大] 闭区间内（含端点）。例：总分 区间内 400~500' },
    { value: OP_NOT_BETWEEN, label: '区间外', group: '比较',
      hint: '列值不在区间内（太小或太大）。常用于抓异常值' },

    // 文本类
    { value: OP_CONTAINS, label: '包含', group: '文本',
      hint: '列值中出现了输入文本。例：地址 包含 "深圳"' },
    { value: OP_NOT_CONTAINS, label: '不包含', group: '文本',
      hint: '列值中没有输入文本。例：备注 不包含 "退货"' },
    { value: OP_STARTS_WITH, label: '开头是', group: '文本',
      hint: '列值以输入文本开头。例：手机号 开头是 "138"' },
    { value: OP_ENDS_WITH, label: '结尾是', group: '文本',
      hint: '列值以输入文本结尾。例：文件名 结尾是 ".xlsx"' },

    // 集合类
    { value: OP_IN, label: '在列表里', group: '集合',
      hint: '列值精确等于多个值中任一个（逗号/分号/换行分隔）。例：省份 在列表里 "广东,浙江,江苏"' },
    { value: OP_NOT_IN, label: '不在列表里', group: '集合',
      hint: '列值不属于列表（黑名单场景）。例：客户ID 不在列表里 "C001,C007"' },

    // 存在性
    { value: OP_EMPTY, label: '为空', group: '存在性',
      hint: '单元格未填或全是空白字符。不需要输入值。用于找漏填' },
    { value: OP_NOT_EMPTY, label: '不为空', group: '存在性',
      hint: '单元格有内容。不需要输入值。例：跟进人 不为空' },

    // 日期
    { value: OP_DATE_BETWEEN, label: '日期范围内', group: '日期',
      hint: '日期在 [起, 止] 闭区间内（按天比较）。输入两个 yyyy-mm-dd。例：下单日期 在 2026-01-01 ~ 2026-03-31' },
    { value: OP_DATE_NOT_BETWEEN, label: '日期范围外', group: '日期',
      hint: '日期不在区间内（过早或过晚）' },

    // 格式（背后是预设正则；UI 只暴露中文业务名，下面 FORMAT_OPTIONS 提供选择）
    { value: OP_MATCH_FORMAT, label: '是某种格式', group: '格式',
      hint: '列值匹配预设格式（手机号/邮箱/身份证等）。选中后右侧会出现格式选择框' },
    { value: OP_NOT_MATCH_FORMAT, label: '不是某种格式', group: '格式',
      hint: '列值不匹配预设格式。常用于挑出脏数据。例：手机号列 不是某种格式 "手机号"' },
]

// 预设格式下拉选项（仅在选了 match_format / not_match_format 时显示）
export const FORMAT_OPTIONS = [
    { value: FORMAT_PHONE_CN, label: '手机号',
      hint: '中国 11 位手机号（1[3-9] 开头）。例：13800138000' },
    { value: FORMAT_EMAIL, label: '邮箱',
      hint: '标准 email 格式 xxx@yyy.zz' },
    { value: FORMAT_URL, label: '网址',
      hint: 'http(s)://... 或 www. 开头的网址' },
    { value: FORMAT_ID_CARD_18, label: '18 位身份证',
      hint: '18 位中国居民身份证号（含校验位）' },
    { value: FORMAT_DATE, label: '日期 (YYYY-MM-DD / YYYY/MM/DD)',
      hint: '例：2026-05-05 或 2026/05/05' },
    { value: FORMAT_DIGITS_ONLY, label: '纯数字',
      hint: '只含 0-9 的字符串（用于订单号/学号等纯数字列）' },
    { value: FORMAT_HAS_CHINESE, label: '含中文',
      hint: '列值中出现至少一个汉字（用于抽出中文说明/备注行）' },
    { value: FORMAT_POSTCODE_CN, label: '6 位邮政编码',
      hint: '中国 6 位邮编。例：518000' },
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
