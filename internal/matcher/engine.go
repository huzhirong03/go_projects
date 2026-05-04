package matcher

import (
	"strings"

	"excel-master/internal/core"
)

// Engine 是一个多关键词、多模式的匹配引擎。
// 典型用法：
//
//	eng := matcher.NewEngine([]string{"口红"}, core.MatchExact|core.MatchContains)
//	if kw := eng.Match("哑光口红 A12"); kw != "" { ... }
//
// Match 一旦找到命中就返回对应的关键词（原始输入的那个），不会继续试其他关键词。
//
// 性能关键路径：
// 扫描百万级 cell 时，Match 每 cell 都会被调一次。实测 fixture 01（100k 行 x 14 列）
// 1.4M 次调用里绝大多数时间花在 strings.ToLower(text) —— 对中文字符做 Unicode 表查找 +
// 分配新字符串的开销，整体占扫描耗时 ~85%。但中文/数字/符号根本没有大小写概念，
// 对它们做 ToLower 是纯浪费。
//
// 所以 Engine 在构造时就记录"所有关键词是否都不含 ASCII 字母"（asciiFreeKeywords），
// Match 里走两条路径：
//   - 快路径（所有关键词纯中文/数字/符号）：直接用 raw 做 Contains/Exact，0 次 ToLower
//   - 慢路径（至少一个关键词含 ASCII 字母，如 "VIP"）：保持原 ToLower 语义
//
// 这样大小写不敏感的行为在 ASCII 关键词场景下完全保留（"VIP" 仍能匹配 "vip"）。
type Engine struct {
	keywords          []keywordEntry
	mode              core.MatchMode
	asciiFreeKeywords bool // 所有关键词都不含 ASCII 字母 -> 走无 ToLower 快路径
}

type keywordEntry struct {
	raw     string // 原始关键词（返回给调用方的那个）
	lowered string // 小写版本，用于 exact/contains
}

// containsASCIIAlpha 判断字符串是否含任一 ASCII 字母 [a-zA-Z]。
// 只有它们才有"大小写"概念——中文、数字、标点、全角符号等都不需要 lowercase 预处理。
// 故意只扫 ASCII 字母，不用 unicode.IsLetter；多语言特殊大小写（如土耳其语 dotless i）
// 超出本项目用户场景，保持 ASCII-fast-path 的简单与正确。
func containsASCIIAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// NewEngine 构造引擎。keywords 和 mode 都不能为空。
// 如果 mode 为 0 则默认使用 MatchContains。
func NewEngine(keywords []string, mode core.MatchMode) *Engine {
	if mode == 0 {
		mode = core.MatchContains
	}
	entries := make([]keywordEntry, 0, len(keywords))
	anyASCII := false
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		if containsASCIIAlpha(kw) {
			anyASCII = true
		}
		entries = append(entries, keywordEntry{
			raw:     kw,
			lowered: strings.ToLower(kw),
		})
	}
	// 快路径仅当存在至少一个关键词且全部无 ASCII 字母时启用。
	// 空关键词集合（entries 为空）保持 asciiFreeKeywords=false，走慢路径的 early-return。
	asciiFree := len(entries) > 0 && !anyASCII
	return &Engine{keywords: entries, mode: mode, asciiFreeKeywords: asciiFree}
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
//
// 性能分支：
//   - asciiFreeKeywords=true：所有关键词都是纯中文/数字/符号，直接 raw 对比，零 ToLower。
//     这是 fixture 01 "汉族" 场景，100k 行 x 14 列扫描省 ~60s。
//   - asciiFreeKeywords=false：至少一个关键词含 ASCII 字母（如 "VIP"），
//     走原 ToLower(text) vs k.lowered 的大小写不敏感路径。
//
// 两条路径在各自适用场景下给出完全相同的命中结果，仅性能不同。
func (e *Engine) Match(text string) string {
	if len(e.keywords) == 0 || text == "" {
		return ""
	}

	// 快路径：关键词全无 ASCII 字母 -> 不做 lowercase，直接 raw 比对。
	// 对中文/数字/符号关键词，strings.Contains 走 byte-level 比较，极快。
	if e.asciiFreeKeywords {
		for _, k := range e.keywords {
			if e.mode.Has(core.MatchExact) {
				if text == k.raw {
					return k.raw
				}
			}
			if e.mode.Has(core.MatchContains) {
				if strings.Contains(text, k.raw) {
					return k.raw
				}
			}
		}
		return ""
	}

	// 慢路径：存在至少一个 ASCII 关键词，需要大小写不敏感 -> 先 lowercase 一次。
	lowered := strings.ToLower(text)
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
