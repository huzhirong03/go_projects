package splitter

import (
	"strings"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// requireXLSXSource 在 by_sheet / by_rows / by_column 模式入口调用。
// CSV 源对这些模式不适用（无 Sheet/样式保真需求），返回友好错误让前端提示用户改用
// by_keyword 或先转 xlsx。
func requireXLSXSource(path string) error {
	if core.DetectSourceKind(path) == core.SourceCSV {
		return core.New(core.CodeSourceFormatUnsupported,
			"CSV 文件暂不支持按 Sheet/行数/列值拆分（CSV 无 Sheet 概念，也无样式需要保真）；如需按关键词拆分，请选择\"按关键词\"模式")
	}
	return nil
}

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

// cloneAndExtractSheet 实现原汁原味拆分：直接走纯 zip+xml 手术，
// 100% 保留源文件样式、条件格式、图片锚点、公式、合并单元格等。
//
// 行为：
//   - 只保留指定 sheet，其它 sheet 一并从 workbook.xml / [Content_Types].xml / rels 中清除
//   - keepRows 为 nil：保留该 sheet 所有行；非 nil：只保留 keepRows 里的 1-based 源行号
//     （调用方需自行包含表头），其余行被删，图片锚点跟随过滤+重映射
//   - 跨被删行的合并单元格会被丢弃；同行公式自动做行号偏移
//
// 已替代：旧的 excelize.OpenFile + Save 路径（V1.3 CloneFileAndFilterRows），
// 因为 excelize Save 会丢条件格式、图片锚点 editAs 属性等。
func cloneAndExtractSheet(srcPath, outPath, sheet string, keepRows []int) error {
	return excelio.CloneAndExtractZip(srcPath, outPath, sheet, keepRows)
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
