package splitter

// inplace.go：把拆分结果以"新 Sheet"形式写回源文件。
//
// 设计：上层 (by_rows / by_column) 先把"每个 part 在源 Sheet 里要保留的行号"
// 收集成 InplacePlan 列表，然后调 runInplaceSplit 一次性完成：
//   1. 可选备份源文件
//   2. 二进制克隆 src → tmp.xlsx
//   3. 对每个 plan 的每个 part：CopySheetWithin → FilterRowsInSheet
//   4. Save tmp + AtomicReplace 替换源
//
// by_keyword 不走这条路径，它直接复用 extractor 的 inplace 分支。

import (
	"os"

	"excel-master/internal/core"
	"excel-master/internal/excelio"

	"github.com/xuri/excelize/v2"
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
	tmpPath := srcPath + ".tmp.xlsx"
	_ = os.Remove(tmpPath)
	if err := excelio.CloneFile(srcPath, tmpPath); err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.Remove(tmpPath) }

	f, err := excelize.OpenFile(tmpPath)
	if err != nil {
		cleanup()
		return nil, core.Wrap("EXCEL_OPEN_FAILED", "打开临时文件失败: "+tmpPath, err)
	}

	multiSheet := len(plans) > 1
	created := []string{}
	for _, plan := range plans {
		for _, part := range plan.Parts {
			if len(part.KeepRows) == 0 {
				continue
			}
			name := buildSplitInplaceSheetName(prefix, part.Label, plan.SourceSheet, multiSheet)
			name = excelio.UniqueSheetName(f, name)
			if err := excelio.CopySheetWithin(f, plan.SourceSheet, name); err != nil {
				_ = f.Close()
				cleanup()
				return created, err
			}
			if err := excelio.FilterRowsInSheet(f, name, part.KeepRows); err != nil {
				_ = f.Close()
				cleanup()
				return created, err
			}
			created = append(created, name)
		}
	}
	if err := f.Save(); err != nil {
		_ = f.Close()
		cleanup()
		return created, core.Wrap("EXCEL_SAVE_FAILED", "保存临时文件失败", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return created, core.Wrap("EXCEL_CLOSE_FAILED", "关闭临时文件失败", err)
	}
	if err := excelio.AtomicReplace(srcPath, tmpPath); err != nil {
		cleanup()
		return created, err
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
