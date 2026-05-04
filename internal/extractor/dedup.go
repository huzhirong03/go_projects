package extractor

// dedup.go：V1.1+ 命中行去重。V1.2+ 支持多列组合 + 归一化开关。
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
// 关键约束：
//  1. 多列组合：每列值归一化后用 '\x01' 拼接成复合 key。Excel 单元格不可能含该字符。
//  2. 保留首次出现（首次写入，后续丢弃）。
//  3. 空值语义：任一参与列为空（nil / 空字符串 / 纯空白）→ 整行不参与去重，全部保留。
//     这跟 V1.1 单列语义一致：避免把"未填的多行"误判为一组。
//  4. 找不到任一列名：整个 deduper 降级为 no-op，调用方不报错（跨文件 schema 不一致场景）。
//  5. 归一化开关（V1.2）：
//     - IgnoreSpace：strings.TrimSpace（仅去首尾，不去中间）
//     - IgnoreCase：strings.ToLower（英文生效，中文无影响）
//     默认两个都 false = V1.1 strict 语义，零回归。
//
// 性能：map[string]map[string]struct{}，查询 O(1)；100k 命中行 × 3 列 + 几百 KB 内存，可忽略。

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// dedupKeySep 多列 key 的分隔符。Excel 单元格不可能包含控制字符 \x01，
// 避免正常文本跟分隔符冲突（比如列值里的逗号、制表符）。
const dedupKeySep = "\x01"

// dedupConfig 是 deduper 的全部配置。
// Columns 为空（len==0）时 deduper 整体降级为 no-op。
type dedupConfig struct {
	Columns     []string // 去重列名列表（1-3 个），Bind 时解析为索引
	IgnoreSpace bool     // 忽略前后空白（只去首尾）
	IgnoreCase  bool     // 忽略大小写（英文生效）
}

// dedupMaxColumns 多列组合的硬上限。超过的部分被 buildDedupConfig 截断。
// 99% 真实业务场景 ≤3 列组合；再多维度的去重需求应考虑改业务流程。
const dedupMaxColumns = 3

// buildDedupConfig 合并 V1.1 单列字段 + V1.2 多列字段，统一规范化：
//   - 合并到一个列表，去空白 + 去重 + 最多保留 dedupMaxColumns 个
//   - 保持原始顺序（用户在 UI 上的选择顺序）
//
// 行为约定（向后兼容）：
//   - V1.1 调用方只填 singleCol -> 出 Columns=[singleCol]
//   - V1.2 调用方只填 multiCols -> 出 Columns=multiCols（去空白去重）
//   - 两个都填 -> singleCol 排首位，后面跟 multiCols 里跟 singleCol 不同的列
//   - 两个都空 -> Columns 为空，deduper 为 no-op
func buildDedupConfig(singleCol string, multiCols []string, ignoreSpace, ignoreCase bool) dedupConfig {
	seen := map[string]struct{}{}
	merged := make([]string, 0, 1+len(multiCols))
	add := func(c string) {
		c = strings.TrimSpace(c)
		if c == "" {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		merged = append(merged, c)
	}
	add(singleCol)
	for _, c := range multiCols {
		add(c)
	}
	if len(merged) > dedupMaxColumns {
		merged = merged[:dedupMaxColumns]
	}
	return dedupConfig{
		Columns:     merged,
		IgnoreSpace: ignoreSpace,
		IgnoreCase:  ignoreCase,
	}
}

// deduper 封装一个 writer 的去重状态。nil 实例和空 Columns 都是合法的 no-op。
type deduper struct {
	cfg     dedupConfig
	indices []int                          // Bind 后每列索引；任一 -1 表示整体失效
	bound   bool                           // Bind 被调用过
	seen    map[string]map[string]struct{} // bucket -> compositeKey -> {}
}

// newDeduper 构造 deduper。Columns 为空时 ShouldDrop 恒返回 false（no-op）。
func newDeduper(cfg dedupConfig) *deduper {
	return &deduper{
		cfg:  cfg,
		seen: map[string]map[string]struct{}{},
	}
}

// Enabled 判断当前 deduper 是否真正会影响行为。
// Columns 空 / 未 Bind / 任一列找不到 都返回 false。
func (d *deduper) Enabled() bool {
	if d == nil || len(d.cfg.Columns) == 0 || !d.bound {
		return false
	}
	for _, idx := range d.indices {
		if idx < 0 {
			return false
		}
	}
	return true
}

// Bind 在 writer.Begin(schema) 时调用，把列名解析为 0-based 索引。
// 任一列找不到时对应 indices[i]=-1，Enabled() 会返回 false。
//
// 返回 indices 切片的副本；调用方可据此判断哪些列缺失，emit warning 日志。
func (d *deduper) Bind(columns []string) []int {
	if d == nil || len(d.cfg.Columns) == 0 {
		return nil
	}
	d.indices = make([]int, len(d.cfg.Columns))
	for i, name := range d.cfg.Columns {
		d.indices[i] = findColumnIndex(columns, name)
	}
	d.bound = true
	// 返回副本避免调用方改动内部状态
	out := make([]int, len(d.indices))
	copy(out, d.indices)
	return out
}

// ShouldDrop 判断一条行是否应被丢弃（已见过同 bucket 的同 composite key）。
//
// bucket 取决于 writer 的策略：
//   - merged       -> ""
//   - per_keyword  -> row.MatchedKW
//   - per_source   -> row.SourceFile
//
// 返回 false 的 4 种情况：
//  1. deduper 未启用（no-op）
//  2. 任一列索引越界（schema 缩水）
//  3. 任一列的原始值为空（空值不参与去重）
//  4. 归一化后的 composite key 首次出现（记录并放行）
func (d *deduper) ShouldDrop(bucket string, values []any) bool {
	if !d.Enabled() {
		return false
	}
	parts := make([]string, len(d.indices))
	for i, idx := range d.indices {
		if idx >= len(values) {
			return false // 该行 values 缩水，保留
		}
		raw := dedupKeyForCell(values[idx])
		if raw == "" {
			return false // 任一列为空，整行不去重
		}
		parts[i] = normalizeDedupKey(raw, d.cfg.IgnoreSpace, d.cfg.IgnoreCase)
	}
	key := strings.Join(parts, dedupKeySep)

	m, ok := d.seen[bucket]
	if !ok {
		m = map[string]struct{}{}
		d.seen[bucket] = m
	}
	if _, hit := m[key]; hit {
		return true
	}
	m[key] = struct{}{}
	return false
}

// findColumnIndex 线性查找列名，返回 0-based 索引；找不到返回 -1。
// 列名比较用 strict equality，不做 trim / lowercase（列名来自 headers，不该被用户感知归一化）。
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
// 本函数**不做归一化**（归一化由 normalizeDedupKey 负责）；只做空值判定和类型→字符串的转换，
// 保证"空值语义"先于"归一化语义"生效。
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

// normalizeDedupKey 按开关做归一化。
//
//   - IgnoreSpace：strings.TrimSpace，只去首尾空白（不处理中间）
//   - IgnoreCase：strings.ToLower，英文生效；中文 unicode 不变
//
// 两个开关都 false 时返回 s 本身（零开销）。
func normalizeDedupKey(s string, ignoreSpace, ignoreCase bool) string {
	if ignoreSpace {
		s = strings.TrimSpace(s)
	}
	if ignoreCase {
		s = strings.ToLower(s)
	}
	return s
}
