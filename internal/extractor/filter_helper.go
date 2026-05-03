package extractor

// filter_helper.go：把 core.AdvancedFilterSpec 编译成可执行 internal/filter.Filter。
//
// 为什么要"按文件编译"？
//   多文件夹场景下，不同文件的 headers 可能不一致（"金额" vs "amount" vs "Amount"）。
//   filter.Compile 需要 headers 来把"列名"翻译成"列下标"。同一份 spec 对不同 headers
//   编译出的 *filter.Filter 实例可能不同（甚至某些列在某文件里是 missing）。
//
// 设计：
//   - 入口：buildFilter(spec, headers) → (*filter.Filter, []string, error)
//   - missing 列以 string 切片回报；上层 extractor 决定是"硬错"还是"跳过该文件"
//   - 硬错（编译值非法）通过 error 返回

import (
	"excel-master/internal/core"
	"excel-master/internal/filter"
)

// buildFilter 把 core spec + headers 编译为可执行 Filter。
// spec 为 nil 或 IsEmpty → 返回 (nil, nil, nil)，调用方按"零谓词"处理。
// 编译过程发现的"软错误"（列名缺失）通过返回值 missingColumns 上报；
// "硬错误"（值类型非法、未知 op、未知 format）通过 err 返回。
func buildFilter(spec *core.AdvancedFilterSpec, headers []string) (*filter.Filter, []string, error) {
	if spec == nil || spec.IsEmpty() {
		return nil, nil, nil
	}

	// core spec → filter spec。Conditions 字段对偶。
	conds := make([]filter.Condition, 0, len(spec.Conditions))
	for _, c := range spec.Conditions {
		conds = append(conds, filter.Condition{
			Column: c.Column,
			Op:     filter.Op(c.Op),
			Value:  c.Value,
			Value2: c.Value2,
			Format: c.Format,
		})
	}
	mode := filter.Mode(spec.Mode)
	if mode != filter.ModeAll && mode != filter.ModeAny {
		mode = filter.ModeAll
	}
	f, err := filter.Compile(filter.Spec{Mode: mode, Conditions: conds}, headers)
	if err != nil {
		return nil, nil, err
	}
	return f, f.MissingColumns, nil
}

// fileFilterDecision 描述 buildFilterForFile 的返回结果。
//
//   - Filter: 可空。nil 表示"无筛选"，调用方按全通过处理；非空表示有谓词需运行。
//   - SkipReason: 非空字符串 = 该文件应整体跳过（多文件夹场景，本文件缺所有筛选列）。
//   - PartialMissing: 非空 = 部分列缺失（即仍有谓词，但 caller 应 emit warning）。
//
// 决策规则（多文件夹列名一致性）：
//   - 编译后 Filter.IsZero == true 且确实有 spec 条件 → 所有列都缺失 → SkipReason 提示
//   - 部分缺失 → PartialMissing 列出，Filter 仍可用（剩余条件继续生效）
//   - 没有 missing → 干净通过
type fileFilterDecision struct {
	Filter         *filter.Filter
	SkipReason     string   // 非空 = 整文件跳过
	PartialMissing []string // 非空 = 部分列缺失（仍可继续）
}

// buildFilterForFile：在 buildFilter 的基础上叠加多文件夹决策逻辑。
// 用于 ExtractUnits 主循环按文件分发。
func buildFilterForFile(spec *core.AdvancedFilterSpec, fs *FileSchema) (fileFilterDecision, error) {
	if spec == nil || spec.IsEmpty() {
		return fileFilterDecision{}, nil
	}
	f, missing, err := buildFilter(spec, fs.File.Headers)
	if err != nil {
		return fileFilterDecision{}, err
	}
	d := fileFilterDecision{Filter: f}
	// 全列缺失 ⇒ 所有谓词被丢弃 ⇒ Filter.IsZero
	if f != nil && f.IsZero() && len(missing) > 0 {
		d.Filter = nil // 调用方按"无筛选"处理时不会过滤；这里要明确"应跳过文件"
		d.SkipReason = formatMissingMsg(fs.File.Path, fs.File.SheetName, missing)
		return d, nil
	}
	if len(missing) > 0 {
		d.PartialMissing = missing
	}
	return d, nil
}

func formatMissingMsg(path, sheet string, missing []string) string {
	cols := ""
	for i, c := range missing {
		if i > 0 {
			cols += ", "
		}
		cols += c
	}
	return "[" + path + " / " + sheet + "] 缺失高级筛选列 [" + cols + "]，已整体跳过"
}
