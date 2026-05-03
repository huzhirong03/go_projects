package extractor

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/excelio"

	"github.com/xuri/excelize/v2"
)

// outputStream 代表一个正在打开的输出 xlsx 文件及其 StreamSheet。
type outputStream struct {
	path             string
	w                *excelio.Writer
	s                *excelio.StreamSheet
	sheet            string
	curRow           int // 已写入的最后一行（1-based）
	colWidthsApplied bool
}

// openOutput 创建一个输出文件的 writer（文件不立即落盘，Save 时才写）。
// 若 outPath 已存在，返回 ErrOutputConflict。
func openOutput(outPath, sheet string) (*outputStream, error) {
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
	return &outputStream{path: outPath, w: w, s: s, sheet: sheet, curRow: 0}, nil
}

// writeHeader 写表头，并把 cursor 推到第 1 行。
// 注意：调用方应当先调用 applyColumnWidthsIfNeeded，再调用 writeHeader（StreamWriter 限制：列宽必须先设）。
func (o *outputStream) writeHeader(columns []string, extra ...string) error {
	values := make([]any, 0, len(columns)+len(extra))
	for _, c := range columns {
		values = append(values, c)
	}
	for _, c := range extra {
		values = append(values, c)
	}
	if err := o.s.WriteRow(1, values); err != nil {
		return err
	}
	o.curRow = 1
	return nil
}

// writeRow 追加一行数据，返回新行号。
//
// V1.1 起：
//   - values 元素若为 excelize.Cell 且带 Formula，writeRow 会按
//     srcRow → dstRow 做"同行偏移重写"；不安全的公式自动回退为写 Cell.Value。
//   - height > 0 时同时设置目标行高度（复刻源行外观）。
//   - srcRow == 0 表示"无公式偏移需求"（如表头行）。
func (o *outputStream) writeRow(values []any, srcRow int, height float64) (int, error) {
	next := o.curRow + 1
	out := buildAdjustedRow(values, srcRow, next)
	if err := o.s.WriteRowWithHeight(next, out, height); err != nil {
		return 0, err
	}
	o.curRow = next
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
		// excelize GetCellFormula 返回的公式已经带 "="，RewriteFormulaSameRow 也容错处理
		rewritten, ok := excelio.RewriteFormulaSameRow(cell.Formula, srcRow, dstRow)
		if ok {
			// 把 "=" 前缀剥掉再赋回，避免 excelize 内部输出 "==..."
			cell.Formula = strings.TrimPrefix(rewritten, "=")
			out[i] = cell
			continue
		}
		// 不安全：回退写缓存值
		out[i] = cell.Value
	}
	return out
}

// applyColumnWidthsIfNeeded 在第一次写数据前调用，把统一 schema 的列宽应用到流式 Sheet。
// 多次调用幂等，不重复设置。
func (o *outputStream) applyColumnWidthsIfNeeded(widths map[int]float64) error {
	if o.colWidthsApplied {
		return nil
	}
	o.colWidthsApplied = true
	if len(widths) == 0 {
		return nil
	}
	return o.s.SetColumnWidths(widths)
}

// migratePictures 把源行的所有图片重新插入到目标行。
// schema 用于把源列号翻译成统一列（保持"产品图"还在"产品图"那列）。
// 返回成功迁移的图片张数。
func (o *outputStream) migratePictures(
	pics []excelio.CellPictures,
	fs *FileSchema,
	dstRow int,
	unifiedWidth int,
) (int, error) {
	if len(pics) == 0 {
		return 0, nil
	}
	count := 0
	for _, cp := range pics {
		srcCol0 := cp.Col - 1
		uIdx := fs.UnifiedColFromSource(srcCol0)
		if uIdx < 0 || uIdx >= unifiedWidth {
			// 该列在统一 schema 里被丢弃了，跳过。
			continue
		}
		dstCol := uIdx + 1
		if err := excelio.MigratePicture(o.w.File(), o.sheet, dstRow, excelio.CellPictures{
			Row:      dstRow,
			Col:      dstCol,
			Pictures: cp.Pictures,
		}); err != nil {
			return count, err
		}
		count += len(cp.Pictures)
	}
	return count, nil
}

// save 落盘并关闭。会先移除 excelize 默认的 Sheet1，避免输出文件多一个空 Sheet。
func (o *outputStream) save() error {
	if err := o.w.RemoveDefaultSheet(); err != nil {
		return err
	}
	if err := o.w.Save(o.path); err != nil {
		return err
	}
	return nil
}

func (o *outputStream) close() error {
	if o == nil || o.w == nil {
		return nil
	}
	return o.w.Close()
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

// timestamp 生成紧凑时间戳，用于默认输出文件名。
func timestamp() string {
	return time.Now().Format("20060102_150405")
}
