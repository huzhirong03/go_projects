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
	path                string
	w                   *excelio.Writer
	s                   *excelio.StreamSheet
	sheet               string
	curRow              int // 已写入的最后一行（1-based）
	colWidthsApplied    bool
	defaultHeightLocked bool // 已经尝试设过 defaultRowHeight（幂等）
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
// 同时对所有纯字符串 value 应用 coerceScalar，把"原本是数字的文本"恢复为 float64，
// 避免 Excel 显示"数字以文本形式存储"（左上角绿三角）。
func buildAdjustedRow(values []any, srcRow, dstRow int) []any {
	out := make([]any, len(values))
	for i, v := range values {
		cell, isCell := v.(excelize.Cell)
		if !isCell {
			// 普通 any：若是 string 则尝试恢复为数字，否则原样透传
			if s, ok := v.(string); ok {
				out[i] = coerceScalar(s)
			} else {
				out[i] = v
			}
			continue
		}
		// 有公式：尝试同行偏移重写
		if cell.Formula != "" && srcRow > 0 {
			rewritten, ok := excelio.RewriteFormulaSameRow(cell.Formula, srcRow, dstRow)
			if ok {
				// 把 "=" 前缀剥掉再赋回，避免 excelize 内部输出 "==..."
				cell.Formula = strings.TrimPrefix(rewritten, "=")
				out[i] = cell
				continue
			}
			// 公式不安全：回退写缓存值（也 coerce）
			if s, ok := cell.Value.(string); ok {
				out[i] = coerceScalar(s)
			} else {
				out[i] = cell.Value
			}
			continue
		}
		// excelize.Cell 但无公式：对其 Value 做 coerce 后原封放回 Cell
		if s, ok := cell.Value.(string); ok {
			cell.Value = coerceScalar(s)
		}
		out[i] = cell
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

// ensureDefaultHeightForPics 在第一次迁移图片前，把目标 sheet 的 defaultRowHeight
// 设为源命中行的代表 ht。目的：让 excelize.AddPictureFromBytes 在 twoCellAnchor
// 模式下用"正确的行高 base"反算 to.row，避免源 36pt、目标默认 15pt 的差异导致
// 图片在新文件里跨 2~3 行变形。
//
// 幂等：多次调用只生效一次（第一次传入的 ht）。ht <= 0 时不设置。
func (o *outputStream) ensureDefaultHeightForPics(ht float64) {
	if o.defaultHeightLocked {
		return
	}
	o.defaultHeightLocked = true
	if ht > 0 {
		_ = o.w.SetSheetDefaultRowHeight(o.sheet, ht)
	}
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
