package excelio

import (
	"bytes"
	"regexp"
	"strconv"

	"excel-master/internal/core"
)

// rewriteSheetXML 处理 xl/worksheets/sheetN.xml：
//   - 只保留 <sheetData> 里 r 属性在 rowMap 中的 <row> 节点
//   - 把保留行的 r= 属性改成 rowMap 里的新行号
//   - 把该行内所有 <c r="<letters><N>"> 的 r 属性改成新行号
//   - 对同行公式 <f>=...N...</f> 做行号偏移（<f> 内部所有 <letters>N 被替换为 <letters>newN）
//   - <sheetData> 外部内容（<cols>、<mergeCells>、<conditionalFormatting>、<extLst>、<tableParts> 等）
//     全部原样保留
//
// 工作方式：
//   - 字符串级扫描定位 <sheetData> / </sheetData>，外部 bytewise 保留
//   - 内部用 regex 按 <row ...> ... </row> 分块，逐块决定保留/丢弃/重写
//
// rowMap 形如 {54:3, 56:4, 60:5}（源行号 -> 新行号，均 1-based）。
// 若 rowMap 为 nil：表示"保留全部行、不重写行号"，整段 sheetN.xml 原样返回。
func rewriteSheetXML(data []byte, rowMap map[int]int) ([]byte, error) {
	if rowMap == nil {
		return data, nil
	}
	sdStart := bytes.Index(data, []byte("<sheetData"))
	if sdStart < 0 {
		return nil, core.New("SHEETDATA_NOT_FOUND", "sheetN.xml 里找不到 <sheetData>")
	}
	// 兼容自闭合 <sheetData/> 空表
	selfClose := bytes.Index(data[sdStart:], []byte("/>"))
	openEnd := bytes.Index(data[sdStart:], []byte(">"))
	if openEnd < 0 {
		return nil, core.New("SHEETDATA_MALFORMED", "<sheetData 没有闭合的 >")
	}
	openEndAbs := sdStart + openEnd + 1
	// 判断是否是 <sheetData/>（自闭合）
	if selfClose >= 0 && selfClose == openEnd-1 {
		// 空表直接返回原样
		return data, nil
	}

	sdEnd := bytes.Index(data[openEndAbs:], []byte("</sheetData>"))
	if sdEnd < 0 {
		return nil, core.New("SHEETDATA_NOT_CLOSED", "找不到 </sheetData>")
	}
	sdEndAbs := openEndAbs + sdEnd
	// 闭合标签后继续
	sdCloseEnd := sdEndAbs + len("</sheetData>")

	before := data[:openEndAbs] // 含 <sheetData ...> 起始标签本身
	inside := data[openEndAbs:sdEndAbs]
	after := data[sdCloseEnd:]

	newInside, err := rewriteSheetData(inside, rowMap)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.Grow(len(before) + len(newInside) + (len(data) - sdCloseEnd) + 32)
	out.Write(before)
	out.Write(newInside)
	out.WriteString("</sheetData>")
	out.Write(after)
	return out.Bytes(), nil
}

// rowBlockRegexp 匹配完整一个 <row ...> ... </row> 块。
//
//	(?s) 开启 . 匹配换行；row 不嵌套，所以非贪婪就够了。
var rowBlockRegexp = regexp.MustCompile(`(?s)<row\b[^>]*>.*?</row>`)

// rowRAttrRegexp 从 <row ...> 开标签里提取 r="N" 的 N。
var rowRAttrRegexp = regexp.MustCompile(`\br="(\d+)"`)

// cellRefRegexp 匹配 cell 引用 `<c r="A54" ...` 或公式 `>A54<` 等 `<letters><digits>` 组合。
// 注意：只匹配后接非字母的数字，避免误改诸如 "A540" 这种子串。
var cellRefRegexp = regexp.MustCompile(`([A-Z]+)(\d+)`)

// rewriteSheetData 处理 <sheetData> 内部字节，按 rowMap 过滤 + 重写行号。
// 非 row 结构（比如空白、缩进）保留原样。
func rewriteSheetData(inside []byte, rowMap map[int]int) ([]byte, error) {
	var out bytes.Buffer
	out.Grow(len(inside))

	last := 0
	matches := rowBlockRegexp.FindAllIndex(inside, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		// 保留 row 之间的原始文本（一般就是缩进/换行）
		out.Write(inside[last:start])
		rowBytes := inside[start:end]
		// 提取源行号
		rAttr := rowRAttrRegexp.FindSubmatch(rowBytes)
		if len(rAttr) < 2 {
			// 没有 r= 属性？保守保留
			out.Write(rowBytes)
			last = end
			continue
		}
		srcRow, err := strconv.Atoi(string(rAttr[1]))
		if err != nil {
			out.Write(rowBytes)
			last = end
			continue
		}
		newRow, keep := rowMap[srcRow]
		if !keep {
			// 丢弃整个 row
			last = end
			continue
		}
		rewrittenRow := rewriteRowBlock(rowBytes, srcRow, newRow)
		out.Write(rewrittenRow)
		last = end
	}
	out.Write(inside[last:])
	return out.Bytes(), nil
}

