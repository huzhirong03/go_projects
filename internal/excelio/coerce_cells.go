package excelio

// coerce_cells.go: 对 sheet.xml 做"文本数字 cell → 纯数字 cell"的 xml 层修复。
//
// 背景：用户源 xlsx 里"看起来是数字"的列有时以 shared string 或 inline string
// 存储（常见于从第三方系统导出或 VBA 写入的数据）。Excel 打开会在左上角显示
// 绿色三角警告"数字以文本形式存储"，且无法做 sum/avg 等数值计算。
//
// 修复策略：扫描每个 <c> cell，对符合以下条件的做 xml 改写：
//   - 有 t="s"  且 <v>N</v> 指向的 sharedStrings[N] 能被 CoerceScalar 识别为数字
//     → 改写为 <c r="..." s="..."><v>数字</v></c>（去掉 t 属性）
//   - 有 t="inlineStr" 且 <is><t>X</t></is> 中 X 能被 CoerceScalar 识别为数字
//     → 改写为 <c r="..." s="..."><v>数字</v></c>（去掉 t 和 <is>）
//
// 保守规则（跟 CoerceScalar 一致）：
//   - 含 <f> 公式的 cell 不动（公式 cell 的 t 属性语义不同）
//   - rich text（<si> 含多个 <r>）的 shared string 不动
//   - 带前导 0 / 超过 10 位整数的 "数字字符串" 保留（避免误转手机号/身份证）

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
)

// LoadSharedStrings 从 xlsx zip 里读 xl/sharedStrings.xml 并返回 index -> value 数组。
//
// 对含 rich text（多个 <r> 段）的 si，返回的字符串为空 — 调用方据此跳过该 index
// 的 cell（避免把富文本错转成数字）。
//
// 文件不存在时返回 nil, nil（xlsx 允许无 sharedStrings）。
func LoadSharedStrings(zr *zip.Reader) ([]string, error) {
	data, err := readEntryByNameOptional(zr, "xl/sharedStrings.xml")
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return parseSharedStrings(data), nil
}

// parseSharedStrings 解析 sharedStrings.xml 字节流为 index->value 数组。
// 对 rich text（<si> 里含 <r> 段）条目，value 为空字符串。
func parseSharedStrings(data []byte) []string {
	type tNode struct {
		Value string `xml:",chardata"`
	}
	type siNode struct {
		T    *tNode  `xml:"t"`
		Runs []tNode `xml:"r>t"`
	}
	type sst struct {
		SI []siNode `xml:"si"`
	}
	var s sst
	if err := xml.Unmarshal(data, &s); err != nil {
		return nil
	}
	out := make([]string, len(s.SI))
	for i, si := range s.SI {
		// rich text：有 <r> 段，保守视为"不纯字符串"，不参与数字化
		if len(si.Runs) > 0 {
			continue
		}
		if si.T != nil {
			out[i] = si.T.Value
		}
	}
	return out
}

// cellFullRegexp 匹配完整一个 <c ...>...</c> 块（非自闭合）。
// 捕获：(1) 开标签里的 attrs   (2) 内部内容
var cellFullRegexp = regexp.MustCompile(`(?s)<c\b([^/>]*)>(.*?)</c>`)

// cellTAttrRegexp 从 attrs 里提 t="..." 的值
var cellTAttrRegexp = regexp.MustCompile(`\bt="([^"]*)"`)

// cellSharedVRegexp 从 shared string cell 内容提 <v>N</v>
var cellSharedVRegexp = regexp.MustCompile(`<v>(\d+)</v>`)

// cellInlineTRegexp 从 inline string cell 内容提 <is><t>value</t></is>
// 支持 <t xml:space="preserve">x</t> 变体
var cellInlineTRegexp = regexp.MustCompile(`(?s)<is>\s*<t[^>]*>(.*?)</t>\s*</is>`)

