package extractor

import (
	"context"
	"path/filepath"
	"strings"

	"excel-master/internal/core"
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
// 文件命名：<prefix><源文件名去扩展名>_已提取_<时间戳>.xlsx。默认 prefix 为空。
type perSourceWriter struct {
	outDir    string
	headerRow int      // 1-based；<=0 表示无表头（不强制保留第一行）
	sheets    []string // 用户选中的 Sheet 列表；空=保留所有（但下面会进一步按"是否有命中"过滤）
	prefix    string
	hits      map[string]*perSourceHits // key = 源文件绝对路径
	imgCount  int
	ts        string
}

// perSourceHits 单个源文件的累计命中。
// xlsx 源走 zip 手术：只需记录 sheetRows（行号）。
// csv 源走流式降级：需要记录完整行内容（csvRows），没有 sheet 概念。
type perSourceHits struct {
	path      string           // 源文件路径
	sheetRows map[string][]int // sheet 名 -> 命中的 1-based 源行号列表（可能有重复，内部会去重）
	picCount  int              // 命中行包含的图片数（不触发迁移，仅用于统计）
	csvRows   []MatchedRow     // 仅 csv 源使用：需要整行内容来流式写出，不走 zip 手术
	csvSchema *FileSchema      // 仅 csv 源使用：用于列宽/列名（复用统一 schema 逻辑）
}

func newPerSourceWriter(outDir string, headerRow int, sheets []string, prefix string) *perSourceWriter {
	// 复制一份 sheets 避免调用方后续修改
	sheetsCopy := append([]string(nil), sheets...)
	return &perSourceWriter{
		outDir:    outDir,
		headerRow: headerRow,
		sheets:    sheetsCopy,
		prefix:    prefix,
		hits:      map[string]*perSourceHits{},
		ts:        timestamp(),
	}
}

// Begin 对本 writer 是 no-op：schema 对原汁原味路径不需要（不重新写表头）。
func (p *perSourceWriter) Begin(schema *UnifiedSchema) error {
	return nil
}

// EmitRow 仅累积命中信息；真正的文件操作在 Finalize 里做。
// xlsx 源：记录行号，Finalize 时用 zip 手术按行过滤；
// csv 源：记录整行内容 + schema，Finalize 时流式写纯数据 xlsx。
func (p *perSourceWriter) EmitRow(row MatchedRow, fs *FileSchema) error {
	h, ok := p.hits[row.SourceFile]
	if !ok {
		h = &perSourceHits{path: row.SourceFile, sheetRows: map[string][]int{}}
		p.hits[row.SourceFile] = h
	}
	if core.DetectSourceKind(row.SourceFile) == core.SourceCSV {
		h.csvRows = append(h.csvRows, row)
		h.csvSchema = fs
		return nil
	}
	sheet := fs.File.SheetName
	h.sheetRows[sheet] = append(h.sheetRows[sheet], row.SourceRow)
	h.picCount += len(row.Pictures)
	return nil
}

// Finalize 对每个有命中的源文件执行"复制 + 删行"，返回生成的输出文件路径列表。
func (p *perSourceWriter) Finalize() ([]string, error) {
	return p.finalize(nil, nil)
}

func (p *perSourceWriter) FinalizeWithPrompt(ctx context.Context, emitter core.EventEmitter) ([]string, error) {
	return p.finalize(ctx, emitter)
}

func (p *perSourceWriter) finalize(ctx context.Context, emitter core.EventEmitter) ([]string, error) {
	paths := make([]string, 0, len(p.hits))
	for _, h := range p.hits {
		for {
			skipFile := false
			switch askOfficeLockDecision(ctx, emitter, h.path) {
			case fileOpenRetry:
				continue
			case fileOpenSkip:
				emitter.Log(core.LogWarn, "已跳过正在打开的文件: "+h.path)
				skipFile = true
			case fileOpenCancel:
				return paths, core.ErrCanceled
			}
			if skipFile {
				break
			}
			outPath, err := p.exportOne(h)
			if err == nil {
				paths = append(paths, outPath)
				p.imgCount += h.picCount
				break
			}
			switch askFileOpenDecision(ctx, emitter, h.path, err) {
			case fileOpenRetry:
				continue
			case fileOpenSkip:
				emitter.Log(core.LogWarn, "已跳过无法读取的文件: "+h.path)
				break
			case fileOpenAbort:
				return paths, err
			default:
				return paths, core.ErrCanceled
			}
			break
		}
	}
	return paths, nil
}

// Close 对原汁原味路径也是 no-op（没有持久打开的句柄）。
func (p *perSourceWriter) Close() error { return nil }

func (p *perSourceWriter) ImagesMigrated() int { return p.imgCount }

// exportOne 生成单个源文件的输出：
//   - xlsx 源：走 zip 手术（CloneAndExtractZipMulti），保留样式/图片/合并等。
//   - csv 源：走流式 StreamWriter（exportOneCSV），输出纯数据 xlsx。
func (p *perSourceWriter) exportOne(h *perSourceHits) (string, error) {
	if core.DetectSourceKind(h.path) == core.SourceCSV {
		return p.exportOneCSV(h)
	}

	base := filepath.Base(h.path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	fname := sanitizeFileName(p.prefix+stem) + "_已提取_" + p.ts + ".xlsx"
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

	if err := excelio.CloneAndExtractZipMulti(h.path, outPath, keepSheetRows); err != nil {
		return "", err
	}
	return outPath, nil
}

// exportOneCSV 把一个 CSV 源的命中行写成纯数据 xlsx。
// 没有样式/图片/合并单元格（CSV 自身就没有），表头取 schema.Headers。
func (p *perSourceWriter) exportOneCSV(h *perSourceHits) (string, error) {
	base := filepath.Base(h.path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	fname := sanitizeFileName(p.prefix+stem) + "_已提取_" + p.ts + ".xlsx"
	outPath := filepath.Join(p.outDir, fname)

	const sheet = "结果"
	out, err := openOutput(outPath, sheet)
	if err != nil {
		return "", err
	}
	defer out.close()

	// 表头：用 schema 里 CSV 文件的 Headers（探针阶段读过）。
	var headers []string
	if h.csvSchema != nil {
		headers = h.csvSchema.File.Headers
	}
	if p.headerRow > 0 && len(headers) > 0 {
		if err := out.writeHeader(headers); err != nil {
			return "", err
		}
	}
	for _, r := range h.csvRows {
		if _, err := out.writeRow(r.Values, 0, 0); err != nil {
			return "", err
		}
	}
	if err := out.save(); err != nil {
		return "", err
	}
	return outPath, nil
}
