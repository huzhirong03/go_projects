package excelio

// unshare_formula.go：把 sheetN.xml 里的"共享公式"（<f t="shared" ref="..." si="N">expr</f>
// + <f t="shared" si="N"/>）展开成"普通公式"（<f>expr</f>），每个 cell 自带完整表达式。
//
// 为什么必须展开？
//   inplace 写回新 Sheet 时，我们会从源 sheet 过滤掉非命中行。如果源里某列用 shared
//   formula（主公式 cell 写完整 SUM(F2:J2)，其余行只写 <f t="shared" si="0"/> 引用主），
//   而过滤后主公式 cell 所在行被删除，剩下的 follower cell 找不到 si=0 的定义，
//   Excel 打开会弹"部分内容有问题"，修复时把整列 shared formula 一起删掉，公式丢失。
//
// 偏移规则（xlsx 规范）：
//   follower cell 的 expr = 主公式 expr 中每个相对引用按 (Δrow, Δcol) 偏移；绝对引用
//   ($A$1 / $A1 / A$1）不偏移。
//
// 调用入口：在 rewriteSheetXML(data, rowMap) **之前** 调 unshareFormulasInSheet(data)。

import (
	"bytes"
	"regexp"
	"strconv"
)

// unshareFormulasInSheet 把 data（一个完整 sheetN.xml 字节）里所有 shared formula
// 展开成 normal formula。返回新字节；不含 shared formula 时原样返回。
func unshareFormulasInSheet(data []byte) []byte {
	// 第 1 遍：扫描所有 <c> 块，收集每个 si 的主公式 (col, row, expr)
	type sharedMain struct {
		col  int // 1-based
		row  int // 1-based
		expr string
	}
	mains := map[string]sharedMain{}

	cells := cellTagRegexp.FindAll(data, -1)
	for _, cell := range cells {
		col, row, ok := extractCellRef(cell)
		if !ok {
			continue
		}
		fAttrs, fExpr, hasF, hasContent := extractFTag(cell)
		if !hasF || !hasContent || fExpr == "" {
			continue
		}
		if !sharedAttrRegexp.MatchString(fAttrs) {
			continue
		}
		if !refAttrRegexp.MatchString(fAttrs) {
			continue
		}
		siM := siAttrRegexp.FindStringSubmatch(fAttrs)
		if siM == nil {
			continue
		}
		mains[siM[1]] = sharedMain{col: col, row: row, expr: fExpr}
	}

	// 没有主公式 → 没必要二次扫描
	if len(mains) == 0 {
		// 即便没主公式，也可能有 follower（某种坏数据）；那就什么都不做
		return data
	}

	// 第 2 遍：替换所有含 shared 标记的 cell 里的 <f .../>。
	// 为了不踩"已替换片段"，我们直接对每个 cell 做局部替换并拼回。
	return cellTagRegexp.ReplaceAllFunc(data, func(cell []byte) []byte {
		col, row, ok := extractCellRef(cell)
		if !ok {
			return cell
		}
		// 找 <f> 起止
		loc := fTagRegexp.FindSubmatchIndex(cell)
		if loc == nil {
			return cell
		}
		fStart, fEnd := loc[0], loc[1]
		var fAttrs, fExpr string
		var hasContent bool
		// fTagRegexp 两组备选：闭合带 body / 自闭合
		if loc[2] >= 0 && loc[4] >= 0 { // 带 body
			fAttrs = string(cell[loc[2]:loc[3]])
			fExpr = string(cell[loc[4]:loc[5]])
			hasContent = true
		} else if loc[6] >= 0 { // 自闭合
			fAttrs = string(cell[loc[6]:loc[7]])
			fExpr = ""
			hasContent = false
		} else {
			return cell
		}
		if !sharedAttrRegexp.MatchString(fAttrs) {
			return cell
		}
		siM := siAttrRegexp.FindSubmatch([]byte(fAttrs))
		if siM == nil {
			return cell
		}
		si := string(siM[1])

		var newExpr string
		if hasContent && fExpr != "" {
			// 主公式：保留 expr，去掉 t="shared" / ref / si 属性
			newExpr = fExpr
		} else {
			// follower：从 mains 查并按 (Δrow, Δcol) 偏移
			main, mok := mains[si]
			if !mok {
				return cell // 找不到主，原样保留（避免误改）
			}
			dRow := row - main.row
			dCol := col - main.col
			newExpr = shiftFormula(main.expr, dRow, dCol)
		}
		// 拼新 cell：cell[:fStart] + "<f>"+newExpr+"</f>" + cell[fEnd:]
		var buf bytes.Buffer
		buf.Grow(len(cell) + len(newExpr))
		buf.Write(cell[:fStart])
		buf.WriteString("<f>")
		buf.WriteString(newExpr)
		buf.WriteString("</f>")
		buf.Write(cell[fEnd:])
		return buf.Bytes()
	})
}

