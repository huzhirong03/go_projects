package filter

// types.go：高级筛选的领域类型定义。
//
// 定位：跟"关键词命中（matcher）"是平级、互不耦合的两个谓词模块。
// extractor 主循环里组合 `if matcher.Hit(row) && filter.Apply(row) { emit }`。
//
// 命名约定：
//   - Op: 单条件操作符（字符串常量，跟前端 1:1 对应；前端展示名是中文，这里用英文 key）
//   - Mode: 多条件组合方式（all = AND；any = OR）
//   - Condition: 单条件 DTO（前端 → 后端透传的形态）
//   - Spec: 整个筛选规格（Mode + []Condition）
//   - Filter: Spec 编译后的可执行实例（含列索引、缓存的正则等）

// Op 操作符 key。前端下拉的 value 必须跟这里一致。
type Op string

const (
	// 比较类（数值/字符串自适应）
	OpEqual              Op = "eq"
	OpNotEqual           Op = "ne"
	OpGreaterThan        Op = "gt"
	OpLessThan           Op = "lt"
	OpGreaterOrEqual     Op = "ge"
	OpLessOrEqual        Op = "le"
	OpBetween            Op = "between"     // 双值：value, value2 都需要；闭区间
	OpNotBetween         Op = "not_between" // 闭区间外
	// 文本类
	OpContains    Op = "contains"
	OpNotContains Op = "not_contains"
	OpStartsWith  Op = "starts_with"
	OpEndsWith    Op = "ends_with"
	// 集合类（value 为逗号分隔列表）
	OpIn    Op = "in"
	OpNotIn Op = "not_in"
	// 存在性
	OpEmpty    Op = "empty"
	OpNotEmpty Op = "not_empty"
	// 日期范围（value/value2 都是 yyyy-mm-dd 文本）
	OpDateBetween    Op = "date_between"
	OpDateNotBetween Op = "date_not_between"
	// 格式类（用预设正则；UI 上显示为"是手机号 / 是邮箱 / ..."业务化命名）
	// 单一 Op 配 Format 字段区分类型；这样未来加新格式只用扩 presets 表。
	OpMatchFormat    Op = "match_format"
	OpNotMatchFormat Op = "not_match_format"
)

// Mode 多条件组合方式。
type Mode string

const (
	ModeAll Mode = "all" // 全部满足（AND）
	ModeAny Mode = "any" // 任一满足（OR）
)

// Condition 单条件。前端 → 后端 DTO。
//
// 字段约定：
//   - Column: 列名（按表头匹配，大小写不敏感 + 去首尾空格）
//   - Op: 操作符 key
//   - Value: 主值（多数 Op 用这一个）
//   - Value2: 副值（仅 between / date_between 用）
//   - Format: Op=match_format/not_match_format 时指定预设名（如 "phone_cn"）
//
// 跨语言字段命名：JSON tag 跟 wails/前端的 camelCase 习惯一致。
type Condition struct {
	Column string `json:"column"`
	Op     Op     `json:"op"`
	Value  string `json:"value"`
	Value2 string `json:"value2,omitempty"`
	Format string `json:"format,omitempty"`
}

// Spec 整个筛选规格。空 Conditions 视作"无筛选"，调用方可短路跳过。
type Spec struct {
	Mode       Mode        `json:"mode"`
	Conditions []Condition `json:"conditions"`
}

// IsEmpty 判定 spec 是否实际无效（条件为空 / 全为占位空条件）。
// extractor 集成时用此判定决定是否绕过 filter.Apply。
func (s Spec) IsEmpty() bool {
	if len(s.Conditions) == 0 {
		return true
	}
	for _, c := range s.Conditions {
		if c.Column != "" && c.Op != "" {
			return false
		}
	}
	return true
}
