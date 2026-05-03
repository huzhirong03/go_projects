package filter

// compile.go：把前端送来的 Spec（条件 DTO）编译成可执行 Filter。
//
// 编译期做的事：
//  1. 校验 Mode；缺失视作 ModeAll
//  2. 校验列名（按 headers 大小写不敏感+trim 匹配），找不到列 → 收集为 missingColumns（不算硬错误，由 caller 决定 skip 还是 fail）
//  3. 校验 Op 合法；预解析数值/日期 value 一次（避免每行重复解析）
//  4. 预编译正则（OpMatchFormat / OpNotMatchFormat）
//
// 设计要点：
//   - 编译失败的"软错误"（列名找不到）放在 Filter.MissingColumns 里，让上层决定如何处理（多文件夹场景可以"该文件跳过 + toast"）。
//   - 编译失败的"硬错误"（Op 未知、数值/日期 value 解析失败、正则不存在）通过返回 error 报上去。

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Filter 是 Spec 编译后的可执行实例。
type Filter struct {
	mode  Mode
	preds []predicate

	// MissingColumns 列出 Spec 里引用但 headers 里找不到的列名（去重后）。
	// 上层（extractor 多文件夹模式）可据此决定整文件跳过 + 警告。
	MissingColumns []string
}

// IsZero 是否等同于"无筛选"。Apply 应短路返回 true（全通过）。
// 注：MissingColumns 非空时也算"零谓词"——但**仅当所有条件都因列丢失而被丢弃时**。
func (f *Filter) IsZero() bool { return len(f.preds) == 0 }

// predicate 内部接口：单条件求值。
// 输入是整行 cell 字符串，自身知道要看哪一列。
type predicate interface {
	eval(row []string) bool
}

// Compile 把 Spec 按 headers 编译。
//
// headers 是当前任务的列表头切片（按 1-based 列序排列；row[i] 的下标对应 headers[i]）。
// 找不到的列名记入 MissingColumns；其它硬错误返回 err。
func Compile(spec Spec, headers []string) (*Filter, error) {
	mode := spec.Mode
	if mode != ModeAll && mode != ModeAny {
		mode = ModeAll
	}
	f := &Filter{mode: mode}

	// 列名 → 索引（大小写不敏感 + trim）。重名取第一个。
	colIdx := buildColumnIndex(headers)

	missing := map[string]struct{}{}

	for _, c := range spec.Conditions {
		if c.Column == "" || c.Op == "" {
			// 占位/空行，跳过
			continue
		}
		idx, ok := colIdx[normalizeColumnKey(c.Column)]
		if !ok {
			missing[c.Column] = struct{}{}
			continue
		}

		p, err := compileCondition(idx, c)
		if err != nil {
			return nil, err
		}
		f.preds = append(f.preds, p)
	}

	if len(missing) > 0 {
		f.MissingColumns = make([]string, 0, len(missing))
		for k := range missing {
			f.MissingColumns = append(f.MissingColumns, k)
		}
	}
	return f, nil
}

// buildColumnIndex 把 headers 转成 normalizedKey → index 映射。
func buildColumnIndex(headers []string) map[string]int {
	m := make(map[string]int, len(headers))
	for i, h := range headers {
		k := normalizeColumnKey(h)
		if k == "" {
			continue
		}
		if _, exists := m[k]; !exists {
			m[k] = i
		}
	}
	return m
}

// normalizeColumnKey: 小写 + 去首尾空格。空字符串保持空。
func normalizeColumnKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// compileCondition 把单条件编译成 predicate，并预解析 value。
func compileCondition(colIdx int, c Condition) (predicate, error) {
	switch c.Op {
	case OpEqual, OpNotEqual:
		return &eqPred{col: colIdx, op: c.Op, val: c.Value}, nil

	case OpGreaterThan, OpLessThan, OpGreaterOrEqual, OpLessOrEqual:
		// 这些 op 优先按数值比较，失败回退字符串字典序。
		// 编译时尝试解析一次 value 缓存；失败也允许（运行时按字符串）。
		num, ok := parseNumber(c.Value)
		return &cmpPred{col: colIdx, op: c.Op, val: c.Value, num: num, numOK: ok}, nil

	case OpBetween, OpNotBetween:
		num1, ok1 := parseNumber(c.Value)
		num2, ok2 := parseNumber(c.Value2)
		// between 必须能解析为数值，否则编译报错（区间在文本上无意义）
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("操作符 %s 需要两个数值，得到 %q / %q", c.Op, c.Value, c.Value2)
		}
		// 自动归一化 min/max
		if num1 > num2 {
			num1, num2 = num2, num1
		}
		return &betweenPred{col: colIdx, op: c.Op, min: num1, max: num2}, nil

	case OpContains, OpNotContains, OpStartsWith, OpEndsWith:
		return &textPred{col: colIdx, op: c.Op, val: c.Value}, nil

	case OpIn, OpNotIn:
		// value 是逗号或换行分隔的列表
		set := parseSet(c.Value)
		if len(set) == 0 {
			return nil, fmt.Errorf("操作符 %s 需要至少一个值", c.Op)
		}
		return &inPred{col: colIdx, op: c.Op, set: set}, nil

	case OpEmpty, OpNotEmpty:
		return &emptyPred{col: colIdx, op: c.Op}, nil

	case OpDateBetween, OpDateNotBetween:
		t1, ok1 := parseDate(c.Value)
		t2, ok2 := parseDate(c.Value2)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("操作符 %s 需要两个日期(yyyy-mm-dd)，得到 %q / %q", c.Op, c.Value, c.Value2)
		}
		if t1.After(t2) {
			t1, t2 = t2, t1
		}
		return &dateBetweenPred{col: colIdx, op: c.Op, from: t1, to: t2}, nil

	case OpMatchFormat, OpNotMatchFormat:
		re := compilePreset(c.Format)
		if re == nil {
			return nil, fmt.Errorf("未知格式预设: %q", c.Format)
		}
		return &formatPred{col: colIdx, op: c.Op, re: re}, nil

	default:
		return nil, fmt.Errorf("未知操作符: %q", c.Op)
	}
}

// parseNumber 尝试把字符串解析为 float64。Excel 数值常带 ","：去掉。
// 空串 → 不是数。
func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	s = strings.ReplaceAll(s, ",", "")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseSet 解析"逗号/分号/换行"分隔的字符串列表。
// 自动 trim 单元，空元素丢弃；保留原大小写。
func parseSet(s string) map[string]struct{} {
	if s == "" {
		return nil
	}
	// 同时支持中文逗号、半角逗号、分号、换行
	repl := strings.NewReplacer(
		"，", "\n",
		",", "\n",
		";", "\n",
		"；", "\n",
	)
	parts := strings.Split(repl.Replace(s), "\n")
	out := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out[p] = struct{}{}
	}
	return out
}

// dateLayouts 兼容多种常见日期写法。
var dateLayouts = []string{
	"2006-01-02",
	"2006/01/02",
	"2006.01.02",
	"2006-1-2",
	"2006/1/2",
	"2006-01-02 15:04:05",
	"2006/01/02 15:04:05",
	time.RFC3339,
}

// parseDate 把字符串解析为日期（Local 时区）。
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range dateLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	// 尝试 Excel 序列号：1900-based 的浮点天数。
	// 简单实现：常见 30000-60000 区间视作日期序列。
	if v, err := strconv.ParseFloat(s, 64); err == nil && v > 1 && v < 2958466 {
		// Excel 1900 闰年 bug：epoch = 1899-12-30
		base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.Local)
		return base.AddDate(0, 0, int(v)), true
	}
	return time.Time{}, false
}
