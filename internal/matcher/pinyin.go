package matcher

import (
	"strings"

	"github.com/mozillazg/go-pinyin"
)

// pinyinArgs 默认参数：普通拼音（不带声调），小写。
var pinyinArgs = pinyin.NewArgs()

// ToFullPinyin 把文本转成全拼小写字符串。
// 中文字符转拼音；非中文字符原样保留（小写化）。
// 例如 "哑光口红 A12" -> "yaguangkouhong a12"。
func ToFullPinyin(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text) * 2)
	for _, r := range text {
		if isChineseChar(r) {
			segs := pinyin.Pinyin(string(r), pinyinArgs)
			if len(segs) > 0 && len(segs[0]) > 0 {
				b.WriteString(segs[0][0])
				continue
			}
		}
		b.WriteRune(toLowerRune(r))
	}
	return b.String()
}

// ToInitials 取每个中文字符拼音首字母，非中文字符原样保留（小写化）。
// 例如 "哑光口红 A12" -> "ygkh a12"。
func ToInitials(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if isChineseChar(r) {
			segs := pinyin.Pinyin(string(r), pinyinArgs)
			if len(segs) > 0 && len(segs[0]) > 0 && len(segs[0][0]) > 0 {
				b.WriteByte(segs[0][0][0])
				continue
			}
		}
		b.WriteRune(toLowerRune(r))
	}
	return b.String()
}

// isChineseChar 判断是不是常用 CJK 汉字范围。
func isChineseChar(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) // CJK Extension A
}

func toLowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