// cellTagRegexp 匹配单个 <c .../> 或 <c ...>...</c> 块。
var cellTagRegexp = regexp.MustCompile(`(?s)<c\b[^>]*?(?:/>|>.*?</c>)`)

// fTagRegexp 匹配一个 <f .../> 或 <f ...>body</f>。
// 两组备选：第一备选捕获带 body 的 (attrs, body)；第二备选捕获自闭合 (attrs)。
var fTagRegexp = regexp.MustCompile(`(?s)<f\b([^>]*)>([^<]*)</f>|<f\b([^/>]*)/>`)

// 从 cell 里提取 r="ColRow" 的 col / row。
var cellRefAttrRegexp = regexp.MustCompile(`\br="([A-Z]+)(\d+)"`)

// 从 <f> 属性里提取关键标记。
var sharedAttrRegexp = regexp.MustCompile(`\bt="shared"`)
var refAttrRegexp = regexp.MustCompile(`\bref="[^"]+"`)
var siAttrRegexp = regexp.MustCompile(`\bsi="(\d+)"`)

func extractCellRef(cell []byte) (col, row int, ok bool) {
	m := cellRefAttrRegexp.FindSubmatch(cell)
	if m == nil {
		return 0, 0, false
	}
	col = colLettersToNum(string(m[1]))
	r, err := strconv.Atoi(string(m[2]))
	if err != nil {
		return 0, 0, false
	}
	return col, r, true
}

// extractFTag 从 cell 字节里抽 <f> 标签，返回 (attrs, expr, hasF, hasContent)。
// hasContent=true 表示是 <f ...>expr</f>；false 表示自闭合 <f .../>。
func extractFTag(cell []byte) (attrs, expr string, hasF, hasContent bool) {
	loc := fTagRegexp.FindSubmatchIndex(cell)
	if loc == nil {
		return "", "", false, false
	}
	if loc[2] >= 0 && loc[4] >= 0 {
		return string(cell[loc[2]:loc[3]]), string(cell[loc[4]:loc[5]]), true, true
	}
	if loc[6] >= 0 {
		return string(cell[loc[6]:loc[7]]), "", true, false
	}
	return "", "", false, false
}

// shiftFormula 对 expr 里所有相对引用按 (dRow, dCol) 偏移；绝对引用（$ 前缀）保持不变。
// 不识别 sheet 名引用（如 'Sheet1'!A1）—— 这种引用里 A1 也会被偏移，对绝大多数同 sheet
// 共享公式不会出现，先按同 sheet 处理；如果未来出现跨 sheet shared formula 再补强。
func shiftFormula(expr string, dRow, dCol int) string {
	return cellRefShiftRegexp.ReplaceAllStringFunc(expr, func(m string) string {
		sub := cellRefShiftRegexp.FindStringSubmatch(m)
		// sub: [full, $col, col, $row, row]
		absCol := sub[1] == "$"
		col := sub[2]
		absRow := sub[3] == "$"
		rowStr := sub[4]

		newCol := col
		if !absCol && dCol != 0 {
			n := colLettersToNum(col) + dCol
			if n < 1 {
				return m // 越界保留原样
			}
			newCol = colNumToLetters(n)
		}
		newRow := rowStr
		if !absRow && dRow != 0 {
			n, err := strconv.Atoi(rowStr)
			if err != nil {
				return m
			}
			n += dRow
			if n < 1 {
				return m
			}
			newRow = strconv.Itoa(n)
		}
		return sub[1] + newCol + sub[3] + newRow
	})
}

// cellRefShiftRegexp 匹配 cell 引用 [$]Col[$]Row。带可选绝对前缀。
var cellRefShiftRegexp = regexp.MustCompile(`(\$?)([A-Z]+)(\$?)(\d+)`)

// colLettersToNum: "A"→1, "Z"→26, "AA"→27
func colLettersToNum(s string) int {
	n := 0
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return 0
		}
		n = n*26 + int(c-'A'+1)
	}
	return n
}

// colNumToLetters: 1→"A", 27→"AA"
func colNumToLetters(n int) string {
	if n < 1 {
		return ""
	}
	var s []byte
	for n > 0 {
		n--
		s = append([]byte{byte('A' + n%26)}, s...)
		n /= 26
	}
	return string(s)
}
