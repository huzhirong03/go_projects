package excelio

import (
	"strconv"
	"strings"
)

// CoerceScalar 把字符串值尽可能还原为原生数字类型（float64），失败则保留原字符串。
//
// 用于解决"本应是数字但以文本形式存储"的 cell：Excel 识别为文本时左上角会出现
// 绿色小三角警告，且无法做 sum/avg 等数值运算。
//
// 保守规则（优先保留字符串，避免错转手机号 / 身份证 / 带前导 0 的编号）：
//  1. 空字符串 → 原字符串
//  2. 含科学计数法 e/E → 字符串
//  3. 整数部分数字位数 > 10 → 字符串（手机号 11 位 / 身份证 18 位）
//  4. strconv.ParseFloat 失败 → 字符串
//  5. Round-trip 严格不等 → 字符串（过滤 "0123" "1.50" "+89" 等格式化差异）
//  6. 其他 → float64
func CoerceScalar(s string) any {
	if s == "" {
		return s
	}
	if strings.ContainsAny(s, "eE") {
		return s
	}
	intPart := s
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intPart = s[:dot]
	}
	digitCount := 0
	for _, ch := range intPart {
		if ch >= '0' && ch <= '9' {
			digitCount++
		}
	}
	if digitCount > 10 {
		return s
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	back := strconv.FormatFloat(f, 'f', -1, 64)
	if back != s {
		return s
	}
	return f
}

// CoerceStringToNumber 是 CoerceScalar 的数字判断版本：若能转为数字，返回
// (规范化的数字字符串, true)，否则 ("", false)。用于 xml 层做 cell 类型修复。
func CoerceStringToNumber(s string) (string, bool) {
	v := CoerceScalar(s)
	f, ok := v.(float64)
	if !ok {
		return "", false
	}
	return strconv.FormatFloat(f, 'f', -1, 64), true
}
