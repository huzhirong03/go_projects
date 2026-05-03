package excelio

import (
	"strings"
	"testing"
)

func TestUnshareFormulasInSheet_MainAndFollowers(t *testing.T) {
	// 模拟 Excel 共享公式 K2:K4，主公式在 K2 = SUM(F2:J2)
	src := `<sheetData>
<row r="2"><c r="K2"><f t="shared" ref="K2:K4" si="0">SUM(F2:J2)</f><v>10</v></c></row>
<row r="3"><c r="K3"><f t="shared" si="0"/><v>20</v></c></row>
<row r="4"><c r="K4"><f t="shared" si="0"/><v>30</v></c></row>
</sheetData>`
	out := string(unshareFormulasInSheet([]byte(src)))

	// 主公式应去掉 t="shared" / ref / si，只保留表达式
	if !strings.Contains(out, "<c r=\"K2\"><f>SUM(F2:J2)</f>") {
		t.Errorf("K2 主公式未正确展开:\n%s", out)
	}
	// K3 应展开为 SUM(F3:J3)
	if !strings.Contains(out, "<c r=\"K3\"><f>SUM(F3:J3)</f>") {
		t.Errorf("K3 follower 应偏移为 SUM(F3:J3):\n%s", out)
	}
	// K4 应展开为 SUM(F4:J4)
	if !strings.Contains(out, "<c r=\"K4\"><f>SUM(F4:J4)</f>") {
		t.Errorf("K4 follower 应偏移为 SUM(F4:J4):\n%s", out)
	}
	// 不应再含 t="shared"
	if strings.Contains(out, `t="shared"`) {
		t.Errorf("展开后不应再有 t=\"shared\":\n%s", out)
	}
}

func TestUnshareFormulasInSheet_AbsoluteRefs(t *testing.T) {
	// 主公式含绝对引用 $A$1：偏移时不动；A2 是相对引用：跟随偏移。
	src := `<sheetData>
<row r="2"><c r="B2"><f t="shared" ref="B2:B3" si="0">$A$1+A2</f></c></row>
<row r="3"><c r="B3"><f t="shared" si="0"/></c></row>
</sheetData>`
	out := string(unshareFormulasInSheet([]byte(src)))
	if !strings.Contains(out, "<c r=\"B3\"><f>$A$1+A3</f>") {
		t.Errorf("B3 应保持 $A$1 不动、A2→A3，实际:\n%s", out)
	}
}

func TestUnshareFormulasInSheet_NoSharedFormula(t *testing.T) {
	// 没有 shared formula 时应返回原样
	src := `<sheetData>
<row r="1"><c r="A1"><f>1+2</f><v>3</v></c></row>
</sheetData>`
	out := string(unshareFormulasInSheet([]byte(src)))
	if out != src {
		t.Errorf("无 shared formula 时应原样返回:\nin:  %s\nout: %s", src, out)
	}
}

func TestColLettersConv(t *testing.T) {
	cases := []struct {
		s string
		n int
	}{
		{"A", 1}, {"Z", 26}, {"AA", 27}, {"AZ", 52}, {"BA", 53}, {"AAA", 703},
	}
	for _, c := range cases {
		if got := colLettersToNum(c.s); got != c.n {
			t.Errorf("colLettersToNum(%q)=%d, want %d", c.s, got, c.n)
		}
		if got := colNumToLetters(c.n); got != c.s {
			t.Errorf("colNumToLetters(%d)=%q, want %q", c.n, got, c.s)
		}
	}
}
