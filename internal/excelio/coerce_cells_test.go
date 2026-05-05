package excelio

import (
	"strings"
	"testing"
)

func TestCoerceScalar(t *testing.T) {
	cases := []struct {
		in       string
		wantNum  bool
		wantVal  float64
	}{
		{"89", true, 89},
		{"1.5", true, 1.5},
		{"-42", true, -42},
		{"1234567890", true, 1234567890},

		{"", false, 0},
		{"abc", false, 0},
		{"0123", false, 0},
		{"1.50", false, 0},
		{"+89", false, 0},
		{"13812345678", false, 0},        // 11 位（手机号）
		{"310101199001011234", false, 0}, // 身份证 18 位
		{"1e5", false, 0},
	}
	for _, tc := range cases {
		got := CoerceScalar(tc.in)
		if tc.wantNum {
			f, ok := got.(float64)
			if !ok || f != tc.wantVal {
				t.Errorf("CoerceScalar(%q) = %v (%T), want float64 %v", tc.in, got, got, tc.wantVal)
			}
		} else if _, ok := got.(float64); ok {
			t.Errorf("CoerceScalar(%q) = %v, want string", tc.in, got)
		}
	}
}

func TestCoerceStringToNumber(t *testing.T) {
	cases := []struct {
		in      string
		wantStr string
		wantOK  bool
	}{
		{"89", "89", true},
		{"1.5", "1.5", true},
		{"0123", "", false},
		{"abc", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := CoerceStringToNumber(tc.in)
		if ok != tc.wantOK || got != tc.wantStr {
			t.Errorf("CoerceStringToNumber(%q) = (%q,%v), want (%q,%v)",
				tc.in, got, ok, tc.wantStr, tc.wantOK)
		}
	}
}

func TestParseSharedStrings(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="5" uniqueCount="5">
<si><t>89</t></si>
<si><t>汉族</t></si>
<si><t xml:space="preserve">  有空格  </t></si>
<si><r><rPr><b/></rPr><t>富文本</t></r></si>
<si><t>0123</t></si>
</sst>`
	got := parseSharedStrings([]byte(xml))
	if len(got) != 5 {
		t.Fatalf("期望 5 条 si，实际 %d", len(got))
	}
	if got[0] != "89" {
		t.Errorf("si[0] = %q, want %q", got[0], "89")
	}
	if got[1] != "汉族" {
		t.Errorf("si[1] = %q, want %q", got[1], "汉族")
	}
	if !strings.Contains(got[2], "有空格") {
		t.Errorf("si[2] = %q, want 含'有空格'", got[2])
	}
	// rich text 返回空字符串作为"跳过"标记
	if got[3] != "" {
		t.Errorf("si[3] = %q, want 空（rich text）", got[3])
	}
	if got[4] != "0123" {
		t.Errorf("si[4] = %q, want %q", got[4], "0123")
	}
}

func TestCoerceStringCellsToNumbers_SharedString(t *testing.T) {
	ss := []string{"89", "汉族", "0123", ""}
	sheet := `<sheetData>
<row r="1"><c r="A1" t="s"><v>1</v></c><c r="B1" t="s"><v>0</v></c></row>
<row r="2"><c r="A2" t="s" s="5"><v>0</v></c><c r="B2" t="s"><v>2</v></c></row>
</sheetData>`
	got := string(CoerceStringCellsToNumbers([]byte(sheet), ss))

	// A1 是"汉族"（t=s, index=1），应保留 t="s"
	if !strings.Contains(got, `<c r="A1" t="s"><v>1</v></c>`) {
		t.Errorf("A1 应保留原样 t=s，实际 sheet 片段：\n%s", got)
	}
	// B1 是"89"（t=s, index=0），应变成数字 cell（无 t 属性）
	if !strings.Contains(got, `<c r="B1"><v>89</v></c>`) {
		t.Errorf("B1 应转为 <c r=\"B1\"><v>89</v></c>，实际：\n%s", got)
	}
	// A2 也是 "89"（t=s, index=0, s=5），应保留 s="5"
	if !strings.Contains(got, `<v>89</v>`) {
		t.Errorf("A2 应含 <v>89</v>，实际：\n%s", got)
	}
	// B2 "0123" 应保留 t=s（前导 0 不转）
	if !strings.Contains(got, `<c r="B2" t="s"><v>2</v></c>`) {
		t.Errorf("B2 前导 0 应保留 t=s，实际：\n%s", got)
	}
}

func TestCoerceStringCellsToNumbers_InlineString(t *testing.T) {
	sheet := `<sheetData>
<row r="1">
<c r="A1" t="inlineStr"><is><t>89</t></is></c>
<c r="B1" t="inlineStr"><is><t>0123</t></is></c>
<c r="C1" t="inlineStr"><is><t>汉族</t></is></c>
</row>
</sheetData>`
	got := string(CoerceStringCellsToNumbers([]byte(sheet), nil))

	if !strings.Contains(got, `<c r="A1"><v>89</v></c>`) {
		t.Errorf("A1 inline 89 应转数字，实际：\n%s", got)
	}
	if !strings.Contains(got, `t="inlineStr"><is><t>0123</t></is>`) {
		t.Errorf("B1 前导 0 应原样保留，实际：\n%s", got)
	}
	if !strings.Contains(got, `t="inlineStr"><is><t>汉族</t></is>`) {
		t.Errorf("C1 汉字应原样保留，实际：\n%s", got)
	}
}

func TestCoerceStringCellsToNumbers_SkipFormula(t *testing.T) {
	// 公式 cell 即使 t=str 也不碰
	sheet := `<sheetData>
<row r="1"><c r="A1" t="str"><f>SUM(B1:C1)</f><v>42</v></c></row>
</sheetData>`
	got := string(CoerceStringCellsToNumbers([]byte(sheet), nil))
	if !strings.Contains(got, `<f>SUM(B1:C1)</f>`) {
		t.Errorf("含公式的 cell 应原样保留，实际：\n%s", got)
	}
}

func TestCoerceStringCellsToNumbers_SkipNumberCells(t *testing.T) {
	// 原本就是数字 cell（无 t 或 t="n"），不应被扰动
	sheet := `<sheetData>
<row r="1"><c r="A1"><v>89</v></c><c r="B1" t="n"><v>99</v></c></row>
</sheetData>`
	got := string(CoerceStringCellsToNumbers([]byte(sheet), nil))
	// 无 t 的 cell 原样保留（含 <v>89</v>）
	if !strings.Contains(got, `<c r="A1"><v>89</v></c>`) {
		t.Errorf("A1 原样数字应保留，实际：\n%s", got)
	}
	// t="n" 的 cell 不在 switch 里 → 原样
	if !strings.Contains(got, `<c r="B1" t="n"><v>99</v></c>`) {
		t.Errorf("B1 t=n 数字应保留，实际：\n%s", got)
	}
}
