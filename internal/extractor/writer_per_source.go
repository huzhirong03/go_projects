package extractor

import (
	"path/filepath"
	"strings"

	"excel-master/internal/excelio"
)

// perSourceWriter：每个源文件一个输出文件（原汁原味路径，纯 zip+xml 手术）。
//
// 不同于 per_keyword / merged 的"流式重写"，这里使用"zip 手术：
// 二进制复制 + 选择性 XML 重写"，源文件的所有样式（单元格填充/字体/边框/
// 合并单元格/条件格式/数据验证/批注/图片锚点/冻结窗格/筛选器……）全部 1:1 保留。
//
// 实现：调用 excelio.CloneAndExtractZipMulti，传入命中过的多个 sheet 各自的 keepRows
// （表头行 + 命中行）。完全绕过 excelize.OpenFile/Save，避免 issue #2061 类样式丢失。
//
// 代价 / 限制：
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

// exportOne 生成单个源文件的输出：调 zip 手术一次性完成
// “只保留命中 sheet，每个 sheet 仅保留表头行 + 命中行”。
func (p *perSourceWriter) exportOne(h *perSourceHits) (string, error) {
	// 目标路径
	base := filepath.Base(h.path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	fname := sanitizeFileName(stem) + "_已提取_" + p.ts + ".xlsx"
	outPath := filepath.Join(p.outDir, fname)

	// 构造 keepSheetRows: 每个命中 sheet 保留 表头行 + 命中行，去重后传入
	keepSheetRows := make(map[string][]int, len(h.sheetRows))
	for sheet, rows := range h.sheetRows {
		keep := make([]int, 0, len(rows)+1)
		if p.headerRow > 0 {
			keep = append(keep, p.headerRow)
		}
		keep = append(keep, rows...)
		keepSheetRows[sheet] = excelio.SortedUnique(keep)
	}

	// 一次性 zip 手术：复制 + 按 sheet/行 模板过滤 + 错误时自动清半成品
	if err := excelio.CloneAndExtractZipMulti(h.path, outPath, keepSheetRows); err != nil {
		return "", err
	}
	return outPath, nil
}
