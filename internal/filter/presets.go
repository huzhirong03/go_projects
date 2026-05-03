package filter

// presets.go：内置正则字典，给 OpMatchFormat / OpNotMatchFormat 用。
//
// 用户在 UI 上看到的是"是手机号 / 是邮箱 / ..."这种业务化命名，
// 不暴露原始正则。Format key 由前端透传到后端，后端按此 key 查表。
//
// 加新格式：在这里加一行常量 + 在 presetPatterns 里加映射。
//
// 注意：Go 标准库 regexp 用 RE2 语法（无回溯），不能用 (?<=...) 等高级语法。
// 这里所有预设都用基础语法，跟 JS 大致兼容，前端校验也方便。

import (
	"regexp"
	"sync"
)

// 预设格式名。
const (
	FormatPhoneCN     = "phone_cn"      // 中国大陆手机号
	FormatEmail       = "email"         // 邮箱
	FormatURL         = "url"           // 网址
	FormatIDCard18    = "id_card_18"    // 18 位身份证
	FormatDate        = "date"          // 日期格式 YYYY-MM-DD / YYYY/MM/DD
	FormatDigitsOnly  = "digits_only"   // 纯数字
	FormatHasChinese  = "has_chinese"   // 含中文字符
	FormatPostcodeCN  = "postcode_cn"   // 中国 6 位邮政编码
)

// presetPatterns 是 Format → 正则源码的映射。Compile 时按需 lazy compile 并缓存。
var presetPatterns = map[string]string{
	FormatPhoneCN:    `^1[3-9]\d{9}$`,
	FormatEmail:      `^[\w.+\-]+@[\w\-]+\.[\w.\-]+$`,
	FormatURL:        `^https?://`,
	FormatIDCard18:   `^\d{17}[\dXx]$`,
	FormatDate:       `^\d{4}[-/]\d{1,2}[-/]\d{1,2}$`,
	FormatDigitsOnly: `^\d+$`,
	FormatHasChinese: `[\u4e00-\u9fa5]`,
	FormatPostcodeCN: `^\d{6}$`,
}

// 编译缓存：同一个 Format 的正则只编译一次。
var (
	regexCache   = map[string]*regexp.Regexp{}
	regexCacheMu sync.RWMutex
)

// compilePreset 按 Format key 编译并缓存对应正则。
// 找不到 key 或编译失败时返回 nil。
func compilePreset(format string) *regexp.Regexp {
	if format == "" {
		return nil
	}

	regexCacheMu.RLock()
	if re, ok := regexCache[format]; ok {
		regexCacheMu.RUnlock()
		return re
	}
	regexCacheMu.RUnlock()

	src, ok := presetPatterns[format]
	if !ok {
		return nil
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil
	}

	regexCacheMu.Lock()
	regexCache[format] = re
	regexCacheMu.Unlock()
	return re
}

// IsKnownFormat 判断 format key 是否在预设表里（compile 阶段校验用）。
func IsKnownFormat(format string) bool {
	_, ok := presetPatterns[format]
	return ok
}
