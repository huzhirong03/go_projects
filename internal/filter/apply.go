package filter

// apply.go：各 predicate 的运行期求值 + Filter 的总体 Apply。
//
// 设计原则：
//   - 每个谓词独立、无共享状态；Apply 纯函数风格便于并发（extractor 可多 goroutine 处理行）
//   - 所有取 cell 值通过 getCell 辅助：越界返回 ""，避免每处单独判空
//   - 比较类 op 的数值/字符串自适应：优先用数值比较（解析成功），解析失败降级字符串字典序

import (
	"regexp"
	"strings"
	"time"
)

// Apply 对一行数据求值。Filter 为 nil 或 IsZero → 一律 true（不过滤）。
func (f *Filter) Apply(row []string) bool {
	if f == nil || f.IsZero() {
		return true
	}
	switch f.mode {
	case ModeAny:
		for _, p := range f.preds {
			if p.eval(row) {
				return true
			}
		}
		return false
	default: // ModeAll
		for _, p := range f.preds {
			if !p.eval(row) {
				return false
			}
		}
		return true
	}
}

// getCell 越界安全地取 row[idx]，并 trim 一次。
func getCell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

// --- 各谓词实现 ---

// eqPred: 相等 / 不相等（字符串比较；trim 后）
type eqPred struct {
	col int
	op  Op
	val string
}

func (p *eqPred) eval(row []string) bool {
	cell := getCell(row, p.col)
	target := strings.TrimSpace(p.val)
	eq := cell == target
	if p.op == OpNotEqual {
		return !eq
	}
	return eq
}

// cmpPred: >, <, ≥, ≤
// 优先数值比较；两边都能解析成数则按数值；否则按字符串字典序。
type cmpPred struct {
	col   int
	op    Op
	val   string  // 原值（字符串兜底用）
	num   float64 // 预解析的数值
	numOK bool
}

func (p *cmpPred) eval(row []string) bool {
	cell := getCell(row, p.col)
	if cell == "" {
		return false // 空单元格永远不命中"大于 X"等
	}
	// 严格语义：value 是数值时要求 cell 也能解析为数值；否则不命中（而非字典序降级）。
	// 这样"总分 ≥ 400"不会把 cell="abc" 误判为命中，符合用户直觉。
	if p.numOK {
		v, ok := parseNumber(cell)
		if !ok {
			return false
		}
		return compareNum(v, p.num, p.op)
	}
	// value 本身也不是数值 → 纯字符串字典序（极少见用法，保留以兼容按字符串排序的场景）
	return compareStr(cell, strings.TrimSpace(p.val), p.op)
}

func compareNum(a, b float64, op Op) bool {
	switch op {
	case OpGreaterThan:
		return a > b
	case OpLessThan:
		return a < b
	case OpGreaterOrEqual:
		return a >= b
	case OpLessOrEqual:
		return a <= b
	}
	return false
}

func compareStr(a, b string, op Op) bool {
	switch op {
	case OpGreaterThan:
		return a > b
	case OpLessThan:
		return a < b
	case OpGreaterOrEqual:
		return a >= b
	case OpLessOrEqual:
		return a <= b
	}
	return false
}

// betweenPred: 数值区间 [min, max]；not_between 为补集
type betweenPred struct {
	col      int
	op       Op
	min, max float64
}

func (p *betweenPred) eval(row []string) bool {
	cell := getCell(row, p.col)
	v, ok := parseNumber(cell)
	if !ok {
		return false // 非数值永远不在数值区间里
	}
	in := v >= p.min && v <= p.max
	if p.op == OpNotBetween {
		return !in
	}
	return in
}

// textPred: 包含/不包含/开头是/结尾是（子串；大小写不敏感）
type textPred struct {
	col int
	op  Op
	val string
}

func (p *textPred) eval(row []string) bool {
	cell := strings.ToLower(getCell(row, p.col))
	target := strings.ToLower(strings.TrimSpace(p.val))
	if target == "" {
		// 空 value 无意义：包含空串永远 true；为保行为可预期，按 "不启用" 处理 → true
		return true
	}
	switch p.op {
	case OpContains:
		return strings.Contains(cell, target)
	case OpNotContains:
		return !strings.Contains(cell, target)
	case OpStartsWith:
		return strings.HasPrefix(cell, target)
	case OpEndsWith:
		return strings.HasSuffix(cell, target)
	}
	return false
}

// inPred: 在列表里/不在列表里（字符串精确匹配；trim 后；大小写敏感）
// 若未来要求大小写不敏感可以在 Compile 阶段把 set 全部 ToLower，这里同步 ToLower 查找。
type inPred struct {
	col int
	op  Op
	set map[string]struct{}
}

func (p *inPred) eval(row []string) bool {
	cell := getCell(row, p.col)
	_, ok := p.set[cell]
	if p.op == OpNotIn {
		return !ok
	}
	return ok
}

// emptyPred: 为空/不为空（空 cell + 空串 + 全空白都算空）
type emptyPred struct {
	col int
	op  Op
}

func (p *emptyPred) eval(row []string) bool {
	cell := getCell(row, p.col)
	empty := cell == ""
	if p.op == OpNotEmpty {
		return !empty
	}
	return empty
}

// dateBetweenPred: 日期区间 [from, to]（闭区间；比较到"天"）
type dateBetweenPred struct {
	col      int
	op       Op
	from, to time.Time
}

func (p *dateBetweenPred) eval(row []string) bool {
	cell := getCell(row, p.col)
	t, ok := parseDate(cell)
	if !ok {
		return false
	}
	// 归一化到日粒度（避免小时差干扰）
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	from := time.Date(p.from.Year(), p.from.Month(), p.from.Day(), 0, 0, 0, 0, time.Local)
	to := time.Date(p.to.Year(), p.to.Month(), p.to.Day(), 0, 0, 0, 0, time.Local)
	in := !day.Before(from) && !day.After(to)
	if p.op == OpDateNotBetween {
		return !in
	}
	return in
}

// formatPred: 是 X 格式 / 不是 X 格式（预设正则；cell 已 trim）
type formatPred struct {
	col int
	op  Op
	re  *regexp.Regexp
}

func (p *formatPred) eval(row []string) bool {
	cell := getCell(row, p.col)
	if cell == "" {
		// 空 cell 对"是手机号"一律 false；对"不是手机号"一律 true
		return p.op == OpNotMatchFormat
	}
	m := p.re.MatchString(cell)
	if p.op == OpNotMatchFormat {
		return !m
	}
	return m
}
