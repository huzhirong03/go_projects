package splitter

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/excelio"

	"github.com/xuri/excelize/v2"
)

// partWriter 单个输出分片（一个 xlsx 文件）。
type partWriter struct {
	path             string
	w                *excelio.Writer
	s                *excelio.StreamSheet
	sheet            string
	curRow           int
	colWidthsApplied bool
}

func newPartWriter(outPath, sheet string) (*partWriter, error) {
	if _, err := os.Stat(outPath); err == nil {
		return nil, core.Wrap("OUTPUT_CONFLICT", "输出文件已存在: "+outPath, core.ErrOutputConflict)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, core.Wrap("OUTPUT_MKDIR_FAILED", "创建输出目录失败", err)
	}
	w := excelio.NewWriter()
	s, err := w.StreamFor(sheet)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	return &partWriter{path: outPath, w: w, s: s, sheet: sheet}, nil
}

// applyColumnWidthsIfNeeded 在第一次写数据前调用一次，把源 Sheet 列宽复刻到目标 Sheet。
// 多次调用幂等。StreamWriter 限制：必须在 WriteRow 之前。
func (p *partWriter) applyColumnWidthsIfNeeded(widths map[int]float64) error {
	if p.colWidthsApplied {
		return nil
	}
	p.colWidthsApplied = true
	if len(widths) == 0 {
		return nil
	}
	return p.s.SetColumnWidths(widths)
}

// writeRow 追加一行。
//   - values 元素若为 excelize.Cell{Formula}，按 srcRow → dstRow 做"同行偏移"；
//     不安全的公式回退为写 Cell.Value。
//   - height > 0 时设置目标行行高（复刻外观）。
//   - srcRow == 0 表示"无公式偏移需求"（如表头）。
func (p *partWriter) writeRow(values []any, srcRow int, height float64) (int, error) {
	next := p.curRow + 1
	out := buildAdjustedRow(values, srcRow, next)
	if err := p.s.WriteRowWithHeight(next, out, height); err != nil {
		return 0, err
	}
	p.curRow = next
	return next, nil
}

// buildAdjustedRow 处理 values 里的 excelize.Cell：尝试同行偏移公式，不安全则回退写值。
func buildAdjustedRow(values []any, srcRow, dstRow int) []any {
	out := make([]any, len(values))
	for i, v := range values {
		cell, isCell := v.(excelize.Cell)
		if !isCell || cell.Formula == "" || srcRow <= 0 {
			out[i] = v
			continue
		}
		// excelize GetCellFormula 返回的公式已带 "="；RewriteFormulaSameRow 容错。
		// 输出回写到 Cell.Formula 时必须 strip 前缀，否则 excelize 会拼成 "==..."。
		rewritten, ok := excelio.RewriteFormulaSameRow(cell.Formula, srcRow, dstRow)
		if ok {
			cell.Formula = strings.TrimPrefix(rewritten, "=")
			out[i] = cell
			continue
		}
		out[i] = cell.Value
	}
	return out
}

// migratePictures 把源行图片迁移到当前分片的 dstRow，保持原列号（by_sheet/by_rows 用）。
// 若要改变列号（比如 by_column 需要投射），请用 migratePicturesMapped。
func (p *partWriter) migratePictures(pics []excelio.CellPictures, dstRow int) (int, error) {
	if len(pics) == 0 {
		return 0, nil
	}
	count := 0
	for _, cp := range pics {
		if err := excelio.MigratePicture(p.w.File(), p.sheet, dstRow, excelio.CellPictures{
			Row:      dstRow,
			Col:      cp.Col,
			Pictures: cp.Pictures,
		}); err != nil {
			return count, err
		}
		count += len(cp.Pictures)
	}
	return count, nil
}

func (p *partWriter) save() error {
	if err := p.w.RemoveDefaultSheet(); err != nil {
		return err
	}
	return p.w.Save(p.path)
}

func (p *partWriter) close() error {
	if p == nil || p.w == nil {
		return nil
	}
	return p.w.Close()
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

// readRowFormulas 查询某一行所有 cell 的公式，与 extractor 里的同名函数等价。
// 非公式 cell 对应位置为空字符串。失败静默置空。
func readRowFormulas(r *excelio.Reader, sheet string, row, ncells int) []string {
	out := make([]string, ncells)
	for i := 0; i < ncells; i++ {
		cellName, err := excelio.CellName(i+1, row)
		if err != nil {
			continue
		}
		f, err := r.CellFormula(sheet, cellName)
		if err != nil {
			continue
		}
		out[i] = f
	}
	return out
}

// rowToValues 把 cells + formulas 合并成 []any。
// formulas 为 nil 或对应位置为空，则直接写字符串值；
// 否则写 excelize.Cell{Formula, Value}，由 partWriter 决定后续偏移行为。
func rowToValues(cells, formulas []string) []any {
	out := make([]any, len(cells))
	for i, c := range cells {
		if i < len(formulas) && formulas[i] != "" {
			out[i] = excelize.Cell{Formula: formulas[i], Value: c}
			continue
		}
		out[i] = c
	}
	return out
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