// cellHasFormulaRegexp 检测内容里是否含 <f> — 含则跳过整个 cell
var cellHasFormulaRegexp = regexp.MustCompile(`<f\b`)

// CoerceStringCellsToNumbers 扫描一段 sheet.xml，对符合条件的 shared/inline
// string cell 改写为数字 cell。其他 cell 原样保留。
//
// sharedStrings 为 sharedStrings.xml 的 index->value 数组（LoadSharedStrings 返回）。
// sharedStrings 为 nil 时只处理 inline string cell。
func CoerceStringCellsToNumbers(data []byte, sharedStrings []string) []byte {
	return cellFullRegexp.ReplaceAllFunc(data, func(cellBlock []byte) []byte {
		sub := cellFullRegexp.FindSubmatch(cellBlock)
		if len(sub) < 3 {
			return cellBlock
		}
		attrs := sub[1]
		content := sub[2]
		// 有公式 → 不碰
		if cellHasFormulaRegexp.Match(content) {
			return cellBlock
		}
		// 识别 t 类型
		tMatch := cellTAttrRegexp.FindSubmatch(attrs)
		if len(tMatch) < 2 {
			return cellBlock // 无 t 属性，默认就是数字或其他（日期等），不动
		}
		tType := string(tMatch[1])

		var rawStr string
		switch tType {
		case "s":
			if sharedStrings == nil {
				return cellBlock
			}
			vm := cellSharedVRegexp.FindSubmatch(content)
			if len(vm) < 2 {
				return cellBlock
			}
			idx, err := strconv.Atoi(string(vm[1]))
			if err != nil || idx < 0 || idx >= len(sharedStrings) {
				return cellBlock
			}
			rawStr = sharedStrings[idx]
			if rawStr == "" {
				// rich text 或空字符串 → 不动
				return cellBlock
			}
		case "inlineStr":
			im := cellInlineTRegexp.FindSubmatch(content)
			if len(im) < 2 {
				return cellBlock
			}
			// inline string 里含 <r> 富文本的不匹配 <is><t>..</t></is> 简单形式 → 自然不动
			rawStr = xmlUnescape(string(im[1]))
		default:
			return cellBlock
		}

		// 智能判断：是否可转数字
		numStr, ok := CoerceStringToNumber(rawStr)
		if !ok {
			return cellBlock
		}

		// 改写：去掉 t 属性，内容替换为 <v>数字</v>
		newAttrs := cellTAttrRegexp.ReplaceAll(attrs, []byte(""))
		// 压缩可能留下的多余空格
		newAttrs = collapseSpaces(newAttrs)

		var buf bytes.Buffer
		buf.Grow(len(cellBlock))
		buf.WriteString("<c")
		buf.Write(newAttrs)
		buf.WriteString("><v>")
		buf.WriteString(numStr)
		buf.WriteString("</v></c>")
		return buf.Bytes()
	})
}

// xmlUnescape 反转 xml 基础字符实体。inline string 的 <t> 内容里只会出现
// &amp; &lt; &gt; &quot; &apos;（规范如此）。
func xmlUnescape(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	r := s
	r = strings.ReplaceAll(r, "&amp;", "&")
	r = strings.ReplaceAll(r, "&lt;", "<")
	r = strings.ReplaceAll(r, "&gt;", ">")
	r = strings.ReplaceAll(r, "&quot;", "\"")
	r = strings.ReplaceAll(r, "&apos;", "'")
	return r
}

var multiSpaceRegexp = regexp.MustCompile(` {2,}`)

// collapseSpaces 把相邻多空格合并成 1 个，并去掉尾部多余空格（避免删除 t 属性
// 后生成 <c r="X1" > 这种带冗余空格的 xml）。开头空格保留作为 <c 和属性的分隔。
func collapseSpaces(b []byte) []byte {
	b = multiSpaceRegexp.ReplaceAll(b, []byte(" "))
	for len(b) > 0 && b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	}
	return b
}
