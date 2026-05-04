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
		{"多关键词-命中其二", []string{"口红", "粉底"}, core.MatchContains, "XX 粉底 YY", "粉底"},
		{"全部不命中", []string{"xyz"}, core.MatchContains, "口红 A12", ""},
		{"中文子串模糊-多关键词", []string{"貔", "貅"}, core.MatchContains, "亮铜貔貅", "貔"},
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
