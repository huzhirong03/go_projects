package matcher

import (
	"testing"

	"excel-master/internal/core"
)

func TestParseKeywords(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"口红", []string{"口红"}},
		{"口红, 眼影; 粉底 ，粉底、气垫", []string{"口红", "眼影", "粉底", "气垫"}},
		{"A\nB\tC D", []string{"A", "B", "C", "D"}},
	}
	for _, c := range cases {
		got := ParseKeywords(c.in)
		if len(got) != len(c.want) {
			t.Errorf("ParseKeywords(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("ParseKeywords(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestPinyinConversion(t *testing.T) {
	if got := ToFullPinyin("口红"); got != "kouhong" {
		t.Errorf("ToFullPinyin(口红) = %q, want kouhong", got)
	}
	if got := ToInitials("哑光口红"); got != "ygkh" {
		t.Errorf("ToInitials(哑光口红) = %q, want ygkh", got)
	}
	if got := ToFullPinyin("口红 A12"); got != "kouhong a12" {
		t.Errorf("ToFullPinyin(口红 A12) = %q, want 'kouhong a12'", got)
	}
}

func TestEngineModes(t *testing.T) {
	type tc struct {
		name    string
		kw      []string
		mode    core.MatchMode
		text    string
		wantHit string
	}
	cases := []tc{
		{"精准-命中", []string{"口红"}, core.MatchExact, "口红", "口红"},
		{"精准-不命中", []string{"口红"}, core.MatchExact, "哑光口红", ""},
		{"包含-命中", []string{"口红"}, core.MatchContains, "哑光口红 A12", "口红"},
		{"包含-大小写不敏感", []string{"ABC"}, core.MatchContains, "xAbCy", "ABC"},
		{"拼音全拼-命中", []string{"kouhong"}, core.MatchPinyin, "哑光口红", "kouhong"},
		{"拼音首字母-命中", []string{"kh"}, core.MatchPinyin, "哑光口红", "kh"},
		{"拼音-非中文不被误杀", []string{"abc"}, core.MatchPinyin, "abcxyz", "abc"},
		{"组合模式-命中任一", []string{"kouhong"}, core.MatchExact | core.MatchContains | core.MatchPinyin, "哑光口红", "kouhong"},
		{"多关键词-命中其二", []string{"口红", "粉底"}, core.MatchContains, "XX 粉底 YY", "粉底"},
		{"全部不命中", []string{"xyz"}, core.MatchContains | core.MatchPinyin, "口红 A12", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := NewEngine(c.kw, c.mode)
			if got := e.Match(c.text); got != c.wantHit {
				t.Errorf("Match(%q) = %q, want %q", c.text, got, c.wantHit)
			}
		})
	}
}

func TestEngineMatchRow(t *testing.T) {
	e := NewEngine([]string{"口红"}, core.MatchContains)
	cells := []string{"SKU001", "哑光口红 A", "100"}
	// 全列搜索
	if got := e.MatchRow(cells, nil); got != "口红" {
		t.Errorf("MatchRow 全列 = %q, want 口红", got)
	}
	// 只搜第 0 列（SKU），不命中
	if got := e.MatchRow(cells, []int{0}); got != "" {
		t.Errorf("MatchRow [0] = %q, want 空", got)
	}
	// 只搜第 1 列，命中
	if got := e.MatchRow(cells, []int{1}); got != "口红" {
		t.Errorf("MatchRow [1] = %q, want 口红", got)
	}
}
