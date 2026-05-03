package excelio

import (
	"bytes"
	"regexp"
	"strings"
)

// rewriteWorkbookXML 从 xl/workbook.xml 的 <sheets> 节点里删除 r:id 不在 keepRIDs 中的
// <sheet> 条目。其余部分（workbookPr / bookViews / calcPr / definedNames 等）原样保留。
//
// 注意：不删 <definedNames>（名称定义）和 <calcPr>，因为它们可能引用的不是 sheet 名。
// 极端情况下 defined names 引用被删 sheet 会残留无效引用，Excel 打开会警告但不会损坏文件。
func rewriteWorkbookXML(data []byte, keepRIDs map[string]struct{}) ([]byte, error) {
	return processSheetNodes(data, func(sheetTag []byte) []byte {
		rid := extractRIDFromSheetTag(sheetTag)
		if _, ok := keepRIDs[rid]; ok {
			return sheetTag
		}
		return nil
	}), nil
}

// extractRIDFromSheetTag 从一个 <sheet ...> 标签里提取 r:id 属性值（取不到返回 ""）。
func extractRIDFromSheetTag(sheetTag []byte) string {
	m := rIDAttrRegexp.FindSubmatch(sheetTag)
	if len(m) < 2 {
		return ""
	}
	return string(m[1])
}

// sheetTagRegexp 匹配 workbook.xml 里的单个 <sheet ... /> 或 <sheet ...></sheet>。
// 注意：属性值里允许出现 / （如 r:id 不会有，但与下方两个正则保持一致风格），
// 但 > 必须在 XML 中转义为 &gt;，所以用 [^>]*? 是安全的。
var sheetTagRegexp = regexp.MustCompile(`(?s)<sheet\b[^>]*?(?:/>|>.*?</sheet>)`)

// rIDAttrRegexp 从一个 <sheet> 标签里提取 r:id 属性值。
var rIDAttrRegexp = regexp.MustCompile(`r:id="([^"]+)"`)

// processSheetNodes 遍历 <sheet ...> 节点，按 keep 函数决定保留/删除。
// keep 返回 nil 表示删除该 sheet 节点；返回切片则替换原节点。
func processSheetNodes(data []byte, keep func([]byte) []byte) []byte {
	// 只在 <sheets>...</sheets> 范围内处理，避免误伤
	sStart := bytes.Index(data, []byte("<sheets>"))
	sEnd := bytes.Index(data, []byte("</sheets>"))
	if sStart < 0 || sEnd < 0 || sEnd < sStart {
		return data
	}
	innerStart := sStart + len("<sheets>")
	inner := data[innerStart:sEnd]

	var newInner bytes.Buffer
	newInner.Grow(len(inner))
	last := 0
	for _, m := range sheetTagRegexp.FindAllIndex(inner, -1) {
		st, en := m[0], m[1]
		newInner.Write(inner[last:st])
		kept := keep(inner[st:en])
		if kept != nil {
			newInner.Write(kept)
		}
		last = en
	}
	newInner.Write(inner[last:])

	var out bytes.Buffer
	out.Grow(len(data))
	out.Write(data[:innerStart])
	out.Write(newInner.Bytes())
	out.Write(data[sEnd:])
	return out.Bytes()
}

// rewriteWorkbookRels 从 xl/_rels/workbook.xml.rels 删除 dropRIDs 的 Relationship 条目。
// keepRID 保留；styles / theme / sharedStrings 等 rels 由于 Id 不在 dropRIDs 中会自动保留。
func rewriteWorkbookRels(data []byte, keepRID string, dropRIDs []string) ([]byte, error) {
	dropSet := map[string]struct{}{}
	for _, rid := range dropRIDs {
		dropSet[rid] = struct{}{}
	}
	// 关键：Target 属性值含 "/"（如 "/xl/worksheets/sheet2.xml" 或 "worksheets/sheet2.xml"），
	// 所以正则不能用 [^/>]*，必须用 [^>]*?。
	relRegexp := regexp.MustCompile(`(?s)<Relationship\b[^>]*?(?:/>|>.*?</Relationship>)`)
	idAttr := regexp.MustCompile(`\bId="([^"]+)"`)

	var out bytes.Buffer
	out.Grow(len(data))
	last := 0
	for _, m := range relRegexp.FindAllIndex(data, -1) {
		st, en := m[0], m[1]
		out.Write(data[last:st])
		tag := data[st:en]
		if id := idAttr.FindSubmatch(tag); len(id) >= 2 {
			if _, drop := dropSet[string(id[1])]; drop {
				// 丢弃这个 Relationship
				last = en
				continue
			}
		}
		out.Write(tag)
		last = en
	}
	out.Write(data[last:])
	_ = keepRID // 保留参数便于后续扩展（未使用，当前逻辑用黑名单）
	return out.Bytes(), nil
}

// rewriteContentTypes 从 [Content_Types].xml 删除 dropSheetPaths 指向的 <Override> 条目。
// 其它 Override（theme / styles / drawing / 共享字符串）保留。
func rewriteContentTypes(data []byte, dropSheetPaths []string) ([]byte, error) {
	if len(dropSheetPaths) == 0 {
		return data, nil
	}
	dropSet := map[string]struct{}{}
	for _, p := range dropSheetPaths {
		// [Content_Types].xml 里的 PartName 以 "/" 开头
		dropSet["/"+strings.TrimPrefix(p, "/")] = struct{}{}
	}
	// 关键：PartName 属性值含 "/"（如 "/xl/worksheets/sheet2.xml"），
	// 所以正则不能用 [^/>]*，必须用 [^>]*?。同时兼容自闭合和成对两种写法。
	overrideRegexp := regexp.MustCompile(`(?s)<Override\b[^>]*?(?:/>|>.*?</Override>)`)
	partNameAttr := regexp.MustCompile(`\bPartName="([^"]+)"`)

	var out bytes.Buffer
	out.Grow(len(data))
	last := 0
	for _, m := range overrideRegexp.FindAllIndex(data, -1) {
		st, en := m[0], m[1]
		out.Write(data[last:st])
		tag := data[st:en]
		if pn := partNameAttr.FindSubmatch(tag); len(pn) >= 2 {
			if _, drop := dropSet[string(pn[1])]; drop {
				last = en
				continue
			}
		}
		out.Write(tag)
		last = en
	}
	out.Write(data[last:])
	return out.Bytes(), nil
}
