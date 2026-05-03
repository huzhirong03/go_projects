package extractor

import (
	"os"
	"path/filepath"
	"strings"

	"excel-master/internal/core"
	"excel-master/internal/excelio"

	"github.com/xuri/excelize/v2"
)

// perSourceWriter：每个源文件一个输出文件（原汁原味路径）。
//
// 不同于 per_keyword / merged 的"流式重写"，这里使用"复制源文件 + 删除非命中行"，
// 这样源文件的所有样式（单元格填充/字体/边框/合并单元格/条件格式/数据验证/
// 批注/图片锚点/冻结窗格/筛选器……）全部 1:1 保留。
//
// 代价：
//   - 对大文件走 excelize.OpenFile（全量加载），内存占用高；
//   - RemoveRow 是 O(n) 单行调用，大量删除场景耗时显著；
//   - 为了不破坏原样，不追加"命中关键词"列；如果用户需要这信息，
//     换用 per_keyword / merged 策略即可。
//
// 文件命名：<源文件名去扩展名>_已提取_<时间戳>.xlsx。
type perSourceWriter struct {
	outDir    string
	headerRow int                       // 1-based；<=0 表示无表头（不强制保留第一行）
	sheets    []string                  // 用户选中的 Sheet 列表；空=保留所有（但下面会进一步按"是否有命中"过滤）
	hits      map[string]*perSourceHits // key = 源文件绝对路径
	imgCount  int
	ts        string
}

// perSourceHits 单个源文件的累计命中。
type perSourceHits struct {
	path      string           // 源文件路径
	sheetRows map[string][]int // sheet 名 -> 命中的 1-based 源行号列表（可能有重复，内部会去重）
	picCount  int              // 命中行包含的图片数（不触发迁移，仅用于统计）
}

func newPerSourceWriter(outDir string, headerRow int, sheets []string) *perSourceWriter {
	// 复制一份 sheets 避免调用方后续修改
	sheetsCopy := append([]string(nil), sheets...)
	return &perSourceWriter{
		outDir:    outDir,
		headerRow: headerRow,
		sheets:    sheetsCopy,
		hits:      map[string]*perSourceHits{},
		ts:        timestamp(),
	}
}

// Begin 对本 writer 是 no-op：schema 对原汁原味路径不需要（不重新写表头）。
func (p *perSourceWriter) Begin(schema *UnifiedSchema) error {
	return nil
}

// EmitRow 仅累积命中信息；真正的文件操作在 Finalize 里做。
func (p *perSourceWriter) EmitRow(row MatchedRow, fs *FileSchema) error {
	h, ok := p.hits[row.SourceFile]
	if !ok {
		h = &perSourceHits{path: row.SourceFile, sheetRows: map[string][]int{}}
		p.hits[row.SourceFile] = h
	}
	sheet := fs.File.SheetName
	h.sheetRows[sheet] = append(h.sheetRows[sheet], row.SourceRow)
	h.picCount += len(row.Pictures)
	return nil
}

// Finalize 对每个有命中的源文件执行"复制 + 删行"，返回生成的输出文件路径列表。
func (p *perSourceWriter) Finalize() ([]string, error) {
	paths := make([]string, 0, len(p.hits))
	for _, h := range p.hits {
		outPath, err := p.exportOne(h)
		if err != nil {
			return paths, err
		}
		paths = append(paths, outPath)
		p.imgCount += h.picCount
	}
	return paths, nil
}

// Close 对原汁原味路径也是 no-op（没有持久打开的句柄）。
func (p *perSourceWriter) Close() error { return nil }

func (p *perSourceWriter) ImagesMigrated() int { return p.imgCount }

// exportOne 生成单个源文件的输出：
//  1. 二进制复制 src -> dst
//  2. 只保留用户选中的 Sheet 中 **有命中** 的那些（没命中的 Sheet 一律删掉）
//  3. 对每个保留的 Sheet，只保留 表头行 + 命中行
//  4. Save
func (p *perSourceWriter) exportOne(h *perSourceHits) (string, error) {
	// 目标路径
	base := filepath.Base(h.path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	fname := sanitizeFileName(stem) + "_已提取_" + p.ts + ".xlsx"
	outPath := filepath.Join(p.outDir, fname)

	// 1) 文件级复制
	if err := excelio.CloneFile(h.path, outPath); err != nil {
		return "", err
	}

	// 2) 打开拷贝
	f, err := excelize.OpenFile(outPath)
	if err != nil {
		_ = os.Remove(outPath)
		return "", core.Wrap("EXCEL_OPEN_FAILED", "打开拷贝后的文件失败: "+outPath, err)
	}
	// 任何后续错误都要尝试清理半成品
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(outPath)
	}

	// 3) 收集要保留的 Sheet 名：命中过的 sheet
	keepSheets := make([]string, 0, len(h.sheetRows))
	for s := range h.sheetRows {
		keepSheets = append(keepSheets, s)
	}

	// 4) 只保留命中过的 Sheet（如果还有别的 Sheet 会被删掉）
	if err := excelio.KeepSheetsOnly(f, keepSheets); err != nil {
		cleanup()
		return "", err
	}

	// 5) 对每个保留的 Sheet 过滤行
	for sheet, rows := range h.sheetRows {
		keep := make([]int, 0, len(rows)+1)
		if p.headerRow > 0 {
			keep = append(keep, p.headerRow)
		}
		keep = append(keep, rows...)
		if err := excelio.FilterRowsInSheet(f, sheet, excelio.SortedUnique(keep)); err != nil {
			cleanup()
			return "", err
		}
	}

	// 6) 保存
	if err := f.Save(); err != nil {
		cleanup()
		return "", core.Wrap("EXCEL_SAVE_FAILED", "保存输出文件失败: "+outPath, err)
	}
	if err := f.Close(); err != nil {
		return "", core.Wrap("EXCEL_CLOSE_FAILED", "关闭输出文件失败: "+outPath, err)
	}
	return outPath, nil
}
