package extractor

// dedup.go：V1.1+ 命中行去重。
//
// 背景（设计决策）：
//   - extractor 的 writer 只在 EmitRow 时短暂持有行 Values，Finalize 走 zip 手术
//     按 SourceFile+SourceRow 原地拷贝源文件的行；中间不缓存 values。
//   - 因此去重只能在 EmitRow 的**写入判断**阶段做：已见过的 dedup key 直接 return nil，
//     源行号不进入 sheetRows 列表，Finalize 时自然不会被复制到输出。
//   - 每个 writer（merged / per_keyword / per_source）的"去重范围"不同：
//       merged      -> 全局（唯一 bucket=""）
//       per_keyword -> 每关键词文件内部（bucket = MatchedKW）
//       per_source  -> 每源文件内部（bucket = SourceFile 绝对路径）
//     inplace 写回新 sheet 是 extract_inplace.go 独立路径，接入见 D3。
//
// 关键约束（与用户确认的设计点）：
//   1. strict 比较：完全相等才算重复（不 trim、不 lower；1 != 1.0）
//   2. 保留首次出现（首次写入，后续丢弃）
//   3. 空值（nil / 空字符串 / 纯空白的字符串）视为"不参与去重"——
//      每个空值被当作独立行保留。避免把所有缺列行误判为一组。
//   4. 找不到列名：在 Bind 阶段返回 keyIdx=-1，整个 deduper 成为 no-op，
//      调用方不报错，继续正常输出（跨文件 schema 不一致场景下的主动降级）。
//
// 性能：map[string]map[string]struct{}，查询 O(1)；100k 命中行 + 几百 KB 内存，可忽略。

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// deduper 封装一个 writer 的去重状态。nil 实例和 column=="" 都是合法的 no-op。
type deduper struct {
	column string                      // 去重列名；空串 = 不去重
	keyIdx int                         // Bind 后确定：>=0 时列存在；-1 = 列不存在或未 Bind
	seen   map[string]map[string]bool // bucket -> dedupKey -> seen
}

// newDeduper 构造 deduper。column 为空时 ShouldDrop 恒返回 false（no-op）。
func newDeduper(column string) *deduper {
	return &deduper{
		column: column,
		keyIdx: -1,
		seen:   map[string]map[string]bool{},
	}
}

// Enabled 判断当前 deduper 是否真正会影响行为。
// 列名为空 / Bind 未找到列 / 未 Bind 都返回 false。
func (d *deduper) Enabled() bool {
	return d != nil && d.column != "" && d.keyIdx >= 0
}

// Bind 在 writer.Begin(schema) 时调用，把列名解析为 0-based 索引。
// 找不到时 keyIdx 保持 -1，后续 ShouldDrop 全部返回 false。
//
// 返回 keyIdx >= 0 表示列存在；调用方可据此 emit warning 日志。
func (d *deduper) Bind(columns []string) int {
	if d == nil || d.column == "" {
		return -1
	}
	d.keyIdx = findColumnIndex(columns, d.column)
	return d.keyIdx
}

// ShouldDrop 判断一条行是否应被丢弃（已见过同 bucket 的同 key）。
//
// bucket 取决于 writer 的策略：
//   - merged       -> ""
//   - per_keyword  -> row.MatchedKW
//   - per_source   -> row.SourceFile
//
// 当前 deduper 未启用 / 列不存在 / 去重列值为空 -> 始终返回 false（保留该行）。
func (d *deduper) ShouldDrop(bucket string, values []any) bool {
	if !d.Enabled() {
		return false
	}
	if d.keyIdx >= len(values) {
		return false // 该行 values 缩水（跨文件 schema 不一致），保留
	}
	key := dedupKeyForCell(values[d.keyIdx])
	if key == "" {
		return false // 空值不参与去重
	}
	m, ok := d.seen[bucket]
	if !ok {
		m = map[string]bool{}
		d.seen[bucket] = m
	}
	if m[key] {
		return true // 重复
	}
	m[key] = true
	return false
}

// findColumnIndex 线性查找列名，返回 0-based 索引；找不到返回 -1。
// 比较用 strict equality（跟 dedup 的 strict 语义一致）；列名不做 trim/lowercase。
func findColumnIndex(columns []string, name string) int {
	for i, c := range columns {
		if c == name {
			return i
		}
	}
	return -1
}

// dedupKeyForCell 把单元格值转成去重 key 字符串。
//
//   - nil / 空字符串 / 纯空白 -> 返回 ""（调用方据此跳过，不参与去重）
//   - excelize.Cell（含公式）-> 取其 Value 字段
//   - 其他类型（string / 数值 / bool / time）-> fmt.Sprintf("%v")
//
// strict 比较意味着 "1" 和 "1.0" 不等、"丰田" 和 "丰田 " 不等。这是 V1 的故意选择，
// 避免引入 normalization 规则导致用户预期不可控。如果用户反馈需要忽略空白/大小写，
// V2 加 DedupIgnoreWhitespace / DedupIgnoreCase 选项。
func dedupKeyForCell(v any) string {
	if v == nil {
		return ""
	}
	if cell, ok := v.(excelize.Cell); ok {
		v = cell.Value
	}
	if s, ok := v.(string); ok {
		if strings.TrimSpace(s) == "" {
			return ""
		}
		return s
	}
	s := fmt.Sprintf("%v", v)
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}
