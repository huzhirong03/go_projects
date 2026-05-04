package excelio

// fast_reader.go：基于 github.com/thedatashed/xlsxreader 的 xlsx 流式行读取，
// 作为 excelize.Rows 的"快速扫描"替代。
//
// 为什么：PoC (internal/excelio/poc_xlsxreader_test.go) 实测 100k 行 fixture
// 扫描阶段 xlsxreader 3.33s vs excelize 5.03s，约 1.51× 加速；且无 CGO 依赖，
// 纯 Go SAX 解析 + 低内存（不缓存整个 sheet data）。
//
// 职责边界：只做 Phase 2 的"扫描 + 取 cell values"。公式保留、行高、样式、图片
// 全部仍由 excelize 处理，fast_reader 不碰写路径。所以业务语义完全等价于
// excelize 扫描路径——只是更快。
//
// 关键实现点：
//  1. xlsxreader.Row.Cells 是稀疏的（空 cell 不在里面），要还原为密集 []string
//     才能和 excelize.Columns() 行为一致。对齐到"从 A 列开始到最后一个非空 cell"。
//  2. ReadRows 返回 channel，内部用 goroutine 推送；Close 必须关闭以释放 goroutine。
//  3. xlsxreader 对标准 xlsx 兼容性足够，但部分"非标文件"（如 WPS 加密/稀有元素）
//     可能解析失败——此时 Open 返回错误，调用方应降级到 excelize。

import (
	"strings"

	"excel-master/internal/core"

	"github.com/thedatashed/xlsxreader"
)

// FastRowIterator 用 xlsxreader 实现 RowIter。
// 由 OpenFast 创建；调用方必须 Close。
type FastRowIterator struct {
	ch     chan xlsxreader.Row
	cur    xlsxreader.Row
	err    error
	sheet  string
	closed bool
}

// FastReader 是对一个 xlsx 的 xlsxreader 句柄包装，只用于扫描场景。
// 生命周期：OpenFast -> Iterate(sheet) -> it.Close() -> r.Close()。
//
// 持有 *XlsxFileCloser（嵌入 XlsxFile 并提供 Close）。XlsxFile 本身的 Sheets
// 和 ReadRows 方法可以通过嵌入直接调用。
type FastReader struct {
	xl   *xlsxreader.XlsxFileCloser
	path string
}

// OpenFast 用 xlsxreader 打开 xlsx 文件。失败时调用方应考虑回退 Open（excelize）。
func OpenFast(path string) (*FastReader, error) {
	xl, err := xlsxreader.OpenFile(path)
	if err != nil {
		return nil, core.Wrap("EXCEL_OPEN_FAILED", "xlsxreader 打开失败: "+path, err)
	}
	return &FastReader{xl: xl, path: path}, nil
}

// Close 释放 xlsxreader 内部的 zip 句柄和 goroutine。幂等。
func (r *FastReader) Close() error {
	if r == nil || r.xl == nil {
		return nil
	}
	err := r.xl.Close()
	r.xl = nil
	return err
}

// SheetNames 返回 xlsx 里所有 sheet 名。
func (r *FastReader) SheetNames() []string {
	if r == nil || r.xl == nil {
		return nil
	}
	return append([]string(nil), r.xl.Sheets...)
}

// Iterate 创建指定 sheet 的行迭代器。调用方必须 Close。
//
// 行号从 1 开始；xlsxreader 自带的 Row.Index 直接采用（与 excelize 一致）。
// 如果 sheet 在 workbook 里不存在，返回 error 而非 panic。
func (r *FastReader) Iterate(sheet string) (*FastRowIterator, error) {
	if r == nil || r.xl == nil {
		return nil, core.New("EXCEL_READ_FAILED", "FastReader 未打开")
	}
	found := false
	for _, s := range r.xl.Sheets {
		if s == sheet {
			found = true
			break
		}
	}
	if !found {
		return nil, core.New("SHEET_NOT_FOUND", "sheet 不存在: "+sheet)
	}
	return &FastRowIterator{
		ch:    r.xl.ReadRows(sheet),
		sheet: sheet,
	}, nil
}

// Next 推进到下一行。返回 false 表示迭代结束或出错（Err() 区分）。
func (it *FastRowIterator) Next() bool {
	if it == nil || it.closed {
		return false
	}
	row, ok := <-it.ch
	if !ok {
		return false
	}
	if row.Error != nil {
		it.err = core.Wrap("EXCEL_READ_FAILED", "xlsxreader 读行失败", row.Error)
		return false
	}
	it.cur = row
	return true
}

// Columns 返回当前行的单元格文本切片，长度 = 从 A 列起到最后一个非空 cell 的列数。
// 中间空 cell 填 ""，与 excelize.Rows.Columns() 的密集语义保持一致。
//
// 若当前行没有任何 cell（极少见，但 xlsxreader 可能过滤空行），返回 nil, nil。
func (it *FastRowIterator) Columns() ([]string, error) {
	if it == nil || len(it.cur.Cells) == 0 {
		return nil, nil
	}
	maxIdx := -1
	for _, c := range it.cur.Cells {
		idx := columnLettersToIndex(c.Column)
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	if maxIdx < 0 {
		return nil, nil
	}
	out := make([]string, maxIdx+1)
	for _, c := range it.cur.Cells {
		idx := columnLettersToIndex(c.Column)
		if idx < 0 || idx > maxIdx {
			continue
		}
		out[idx] = c.Value
	}
	return out, nil
}

// RowNum 返回当前行的 1-based 行号（对应 xlsx 里 <row r="N"/> 的 N）。
func (it *FastRowIterator) RowNum() int {
	if it == nil {
		return 0
	}
	return it.cur.Index
}

// Err 返回迭代过程中累积的错误。正常结束时为 nil。
func (it *FastRowIterator) Err() error {
	if it == nil {
		return nil
	}
	return it.err
}

// Close 释放迭代器。为了彻底释放 xlsxreader 的 goroutine，会尽量把 channel 拉空。
// 主 FastReader.Close 才真正关 zip 句柄；调用方两个都要 Close。
func (it *FastRowIterator) Close() error {
	if it == nil || it.closed {
		return nil
	}
	it.closed = true
	// drain channel 让 xlsxreader 内部 goroutine 退出
	go func(ch chan xlsxreader.Row) {
		for range ch {
		}
	}(it.ch)
	return nil
}

// columnLettersToIndex 把列字母转为 0-based 索引："A"=0, "Z"=25, "AA"=26, "AB"=27…
// 非字母或空串返回 -1（调用方应跳过该 cell）。
func columnLettersToIndex(col string) int {
	if col == "" {
		return -1
	}
	col = strings.ToUpper(col)
	idx := 0
	for i := 0; i < len(col); i++ {
		c := col[i]
		if c < 'A' || c > 'Z' {
			return -1
		}
		idx = idx*26 + int(c-'A') + 1
	}
	return idx - 1
}
