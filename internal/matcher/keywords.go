// Package matcher 提供关键词匹配能力。
// 支持组合：精准 + 包含 + 拼音（全拼和首字母），多关键词取 OR。
package matcher

import (
	"strings"
	"unicode"
)

// ParseKeywords 把用户输入的关键词字符串解析成切片。
// 分隔符：半角/全角逗号、空格、分号、顿号、换行。连续分隔符视为一个。
// 会自动去重和去首尾空白；空串返回空切片。
func ParseKeywords(raw string) []string {
	if raw == "" {
		return nil
	}
	splitter := func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '、', '\n', '\r', '\t':
			return true
		}
		return unicode.IsSpace(r)
	}
	parts := strings.FieldsFunc(raw, splitter)
	if len(parts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
