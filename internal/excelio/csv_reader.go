package excelio

import (
	"bufio"
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

	cr := csv.NewReader(br)
	cr.LazyQuotes = true    // 业务 CSV 经常有非法引号，强制容忍
	cr.FieldsPerRecord = -1 // 容忍每行字段数不同
	cr.ReuseRecord = true
	if d := pickDelimiter(opts.Delimiter); d != 0 {
		cr.Comma = d
	}

	out := &CSVReader{f: f, csv: cr, det: det}
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
// 接受常见别名："comma" / "tab" / "\t" / ";" 等；不合法或空返回 0（表示走默认逗号）。
func pickDelimiter(s string) rune {
	switch s {
	case "", "auto", ",", "comma":
		return 0
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
