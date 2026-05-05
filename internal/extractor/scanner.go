// Package extractor 实现"文件夹批量关键词提取"的核心管线。
// 核心流程：ScanFolder -> BuildSchema -> 按行流式匹配 -> OutputWriter 输出。
package extractor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// FileInfo 描述扫描到的"一个可处理单元"。
// V1.1 起，一个 .xlsx 含多个 Sheet 时会展开为多条 FileInfo，
// 每条 (Path, SheetName) 唯一，下游统一按"单元"处理；
// 上层结果统计若关心"去重的文件数"用 Path 聚合即可。
type FileInfo struct {
	Path         string
	SheetName    string
	Headers      []string        // 若 HeaderRow <= 0 则为 nil
	ColumnWidths map[int]float64 // 1-based 列号 -> 列宽（用于复刻外观）
}

// ScanFolder 扫描文件夹，列出所有 .xlsx 中的全部 Sheet 单元。
// allowSheets 非空时，仅保留 SheetName ∈ allowSheets 的单元（对所有文件统一过滤）。
// 对每个文件只打开一次读表头，不遍历数据行。
// headerRow 为 1-based；0 表示无表头。
func ScanFolder(folder string, headerRow int, allowSheets []string) ([]FileInfo, error) {
	if folder == "" {
		return nil, core.New("INVALID_FOLDER", "文件夹路径为空")
	}
	stat, err := os.Stat(folder)
	if err != nil {
		return nil, core.Wrap("INVALID_FOLDER", "无法访问文件夹: "+folder, err)
	}
	if !stat.IsDir() {
		return nil, core.New("INVALID_FOLDER", "路径不是文件夹: "+folder)
	}

	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, core.Wrap("INVALID_FOLDER", "读取文件夹失败", err)
	}

	allow := newSheetFilter(allowSheets)

	var units []FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "~$") { // Excel 临时锁
			continue
		}
		if !core.IsSupported(name) {
			continue
		}
		full := filepath.Join(folder, name)
		fileUnits, err := probeAny(full, headerRow, allow)
		if err != nil {
			// 单个文件坏掉不应中断整个扫描，但要向上抛，让调用方决定日志策略。
			return nil, err
		}
		units = append(units, fileUnits...)
	}

	// 稳定排序：先按 Path 再按 SheetName，保证同一个文件夹跨次运行结果一致。
	sort.Slice(units, func(i, j int) bool {
		if units[i].Path != units[j].Path {
			return units[i].Path < units[j].Path
		}
		return units[i].SheetName < units[j].SheetName
	})
	return units, nil
}

// ScanFile 等价于 ScanFolder 但只处理单个文件（用于"单文件按关键词拆分"等场景）。
func ScanFile(path string, headerRow int, allowSheets []string) ([]FileInfo, error) {
	if path == "" {
		return nil, core.New("INVALID_FILE", "文件路径为空")
	}
	if !core.IsSupported(path) {
		return nil, core.New(core.CodeSourceFormatUnsupported, "仅支持 .xlsx / .xlsm / .csv 文件: "+path)
	}
	allow := newSheetFilter(allowSheets)
	units, err := probeAny(path, headerRow, allow)
	if err != nil {
		return nil, err
	}
	if len(units) == 0 && allow != nil {
		return nil, core.New("NO_MATCHED_SHEET", "文件没有任何匹配指定 Sheet 名的工作表")
	}
	return units, nil
}

// SheetsOf 仅返回某个文件的全部 Sheet 名（用于前端预扫描列出可勾选项）。
// 不读表头，性能开销 < 100ms。CSV 固定返回 ["CSV"]。
func SheetsOf(path string) ([]string, error) {
	if core.DetectSourceKind(path) == core.SourceCSV {
		return []string{csvSheetName}, nil
	}
	r, err := excelio.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.SheetNames(), nil
}

