package excelio

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"io"
	"os"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"excel-master/internal/core"
)

// CSVOptions 打开 CSV 时的可选参数。
//
//	Encoding   "" / "auto" 走自动嗅探；其他值走 encodingByName 显式指定
//	           （utf-8/utf-8-bom/gbk/gb18030/big5/utf-16le/utf-16be）
//	Delimiter  字段分隔符；"" 默认逗号；只取首个 rune；不允许 \r \n
type CSVOptions struct {
	Encoding  string
	Delimiter string
}

// CSVReader 是 CSV 的行迭代器，对齐 xlsx 的 Rows() 风格：
//
//	for r.Next() {
//	    record := r.Record()
//	    ...
//	}
//	if err := r.Err(); err != nil { ... }
//	r.Close()
//
// 内部走流式解码，整文件内存占用恒定（bufio 64KB + 单条 record）。
type CSVReader struct {
	f       *os.File
	csv     *csv.Reader
	det     EncodingDetect
	delim   rune // 实际生效的分隔符（显式指定或嗅探得到）
	rec     []string
	err     error
	row     int // 1-based 已读到的行号（与 xlsx headerRow 对齐）
	bytesAt int64
	total   int64
}

// OpenCSV 打开并嗅探编码，返回流式 CSVReader。错误统一包成 core.AppError。
func OpenCSV(path string, opts CSVOptions) (*CSVReader, error) {
	det, err := DetectCSVEncoding(path, opts.Encoding)
	if err != nil {
		return nil, core.Wrap(core.CodeCSVOpenFailed, "嗅探 CSV 编码失败", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, core.Wrap(core.CodeCSVOpenFailed, "打开 CSV 文件失败", err)
	}
	stat, _ := f.Stat()

	// 解码层：UTF-8 直接走原流；其他编码套 transform.NewReader 转 UTF-8。
	var r io.Reader = f
	if det.Enc != nil && det.Enc != unicode.UTF8 {
		r = transform.NewReader(f, det.Enc.NewDecoder())
	}

	br := bufio.NewReaderSize(r, 64*1024)
	// UTF-8 BOM：transform 不剥（Enc==UTF8 没走 transform），自己 peek + Discard。
	if det.SkipBOM {
		if b, _ := br.Peek(3); len(b) == 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
			_, _ = br.Discard(3)
		}
	}

	// 决定分隔符：
	//   显式指定（","/";"/"\t"/"|" 或别名）→ 直接用
	//   未指定 / "auto" → 嗅探前 8 KB，选字段数稳定且 >= 2 的候选；都不行 fallback ','
	chosen := pickDelimiter(opts.Delimiter)
	if chosen == 0 {
		if d, ok := sniffDelimiter(br); ok {
			chosen = d
		} else {
			chosen = ','
		}
	}

	cr := csv.NewReader(br)
	cr.Comma = chosen
	cr.LazyQuotes = true    // 业务 CSV 经常有非法引号，强制容忍
	cr.FieldsPerRecord = -1 // 容忍每行字段数不同
	cr.ReuseRecord = true

	out := &CSVReader{f: f, csv: cr, det: det, delim: chosen}
	if stat != nil {
		out.total = stat.Size()
	}
	return out, nil
}

// Next 读下一行。返回 false 表示读完或出错；调用方在 false 后必须查 Err()。
func (r *CSVReader) Next() bool {
	rec, err := r.csv.Read()
	if err == io.EOF {
		return false
	}
	if err != nil {
		r.err = core.Wrap(core.CodeCSVDecodeFailed, "解析 CSV 行失败", err)
		return false
	}
	r.row++
	// ReuseRecord = true 时 rec 底层 buffer 会被复用，调用方如需保留必须自己拷贝。
	r.rec = rec
	return true
}

// Record 当前行的字段切片。**不可长期持有**：下一次 Next 会复用底层 slice。
func (r *CSVReader) Record() []string { return r.rec }

// Row 当前已读的物理行号（1-based）。
func (r *CSVReader) Row() int { return r.row }

// Err 读取过程中的错误（io.EOF 不算）。
func (r *CSVReader) Err() error { return r.err }

