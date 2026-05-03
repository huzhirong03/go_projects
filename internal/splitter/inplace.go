package splitter

// inplace.go：把拆分结果以"新 Sheet"形式写回源文件。
//
// 设计：上层 (by_rows / by_column) 先把"每个 part 在源 Sheet 里要保留的行号"
// 收集成 InplacePlan 列表，然后调 runInplaceSplit 一次性完成：
//   1. 可选备份源文件
//   2. 把 plan 转为 InplaceSheetSpec 列表（名字去重）
//   3. excelio.AddFilteredSheetsZip 一次性 zip 手术：src → tmp 追加 N 个过滤后的新 Sheet
//   4. AtomicReplace 替换源
//
// V1.x 旧路径用 excelize CopySheet+RemoveRow，遇上大 sheet 会 O(N²) 卡几十秒。
// 现路径纯 zip+xml 手术，能在几百毫秒内完成。by_keyword 走 extractor inplace 分支。

import (
	"os"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// InplacePart 描述一个要落到新 Sheet 的 part。
//   - Label: 新 Sheet 名的标签（如 "part001"、"类别A"），最终 Sheet 名 = prefix + label [+ "_" + sourceSheet]
//   - KeepRows: 该 part 在源 Sheet 中要保留的 1-based 行号，必须已含表头行
type InplacePart struct {
	Label    string
	KeepRows []int
}

// InplacePlan 描述源 Sheet → 多个 part 的映射。
type InplacePlan struct {
	SourceSheet string
	Parts       []InplacePart
}

// runInplaceSplit 执行 inplace 写回。返回新创建的 Sheet 名列表。
//
//	srcPath:  源 xlsx 路径
//	prefix:   新 Sheet 名前缀，"" 时按调用方语义传入（如 "拆_"）
//	plans:    每个源 Sheet 的 part 列表
//	backup:   true 时先生成 src.bak
func runInplaceSplit(srcPath, prefix string, plans []InplacePlan, backup bool) ([]string, error) {
	if len(plans) == 0 {
		return nil, core.New("INPLACE_NO_PARTS", "没有可写回的分片")
	}
	if backup {
		if _, err := excelio.BackupCopy(srcPath); err != nil {
			return nil, err
		}
	}

	// 处理 sheet 名唯一化：读原 xlsx 现有 sheet 名作为初始集合
	existingNames, err := excelio.ListSheetNamesZip(srcPath)
	if err != nil {
		return nil, err
	}
	nameSet := map[string]struct{}{}
	for _, n := range existingNames {
		nameSet[n] = struct{}{}
	}

	multiSheet := len(plans) > 1
	specs := []excelio.InplaceSheetSpec{}
	for _, plan := range plans {
		for _, part := range plan.Parts {
			if len(part.KeepRows) == 0 {
				continue
			}
			base := buildSplitInplaceSheetName(prefix, part.Label, plan.SourceSheet, multiSheet)
			name := excelio.UniqueNameInSet(base, nameSet)
			specs = append(specs, excelio.InplaceSheetSpec{
				SourceSheet:  plan.SourceSheet,
				NewSheetName: name,
				KeepRows:     part.KeepRows,
			})
		}
	}
	if len(specs) == 0 {
		return nil, core.New("INPLACE_NO_PARTS", "没有可写回的分片")
	}

	tmpPath := srcPath + ".tmp.xlsx"
	_ = os.Remove(tmpPath)
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := excelio.AddFilteredSheetsZip(srcPath, tmpPath, specs); err != nil {
		cleanup()
		return nil, err
	}
	if err := excelio.AtomicReplace(srcPath, tmpPath); err != nil {
		cleanup()
		return nil, err
	}
	created := make([]string, 0, len(specs))
	for _, s := range specs {
		created = append(created, s.NewSheetName)
	}
	return created, nil
}

// buildSplitInplaceSheetName 组装拆分 inplace 模式下的新 Sheet 名。
// 单源 Sheet：prefix+label；多源 Sheet：prefix+label+"_"+sourceSheet。
func buildSplitInplaceSheetName(prefix, label, sourceSheet string, multiSheet bool) string {
	base := prefix + label
	if multiSheet {
		base = base + "_" + sourceSheet
	}
	return excelio.SanitizeSheetName(base)
}
