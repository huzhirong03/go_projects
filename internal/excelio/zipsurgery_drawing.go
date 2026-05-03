package excelio

import (
	"bytes"
	"regexp"
	"strconv"
)

// rewriteDrawingXML 处理 xl/drawings/drawingN.xml：
//   - 遍历每个 <xdr:twoCellAnchor> / <xdr:oneCellAnchor> / <xdr:absoluteAnchor> 节点
//   - 读出 <xdr:from><xdr:row>N</xdr:row></xdr:from>（drawing 中 row 是 **0-based**，
//     对应 sheet 中 1-based 行号 = N+1）
//   - 若该 anchor 的 from.row+1 不在 rowMap 里：整个 anchor 节点丢弃
//   - 若在：把 from.row 和 to.row（如果有）都重写为 rowMap[oldRow+1]-1（回到 0-based）
//
// 之所以要整块删除锚点对应的 drawing 节点，是因为如果只改行号但 anchor 指向被删的行，
// 在 Excel 里会出现"图片挤到一起"或"图片在错误的 cell"的问题（这是 V1.2/V1.3 的核心 bug）。
//
// 其他部分（命名空间声明、blipFill、clientData 等）原样保留。
// 若 rowMap 为 nil：表示"保留全部行、不重写图片锚点"，drawing 原样返回。
func rewriteDrawingXML(data []byte, rowMap map[int]int) ([]byte, error) {
	if rowMap == nil {
		return data, nil
	}
	anchorRegexps := []*regexp.Regexp{
		regexp.MustCompile(`(?s)<xdr:twoCellAnchor\b[^>]*>.*?</xdr:twoCellAnchor>`),
		regexp.MustCompile(`(?s)<xdr:oneCellAnchor\b[^>]*>.*?</xdr:oneCellAnchor>`),
		regexp.MustCompile(`(?s)<xdr:absoluteAnchor\b[^>]*>.*?</xdr:absoluteAnchor>`),
	}
	out := make([]byte, len(data))
	copy(out, data)
	for _, re := range anchorRegexps {
		out = processAnchors(out, re, rowMap)
	}
	return out, nil
}

// fromRowRegexp 匹配 from 子树里的 <xdr:row>N</xdr:row>
var fromRowRegexp = regexp.MustCompile(`(?s)<xdr:from>.*?<xdr:row>(\d+)</xdr:row>.*?</xdr:from>`)

// toRowRegexp 匹配 to 子树里的 <xdr:row>N</xdr:row>
var toRowRegexp = regexp.MustCompile(`(?s)<xdr:to>.*?<xdr:row>(\d+)</xdr:row>.*?</xdr:to>`)

// anyRowRegexp 匹配 <xdr:row>N</xdr:row>（用于 replace）。
var anyRowRegexp = regexp.MustCompile(`<xdr:row>(\d+)</xdr:row>`)

// processAnchors 对每个 anchor 块判断保留/丢弃/重写。
func processAnchors(data []byte, re *regexp.Regexp, rowMap map[int]int) []byte {
	var out bytes.Buffer
	out.Grow(len(data))
	last := 0
	for _, m := range re.FindAllIndex(data, -1) {
		start, end := m[0], m[1]
		out.Write(data[last:start])
		anchor := data[start:end]
		if keepAndRewrite, newAnchor := decideAnchor(anchor, rowMap); keepAndRewrite {
			out.Write(newAnchor)
		}
		// 丢弃的情况不写，相当于删掉
		last = end
	}
	out.Write(data[last:])
	return out.Bytes()
}

// decideAnchor 判定一个 anchor 是否保留：
//   - 看 <xdr:from><xdr:row>N</xdr:row></xdr:from> 的 N（0-based）对应 sheet 行号 N+1
//   - 如果 N+1 在 rowMap 里：保留并把 from/to 的 row 重写为新的 0-based 行号
//   - 否则：丢弃整个 anchor
//
// 返回 (是否保留, 重写后的 anchor 字节)
func decideAnchor(anchor []byte, rowMap map[int]int) (bool, []byte) {
	fromMatch := fromRowRegexp.FindSubmatch(anchor)
	if len(fromMatch) < 2 {
		// 没有 from row，保守保留
		return true, anchor
	}
	fromZero, err := strconv.Atoi(string(fromMatch[1]))
	if err != nil {
		return true, anchor
	}
	srcRow := fromZero + 1
	newRow, ok := rowMap[srcRow]
	if !ok {
		// 该行被删 → 丢弃整个 anchor
		return false, nil
	}
	newZero := newRow - 1

	// 改写 from row
	newAnchor := replaceInSection(anchor, fromRowRegexp, newZero)
	// 改写 to row（若存在）。偏移量与 from 一致：新 to.row = 原 to.row + (newZero - fromZero)
	delta := newZero - fromZero
	if toMatch := toRowRegexp.FindSubmatch(newAnchor); len(toMatch) >= 2 {
		oldToZero, err := strconv.Atoi(string(toMatch[1]))
		if err == nil {
			newToZero := oldToZero + delta
			newAnchor = replaceToRow(newAnchor, newToZero)
		}
	}
	return true, newAnchor
}

// replaceInSection 在 anchor 中 from 子树里把 <xdr:row>X</xdr:row> 的 X 替换成 newVal。
// 通过匹配整个 <xdr:from>...</xdr:from> 段，再在其内部用 anyRowRegexp 改第一个 row。
func replaceInSection(anchor []byte, sectionRe *regexp.Regexp, newVal int) []byte {
	return sectionRe.ReplaceAllFunc(anchor, func(section []byte) []byte {
		// section 是 <xdr:from> ... </xdr:from> 全文；把其中第一个 <xdr:row>N</xdr:row> 改为 newVal
		first := anyRowRegexp.FindIndex(section)
		if first == nil {
			return section
		}
		newRow := []byte("<xdr:row>" + strconv.Itoa(newVal) + "</xdr:row>")
		out := make([]byte, 0, len(section))
		out = append(out, section[:first[0]]...)
		out = append(out, newRow...)
		out = append(out, section[first[1]:]...)
		return out
	})
}

// replaceToRow 把 anchor 中 <xdr:to>...</xdr:to> 内第一个 row 改为 newVal。
func replaceToRow(anchor []byte, newVal int) []byte {
	return replaceInSection(anchor, toRowRegexp, newVal)
}