// Encoding 实际生效的编码名，用于日志/UI 反馈。
func (r *CSVReader) Encoding() string { return r.det.Name }

// Delimiter 实际生效的分隔符（显式指定或自动嗅探的结果）。日志/UI 用。
func (r *CSVReader) Delimiter() rune { return r.delim }

// FileSize 文件总字节数（用于按字节比例做进度条；Stat 失败时为 0）。
func (r *CSVReader) FileSize() int64 { return r.total }

// Close 关闭底层文件。
func (r *CSVReader) Close() error {
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// pickDelimiter 把 UI/任务参数里的分隔符字符串转成 rune。
//
// 返回值约定：
//   - 0  = 用户没明确指定（"" / "auto"），调用方应触发 sniffDelimiter
//   - 非 0 = 用户明确选了某个分隔符（含显式选 ","/"comma"），直接使用，不嗅探
//
// 别名："comma"=','  "semicolon"=';'  "tab"/"TAB"/"\t"=\t  "pipe"='|'
func pickDelimiter(s string) rune {
	switch s {
	case "", "auto":
		return 0 // 触发嗅探
	case ",", "comma":
		return ','
	case ";", "semicolon":
		return ';'
	case "\t", "tab", "TAB":
		return '\t'
	case "|", "pipe":
		return '|'
	}
	for _, c := range s {
		if c == '\r' || c == '\n' {
			return 0
		}
		return c
	}
	return 0
}

// sniffDelimiter 从 br 中 Peek 前 8 KB（不消耗读位置），尝试在候选
// {',', ';', '\t', '|'} 中找出最可能的字段分隔符。
//
// 算法：对每个候选，用真实的 csv.Reader 切前 N 行（自动处理 quote 转义），
// 统计每行字段数；选"字段数中位数 >= 2 且 90%+ 行间一致"的候选。
// 同分时按 ',' > ';' > '\t' > '|' 优先级（逗号优先保兼容）。
//
// 这是 Python csv.Sniffer / R read_csv / Excel 导入向导都在用的标准做法。
// Peek 不会推进 br 的读取位置，所以嗅探完后 csv.Reader 还能从头读完整数据。
func sniffDelimiter(br *bufio.Reader) (rune, bool) {
	const sniffSize = 8 * 1024
	buf, _ := br.Peek(sniffSize)
	if len(buf) < 4 { // 至少要有几行才有意义
		return 0, false
	}

	candidates := []rune{',', ';', '\t', '|'} // 顺序决定同分时优先级
	var bestRune rune
	var bestScore float64 = -1

	for _, c := range candidates {
		score := scoreSniffCandidate(buf, c)
		if score > bestScore {
			bestScore = score
			bestRune = c
		}
	}
	if bestScore <= 0 {
		return 0, false
	}
	return bestRune, true
}

// scoreSniffCandidate 用候选分隔符切前 50 行，返回质量分数：
//   - 字段数 < 2 的行视为切失败 -> 整体 0 分
//   - 字段数中位数 * 行间一致性比例（0..1）即为分数
//
// 真实 CSV 用此函数：逗号 CSV 用逗号切 → 6 字段 × 100% = 6.0；用分号切 → 1 字段 → 0。
func scoreSniffCandidate(buf []byte, d rune) float64 {
	cr := csv.NewReader(bytes.NewReader(buf))
	cr.Comma = d
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1

	var counts []int
	for i := 0; i < 50; i++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 解析失败的候选直接淘汰（quote 错位等极端情况）
			break
		}
		if len(rec) == 0 {
			continue
		}
		counts = append(counts, len(rec))
	}
	// 由于 Peek 截断，最后一行很可能不完整：丢弃
	if len(counts) >= 2 {
		counts = counts[:len(counts)-1]
	}
	if len(counts) < 1 {
		return 0
	}

	// 用首行作为基线（CSV 表头通常字段数 = 数据行）
	first := counts[0]
	if first < 2 {
		return 0 // 切不开就是错的分隔符
	}
	same := 0
	for _, c := range counts {
		if c == first {
			same++
		}
	}
	consistency := float64(same) / float64(len(counts))
	if consistency < 0.9 {
		return 0 // 行与行字段数不一致 = 切错了
	}
	return float64(first) * consistency
}