// FolderSheetsUnion 扫描文件夹内所有 xlsx，返回 Sheet 名的并集（保持首次出现顺序）。
// 用于"批量提取"前端给用户列出"该文件夹下涉及哪些 Sheet 名"。
func FolderSheetsUnion(folder string) ([]string, error) {
	if folder == "" {
		return nil, core.New("INVALID_FOLDER", "文件夹路径为空")
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, core.Wrap("INVALID_FOLDER", "读取文件夹失败", err)
	}
	seen := map[string]bool{}
	var union []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "~$") || !core.IsSupported(name) {
			continue
		}
		sheets, err := SheetsOf(filepath.Join(folder, name))
		if err != nil {
			return nil, err
		}
		for _, s := range sheets {
			if !seen[s] {
				seen[s] = true
				union = append(union, s)
			}
		}
	}
	return union, nil
}

// csvSheetName 是 CSV 文件被包装成 "伪 Sheet" 后的名字，让下游代码不用改走 (Path, SheetName) 该路径。
const csvSheetName = "CSV"

// probeAny 按文件类型分发到 probeFile (xlsx) 或 probeCSV (csv)。
func probeAny(path string, headerRow int, allow sheetFilter) ([]FileInfo, error) {
	switch core.DetectSourceKind(path) {
	case core.SourceCSV:
		return probeCSVFile(path, headerRow, allow)
	case core.SourceXLSX:
		return probeFile(path, headerRow, allow)
	default:
		return nil, core.New(core.CodeSourceFormatUnsupported, "不支持的文件格式: "+path)
	}
}

// probeCSVFile 读 CSV 首行作为表头，构造一条 SheetName="CSV" 的 FileInfo。
// 这里不记录列宽（CSV 没有该概念），allow 允许列表不含 "CSV" 时返回空切片。
func probeCSVFile(path string, headerRow int, allow sheetFilter) ([]FileInfo, error) {
	if !allow.match(csvSheetName) {
		return nil, nil
	}
	info := FileInfo{Path: path, SheetName: csvSheetName}
	if headerRow > 0 {
		r, err := excelio.OpenCSV(path, excelio.CSVOptions{})
		if err != nil {
			return nil, err
		}
		defer r.Close()
		for i := 1; i <= headerRow; i++ {
			if !r.Next() {
				if err := r.Err(); err != nil {
					return nil, err
				}
				return nil, core.Wrap(core.CodeCSVOpenFailed, "CSV 表头行超出文件范围: "+path, nil)
			}
			if i == headerRow {
				info.Headers = append([]string(nil), r.Record()...)
			}
		}
	}
	return []FileInfo{info}, nil
}

func probeFile(path string, headerRow int, allow sheetFilter) ([]FileInfo, error) {
	r, err := excelio.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	sheets := r.SheetNames()
	if len(sheets) == 0 {
		return nil, core.New("EMPTY_WORKBOOK", "工作簿没有 Sheet: "+path)
	}

	out := make([]FileInfo, 0, len(sheets))
	for _, sh := range sheets {
		if !allow.match(sh) {
			continue
		}
		var headers []string
		if headerRow > 0 {
			h, err := r.Header(sh, headerRow)
			if err != nil {
				// 完全空的 Sheet（典型场景：Excel 转 CSV 后残留的空 Sheet1）静默跳过，
				// 不让它破坏整个批量任务。HEADER_ROW_MISSING 等其他错误仍上抛。
				if core.IsEmptySheet(err) {
					continue
				}
				return nil, err
			}
			headers = h
		}
		// 列宽复制：失败不致命，最坏情况就是输出文件用默认列宽
		widths, _ := r.ColumnWidths(sh)
		out = append(out, FileInfo{Path: path, SheetName: sh, Headers: headers, ColumnWidths: widths})
	}
	return out, nil
}

// sheetFilter 简单封装"允许列表"。空列表表示通过所有。
type sheetFilter map[string]struct{}

func newSheetFilter(names []string) sheetFilter {
	if len(names) == 0 {
		return nil
	}
	m := make(sheetFilter, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

func (f sheetFilter) match(name string) bool {
	if f == nil {
		return true
	}
	_, ok := f[name]
	return ok
}
