package matcher

// 隔离 benchmark：只测 Engine.Match 的绝对速度，不掺 excelize 读 I/O。
// 目的是验证 B1 快路径（asciiFreeKeywords=true）是否真的比慢路径快。
//
// 跑法：
//
//	go test ./internal/matcher/ -run=^$ -bench=BenchmarkEngineMatch -benchmem -count=3
//
// 预期（按 fixture 01 的 cell 分布构造）：
//   - ASCIIFastPath_ChineseOnlyKW：纯中文关键词 + 中文 cell，最快
//   - SlowPath_ASCIIKW：ASCII 关键词 + 中文 cell，ToLower 命中大头
//   - SlowPath_MixedKW：中英混合，中文 cell 仍要 ToLower

import (
	"fmt"
	"testing"

	"excel-master/internal/core"
)

// buildBenchCells 构造一个接近 fixture 01 分布的 cell 集合：
// 14 列典型学生信息行，其中 ~30% cell 是空串、~70% 是中文短串。
// 返回 100 行 x 14 列 = 1400 cells 的平坦切片，模拟 scanning 一个中型文件。
func buildBenchCells() []string {
	cols := []string{
		"20260001", "张三", "男", "汉族", "2012-05-01",
		"四年级", "1班", "13800138000", "北京市海淀区中关村街道 xxx 号",
		"", "", "2026-03-01", "在读", "100001",
	}
	out := make([]string, 0, 100*14)
	for i := 0; i < 100; i++ {
		for _, c := range cols {
			if c == "" {
				out = append(out, "")
				continue
			}
			out = append(out, fmt.Sprintf("%s-%d", c, i))
		}
	}
	return out
}

// BenchmarkEngineMatch_FastPath_ChineseKW：纯中文关键词 ("汉族")
// 应走快路径 asciiFreeKeywords=true，零 ToLower 调用。
func BenchmarkEngineMatch_FastPath_ChineseKW(b *testing.B) {
	eng := NewEngine([]string{"汉族"}, core.MatchContains)
	if !eng.asciiFreeKeywords {
		b.Fatalf("预期 asciiFreeKeywords=true")
	}
	cells := buildBenchCells()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range cells {
			_ = eng.Match(c)
		}
	}
}

// BenchmarkEngineMatch_SlowPath_ASCIIKW：ASCII 关键词 ("VIP")
// 应走慢路径 asciiFreeKeywords=false，每 cell 一次 ToLower。
func BenchmarkEngineMatch_SlowPath_ASCIIKW(b *testing.B) {
	eng := NewEngine([]string{"VIP"}, core.MatchContains)
	if eng.asciiFreeKeywords {
		b.Fatalf("预期 asciiFreeKeywords=false")
	}
	cells := buildBenchCells()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range cells {
			_ = eng.Match(c)
		}
	}
}

// BenchmarkEngineMatch_SlowPath_MixedKW：中英混合关键词
// 走慢路径；对比纯 ASCII 看中文关键词是否额外增加开销。
func BenchmarkEngineMatch_SlowPath_MixedKW(b *testing.B) {
	eng := NewEngine([]string{"VIP", "汉族"}, core.MatchContains)
	if eng.asciiFreeKeywords {
		b.Fatalf("预期 asciiFreeKeywords=false")
	}
	cells := buildBenchCells()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range cells {
			_ = eng.Match(c)
		}
	}
}

// BenchmarkEngineMatch_FastPath_MultipleChineseKW：多个纯中文关键词
// 快路径下多关键词是否线性增加成本。
func BenchmarkEngineMatch_FastPath_MultipleChineseKW(b *testing.B) {
	eng := NewEngine([]string{"汉族", "壮族", "回族", "藏族"}, core.MatchContains)
	if !eng.asciiFreeKeywords {
		b.Fatalf("预期 asciiFreeKeywords=true")
	}
	cells := buildBenchCells()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range cells {
			_ = eng.Match(c)
		}
	}
}
