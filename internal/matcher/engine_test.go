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

// TestEngineASCIIFastPath 验证 B1 快路径：
// 关键词都不含 ASCII 字母时，asciiFreeKeywords=true，Match 走无 ToLower 分支；
// 关键词含 ASCII 字母时，走 ToLower 慢路径，大小写不敏感仍然生效。
// 两条路径对各自适用场景必须返回完全相同的命中结果。
func TestEngineASCIIFastPath(t *testing.T) {
	t.Run("纯中文关键词-快路径", func(t *testing.T) {
		e := NewEngine([]string{"汉族"}, core.MatchContains)
		if !e.asciiFreeKeywords {
			t.Fatalf("纯中文关键词应启用 asciiFreeKeywords 快路径")
		}
		if got := e.Match("张三 汉族 男"); got != "汉族" {
			t.Errorf("快路径 Contains 未命中: %q", got)
		}
		if got := e.Match("张三 壮族 男"); got != "" {
			t.Errorf("快路径不应命中无关文本: %q", got)
		}
	})

	t.Run("纯数字关键词-快路径", func(t *testing.T) {
		e := NewEngine([]string{"2026"}, core.MatchContains)
		if !e.asciiFreeKeywords {
			t.Fatalf("纯数字关键词应启用快路径")
		}
		if got := e.Match("日期 2026-05-04"); got != "2026" {
			t.Errorf("快路径数字命中错误: %q", got)
		}
	})

	t.Run("ASCII 关键词-慢路径-大小写不敏感", func(t *testing.T) {
		e := NewEngine([]string{"VIP"}, core.MatchContains)
		if e.asciiFreeKeywords {
			t.Fatalf("ASCII 关键词不应启用快路径")
		}
		// 大小写不敏感必须保持
		for _, text := range []string{"VIP 客户", "vip 会员", "VipUser", "xxVIPxx"} {
			if got := e.Match(text); got != "VIP" {
				t.Errorf("ASCII 慢路径大小写不敏感失效: text=%q got=%q", text, got)
			}
		}
	})

	t.Run("中英混合关键词-慢路径", func(t *testing.T) {
		// 一个 ASCII 一个中文：应走慢路径，两个都能命中
		e := NewEngine([]string{"VIP", "汉族"}, core.MatchContains)
		if e.asciiFreeKeywords {
			t.Fatalf("混合关键词存在 ASCII 应走慢路径")
		}
		if got := e.Match("张三 汉族"); got != "汉族" {
			t.Errorf("混合关键词慢路径中文命中错误: %q", got)
		}
		if got := e.Match("李四 Vip 客户"); got != "VIP" {
			t.Errorf("混合关键词慢路径 ASCII 大小写不敏感失效: %q", got)
		}
	})

	t.Run("空关键词集合-不启用快路径", func(t *testing.T) {
		e := NewEngine([]string{"", "  "}, core.MatchContains)
		if e.asciiFreeKeywords {
			t.Fatalf("空关键词集合不应启用快路径")
		}
		if got := e.Match("任何文本"); got != "" {
			t.Errorf("空关键词任何输入都应返回空串: %q", got)
		}
	})

	t.Run("纯中文关键词-精准模式", func(t *testing.T) {
		// 快路径下 Exact 模式也必须工作
		e := NewEngine([]string{"汉族"}, core.MatchExact)
		if !e.asciiFreeKeywords {
			t.Fatalf("纯中文 Exact 应启用快路径")
		}
		if got := e.Match("汉族"); got != "汉族" {
			t.Errorf("快路径 Exact 命中错误: %q", got)
		}
		if got := e.Match("汉族裔"); got != "" {
			t.Errorf("快路径 Exact 不应命中超集: %q", got)
		}
	})
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
