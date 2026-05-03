package splitter

import (
	"os"
	"strings"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/excelio"

	"github.com/xuri/excelize/v2"
)

// sanitizeFileName 替换 Windows 文件名非法字符。
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "_"
	}
	repl := strings.NewReplacer(
		"\\", "_", "/", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return repl.Replace(name)
}

// timestamp 紧凑时间戳。
func timestamp() string { return time.Now().Format("20060102_150405") }

// cloneAndExtractSheet 实现原汁原味拆分的通用步骤：
//  1. 二进制复制 srcPath -> outPath（CloneFile 已保证 outPath 不存在）
//  2. excelize.OpenFile(outPath)
//  3. 只保留 [sheet] 这个 Sheet，其它全删
//  4. 若 keepRows 非 nil，则过滤行（保留 keepRows，其余 RemoveRow，图片跟着删）
//  5. Save + Close
//
// 任何步骤失败都会尝试清理半成品（删 outPath）。
//
// keepRows 传 nil 表示"不过滤行，保留该 Sheet 所有行"（by_sheet 的场景）。
// 传非 nil 切片则只保留里面的行号（by_rows / by_column 的场景；调用方需自行包含表头）。
func cloneAndExtractSheet(srcPath, outPath, sheet string, keepRows []int) error {
	if err := excelio.CloneFile(srcPath, outPath); err != nil {
		return err
	}
	f, err := excelize.OpenFile(outPath)
	if err != nil {
		_ = os.Remove(outPath)
		return core.Wrap("EXCEL_OPEN_FAILED", "打开拷贝文件失败: "+outPath, err)
	}
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(outPath)
	}
	if err := excelio.KeepSheetsOnly(f, []string{sheet}); err != nil {
		cleanup()
		return err
	}
	if keepRows != nil {
		if err := excelio.FilterRowsInSheet(f, sheet, keepRows); err != nil {
			cleanup()
			return err
		}
	}
	if err := f.Save(); err != nil {
		cleanup()
		return core.Wrap("EXCEL_SAVE_FAILED", "保存输出文件失败: "+outPath, err)
	}
	if err := f.Close(); err != nil {
		return core.Wrap("EXCEL_CLOSE_FAILED", "关闭输出文件失败: "+outPath, err)
	}
	return nil
}

// selectSheets 按 allowed 过滤 allSheets，返回保持原顺序的子集。
// allowed 为空表示"全部允许"，直接返回 allSheets 原样。
// 若 allowed 中所有项都不在 allSheets 内，返回空切片，调用方应当处理这种情况。
func selectSheets(allSheets, allowed []string) []string {
	if len(allowed) == 0 {
		return allSheets
	}
	set := map[string]struct{}{}
	for _, s := range allowed {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(allSheets))
	for _, s := range allSheets {
		if _, ok := set[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

// sheetSegment 文件名中插入"-Sheet名-"的辅助，仅在多 Sheet 时返回非空。
// 单 Sheet 场景保持文件名简洁与旧版兼容。
func sheetSegment(sheet string, multi bool) string {
	if !multi {
		return ""
	}
	return "_" + sanitizeFileName(sheet)
}