// rewriteRowBlock 在单个 <row ...> ... </row> 块上做 3 件事：
//  1. 把 `<row ... r="<srcRow>"` 改为 `r="<newRow>"`
//  2. 把所有 `<c r="<letters><srcRow>"` 改成 `r="<letters><newRow>"`
//  3. 把所有 `<f>` 元素内部所有 `<letters><srcRow>`（后接非字母非数字）替换为 `<letters><newRow>`
//     —— 这实现了"同行公式偏移"；跨行引用不改
func rewriteRowBlock(row []byte, srcRow, newRow int) []byte {
	srcStr := strconv.Itoa(srcRow)
	newStr := strconv.Itoa(newRow)

	// (1) 改 row 的 r 属性（第一次匹配）
	row = rowRAttrRegexp.ReplaceAllFunc(row, func(m []byte) []byte {
		// m 形如 r="54"，只改第一次出现（row 标签里的）
		// 简单起见：直接匹配第一个 r="<srcRow>" 并替换；避免影响后续 c 标签里的 r="A54"
		// 因为 row 的 r= 紧挨 <row，而 c 的 r= 都是 r="A.."，不是纯数字
		// 这里 regexp 是 \br="(\d+)"，只匹配值为纯数字的 r 属性 → 正好只会命中 row 标签里的
		// 所以 ReplaceAllFunc 会命中 row r="54"，但不会命中 c r="A54"
		return []byte(`r="` + newStr + `"`)
	})

	// (2) 定位所有 <c ...> 开标签，改其中 r="<letters><srcRow>"
	// 用 regex 匹配 r="<letters><srcRow>"
	cRefRegexp := regexp.MustCompile(`r="([A-Z]+)` + srcStr + `"`)
	row = cRefRegexp.ReplaceAllFunc(row, func(m []byte) []byte {
		// m = r="A54" -> r="A3"（提取 letters 部分）
		subm := cRefRegexp.FindSubmatch(m)
		if len(subm) < 2 {
			return m
		}
		letters := subm[1]
		return []byte(`r="` + string(letters) + newStr + `"`)
	})

	// (3) 在 <f>...</f> 内部替换 <letters><srcRow> -> <letters><newRow>
	// 只匹配公式内部，避免误改 <v>54</v> 这种
	row = rewriteFormulasInRow(row, srcRow, newRow)

	return row
}

// formulaRegexp 匹配 <f ...>内容</f>，含可能的 t= 等属性。
var formulaRegexp = regexp.MustCompile(`(?s)(<f\b[^>]*>)(.*?)(</f>)`)

// rewriteFormulasInRow 对 row 字节里每个 <f>...</f> 内容做同行偏移：
// 把形如 `<letters><srcRow>` 且后一字符非数字的片段替换为 `<letters><newRow>`。
func rewriteFormulasInRow(row []byte, srcRow, newRow int) []byte {
	srcStr := strconv.Itoa(srcRow)
	newStr := strconv.Itoa(newRow)

	// 精确匹配公式内 cell 引用：字母 + 源行号 + (右边界：非数字字符)
	// 用向前看 `([^0-9]|$)`，但 RE2 不支持 lookahead。改用捕获组保留后字符。
	inner := regexp.MustCompile(`([A-Z]+)` + srcStr + `([^0-9A-Za-z]|$)`)

	return formulaRegexp.ReplaceAllFunc(row, func(m []byte) []byte {
		sub := formulaRegexp.FindSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		openTag := sub[1]
		body := sub[2]
		closeTag := sub[3]
		newBody := inner.ReplaceAllFunc(body, func(b []byte) []byte {
			s := inner.FindSubmatch(b)
			if len(s) < 3 {
				return b
			}
			letters := s[1]
			tail := s[2]
			return []byte(string(letters) + newStr + string(tail))
		})
		var buf bytes.Buffer
		buf.Write(openTag)
		buf.Write(newBody)
		buf.Write(closeTag)
		return buf.Bytes()
	})
}
