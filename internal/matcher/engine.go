package matcher

import (
	"strings"

	"excel-master/internal/core"
)

// Engine 是一个多关键词、多模式的匹配引擎。
// 典型用法：
//
//	eng := matcher.NewEngine([]string{"口红", "kouhong"}, core.MatchExact|core.MatchContains|core.MatchPinyin)
//	if kw := eng.Match("哑光口红 A12"); kw != "" { ... }
//
// Match 一旦找到命中就返回对应的关键词（原始输入的那个），不会继续试其他关键词。
type Engine struct {
	keywords []keywordEntry
	mode     core.MatchMode
}

type keywordEntry struct {
	raw      string // 原始关键词（返回给调用方的那个）
	lowered  string // 小写版本，用于 exact/contains
	pinyin   string // 全拼小写
	initials string // 首字母小写
}

// NewEngine 构造引擎。keywords 和 mode 都不能为空。
// 如果 mode 为 0 则默认使用 MatchContains。
func NewEngine(keywords []string, mode core.MatchMode) *Engine {
	if mode == 0 {
		mode = core.MatchContains
	}
	entries := make([]keywordEntry, 0, len(keywords))
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		e := keywordEntry{
			raw:     kw,
			lowered: strings.ToLower(kw),
		}
		if mode.Has(core.MatchPinyin) {
			e.pinyin = ToFullPinyin(kw)
			e.initials = ToInitials(kw)
		}
		entries = append(entries, e)
	}
	return &Engine{keywords: entries, mode: mode}
}

// Keywords 返回当前引擎的原始关键词列表。
func (e *Engine) Keywords() []string {
	out := make([]string, len(e.keywords))
	for i, k := range e.keywords {
		out[i] = k.raw
	}
	return out
}

// HasKeywords 判断引擎是否含至少一个关键词。
// 给 extractor 主循环用——避免每行 len(Keywords()) 分配切片。
func (e *Engine) HasKeywords() bool {
	return len(e.keywords) > 0
}

// Match 检查文本是否命中任一关键词。命中返回关键词原文，否则返回空串。
func (e *Engine) Match(text string) string {
	if len(e.keywords) == 0 || text == "" {
		return ""
	}
	lowered := strings.ToLower(text)
	var textPinyin, textInitials string
	if e.mode.Has(core.MatchPinyin) {
		textPinyin = ToFullPinyin(text)
		textInitials = ToInitials(text)
	}

	for _, k := range e.keywords {
		if e.mode.Has(core.MatchExact) {
			if lowered == k.lowered {
				return k.raw
			}
		}
		if e.mode.Has(core.MatchContains) {
			if strings.Contains(lowered, k.lowered) {
				return k.raw
			}
		}
		if e.mode.Has(core.MatchPinyin) {
			// 拼音子串匹配（支持全拼和首字母）。
			if k.pinyin != "" && strings.Contains(textPinyin, k.pinyin) {
				return k.raw
			}
			if k.initials != "" && strings.Contains(textInitials, k.initials) {
				return k.raw
			}
		}
	}
	return ""
}

// MatchRow 对一整行的多个单元格依次尝试匹配，返回命中的关键词（任意列命中即算整行命中）。
// searchCols 为空切片表示"全列搜索"。
func (e *Engine) MatchRow(cells []string, searchCols []int) string {
	if len(searchCols) == 0 {
		for _, c := range cells {
			if kw := e.Match(c); kw != "" {
				return kw
			}
		}
		return ""
	}
	for _, idx := range searchCols {
		if idx < 0 || idx >= len(cells) {
			continue
		}
		if kw := e.Match(cells[idx]); kw != "" {
			return kw
		}
	}
	return ""
}
