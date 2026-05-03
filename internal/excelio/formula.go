package excelio

import (
	"regexp"
	"strconv"
)

// 用来匹配 Excel 单元格引用，捕获 3 组：
//   1: 列部分（含可选 $），如 "$B" 或 "AA"
//   2: 行的可选 $ 标记（"$" 或空），用来识别绝对行
//   3: 行数字，如 "5"
//
// 例如：
//
//	"B5"     -> 1="B"   2=""  3="5"
//	"$B5"    -> 1="$B"  2=""  3="5"
//	"B$5"    -> 1="B"   2="$" 3="5"
//	"$B$5"   -> 1="$B"  2="$" 3="5"
var cellRefRegex = regexp.MustCompile(`(\$?[A-Z]+)(\$?)(\d+)`)

// RewriteFormulaSameRow 尝试把一个公式从源行偏移到目标行，
// 仅当所有引用都"安全"（同源行 + 相对行 + 不跨 Sheet/工作簿）时才执行偏移。
//
// 返回 (rewritten, ok)：
//   - ok=true: rewritten 是已偏移的公式（带前导 "="），调用方应当作公式写入；
//   - ok=false: 公式不安全，调用方应回退为写计算后的值。
//
// 安全规则：
//
//	1. 公式不含 "!"（跨 Sheet）和 "["（跨工作簿）；
//	2. 任何 cell 引用都不是绝对行（无 "$<digits>" 形态）；
//	3. 任何 cell 引用的行号都等于 srcRow；范围如 B5:B10 因每个端点单独检查，
//	   只要端点行号不一致或不等于 srcRow 就会判为不安全。
//
// 例子：
//
//	"=B5*C5",         src=5, dst=2 -> "=B2*C2", true
//	"=ROUND(B5*C5,2)",src=5, dst=2 -> "=ROUND(B2*C2,2)", true
//	"=$B5*C5",        src=5, dst=2 -> "=$B2*C2", true   // 列绝对、行相对：安全
//	"=B$5*C5",        src=5, dst=2 -> "", false         // 行绝对：不安全
//	"=B4*C5",         src=5, dst=2 -> "", false         // 跨行：不安全
//	"=SUM(B5:B10)",   src=5, dst=2 -> "", false         // 跨行范围
//	"=Sheet2!B5",     src=5, dst=2 -> "", false         // 跨 Sheet
//	"=10+20",         src=5, dst=2 -> "=10+20", true    // 没引用：安全
func RewriteFormulaSameRow(formula string, srcRow, dstRow int) (string, bool) {
	if formula == "" {
		return "", false
	}
	body := formula
	if body[0] == '=' {
		body = body[1:]
	}

	// 快速否决：跨 Sheet / 跨工作簿
	for i := 0; i < len(body); i++ {
		if body[i] == '!' || body[i] == '[' {
			return "", false
		}
	}

	matches := cellRefRegex.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		// 没有 cell 引用（如 "=10+20"），原样返回
		return "=" + body, true
	}

	// 每个 match 的位置布局：
	//   m[0],m[1]    整体匹配
	//   m[2],m[3]    group1 列部分
	//   m[4],m[5]    group2 行 $ 标记
	//   m[6],m[7]    group3 行数字
	for _, m := range matches {
		// group 2 非空 => 行绝对 => 不安全
		if m[5] > m[4] {
			return "", false
		}
		rowStr := body[m[6]:m[7]]
		row, err := strconv.Atoi(rowStr)
		if err != nil {
			return "", false
		}
		if row != srcRow {
			return "", false
		}
	}

	// 全部安全：从右到左替换行号，避免索引偏移
	dstRowStr := strconv.Itoa(dstRow)
	out := []byte(body)
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		rowStart := m[6]
		rowEnd := m[7]
		// 拼接 out[:rowStart] + dstRowStr + out[rowEnd:]
		newOut := make([]byte, 0, len(out)-(rowEnd-rowStart)+len(dstRowStr))
		newOut = append(newOut, out[:rowStart]...)
		newOut = append(newOut, dstRowStr...)
		newOut = append(newOut, out[rowEnd:]...)
		out = newOut
	}
	return "=" + string(out), true
}
