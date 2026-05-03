package excelio

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// 把 utf-8 字符串编码到指定 encoding 后写文件，方便构造各编码样本。
func writeEncoded(t *testing.T, dir, name string, payload []byte, prefix []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	var buf bytes.Buffer
	if len(prefix) > 0 {
		buf.Write(prefix)
	}
	buf.Write(payload)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("写样本失败 %s: %v", p, err)
	}
	return p
}

func encode(t *testing.T, src string, enc transform.Transformer) []byte {
	t.Helper()
	out, _, err := transform.Bytes(enc, []byte(src))
	if err != nil {
		t.Fatalf("encode 失败: %v", err)
	}
	return out
}

func readAllRecords(t *testing.T, path string, opts CSVOptions) ([][]string, *CSVReader) {
	t.Helper()
	r, err := OpenCSV(path, opts)
	if err != nil {
		t.Fatalf("OpenCSV 失败 %s: %v", path, err)
	}
	var rows [][]string
	for r.Next() {
		// ReuseRecord = true，必须拷贝
		cp := append([]string(nil), r.Record()...)
		rows = append(rows, cp)
	}
	if err := r.Err(); err != nil {
		_ = r.Close()
		t.Fatalf("迭代失败 %s: %v", path, err)
	}
	return rows, r
}

// T1：UTF-8 无 BOM，纯英文。
func TestCSVReader_UTF8NoBOM(t *testing.T) {
	dir := t.TempDir()
	p := writeEncoded(t, dir, "utf8.csv", []byte("name,age\nalice,30\nbob,25\n"), nil)
	rows, r := readAllRecords(t, p, CSVOptions{})
	defer r.Close()
	if got, want := r.Encoding(), "utf-8"; got != want {
		t.Fatalf("encoding got=%s want=%s", got, want)
	}
	if len(rows) != 3 || rows[1][0] != "alice" {
		t.Fatalf("rows=%v", rows)
	}
}

// T2：UTF-8 BOM + 中文，验证 BOM 被剥（首字段不带 \uFEFF）。
func TestCSVReader_UTF8BOM_Chinese(t *testing.T) {
	dir := t.TempDir()
	body := []byte("姓名,年级\n张三,六年级\n李四,七年级\n")
	bom := []byte{0xEF, 0xBB, 0xBF}
	p := writeEncoded(t, dir, "utf8bom.csv", body, bom)
	rows, r := readAllRecords(t, p, CSVOptions{})
	defer r.Close()
	if r.Encoding() != "utf-8-bom" {
		t.Fatalf("encoding got=%s want=utf-8-bom", r.Encoding())
	}
	if rows[0][0] != "姓名" {
		t.Fatalf("BOM 没剥干净，首字段=%q", rows[0][0])
	}
	if rows[1][1] != "六年级" {
		t.Fatalf("中文解析错误 rows=%v", rows)
	}
}

// T3：GBK 中文，由 chardet 自动识别。
func TestCSVReader_GBK_AutoDetect(t *testing.T) {
	dir := t.TempDir()
	// 用足够多的中文让 chardet 置信度 ≥ 50
	src := "姓名,年级,班级\n张三,六年级,一班\n李四,七年级,二班\n王五,八年级,三班\n赵六,九年级,四班\n钱七,五年级,五班\n孙八,四年级,六班\n"
	gbkBytes := encode(t, src, simplifiedchinese.GBK.NewEncoder())
	p := writeEncoded(t, dir, "gbk.csv", gbkBytes, nil)
	rows, r := readAllRecords(t, p, CSVOptions{})
	defer r.Close()
	if r.Encoding() != "gbk" && r.Encoding() != "gb18030" {
		t.Fatalf("encoding got=%s want=gbk/gb18030", r.Encoding())
	}
	if rows[1][0] != "张三" || rows[1][1] != "六年级" {
		t.Fatalf("GBK 解码错误 rows=%v", rows)
	}
}

// T4：GB18030 含罕见字（𠮷 = U+20BB7，需 4-byte UTF-8，GB18030 4-byte 编码）。
func TestCSVReader_GB18030_RareChar(t *testing.T) {
	dir := t.TempDir()
	src := "字符,描述\n𠮷,罕见字\n"
	enc := encode(t, src, simplifiedchinese.GB18030.NewEncoder())
	p := writeEncoded(t, dir, "gb18030.csv", enc, nil)
	// 显式指定 gb18030（chardet 在小样本上倾向报 gbk，gbk 不能解 4-byte）
	rows, r := readAllRecords(t, p, CSVOptions{Encoding: "gb18030"})
	defer r.Close()
	if r.Encoding() != "gb18030" {
		t.Fatalf("encoding got=%s want=gb18030", r.Encoding())
	}
	if rows[1][0] != "𠮷" {
		t.Fatalf("GB18030 罕见字解码错误 rows=%v", rows)
	}
}

// T5：UTF-16 LE BOM。
func TestCSVReader_UTF16LE_BOM(t *testing.T) {
	dir := t.TempDir()
	src := "name,city\nalice,北京\nbob,上海\n"
	enc := encode(t, src, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder())
	p := writeEncoded(t, dir, "utf16le.csv", enc, nil)
	rows, r := readAllRecords(t, p, CSVOptions{})
	defer r.Close()
	if r.Encoding() != "utf-16le" {
		t.Fatalf("encoding got=%s want=utf-16le", r.Encoding())
	}
	if rows[1][1] != "北京" {
		t.Fatalf("UTF-16LE 解码错误 rows=%v", rows)
	}
}

// T6：字段内带 \n（必须 csv.Reader 处理，不能按 \n split）。
func TestCSVReader_FieldNewline(t *testing.T) {
	dir := t.TempDir()
	body := "name,note\nalice,\"line1\nline2\"\nbob,ok\n"
	p := writeEncoded(t, dir, "newline.csv", []byte(body), nil)
	rows, r := readAllRecords(t, p, CSVOptions{})
	defer r.Close()
	if len(rows) != 3 {
		t.Fatalf("行数错 rows=%d", len(rows))
	}
	if rows[1][1] != "line1\nline2" {
		t.Fatalf("字段内换行解析错误 rows[1]=%v", rows[1])
	}
}

// T7：字段内逗号、双引号（LazyQuotes 容忍）。
func TestCSVReader_QuotesAndComma(t *testing.T) {
	dir := t.TempDir()
	body := "name,note\nalice,\"a,b,c\"\nbob,\"say \"\"hi\"\"\"\n"
	p := writeEncoded(t, dir, "quote.csv", []byte(body), nil)
	rows, r := readAllRecords(t, p, CSVOptions{})
	defer r.Close()
	if rows[1][1] != "a,b,c" {
		t.Fatalf("字段内逗号解析错误 rows[1]=%v", rows[1])
	}
	if rows[2][1] != `say "hi"` {
		t.Fatalf("字段内双引号解析错误 rows[2]=%v", rows[2])
	}
}

// T8：每行字段数不一致（FieldsPerRecord = -1 容忍）。
func TestCSVReader_VariableFieldCount(t *testing.T) {
	dir := t.TempDir()
	body := "a,b,c\n1,2\n3,4,5,6\n7,8,9\n"
	p := writeEncoded(t, dir, "vary.csv", []byte(body), nil)
	rows, r := readAllRecords(t, p, CSVOptions{})
	defer r.Close()
	if len(rows) != 4 {
		t.Fatalf("行数错 rows=%d", len(rows))
	}
	if len(rows[1]) != 2 || len(rows[2]) != 4 {
		t.Fatalf("字段数应被保留 rows=%v", rows)
	}
}

// T9：用户显式指定 GBK 但文件实为 UTF-8 → 按用户选择走 GBK 解码（结果会乱码，但不报错）。
// 验证：override 确实生效（Encoding 名变了），不验证内容（乱码本身就是用户责任）。
func TestCSVReader_ManualOverride_Wrong(t *testing.T) {
	dir := t.TempDir()
	// UTF-8 文件，但显式指定 gbk
	p := writeEncoded(t, dir, "utf8_overridden.csv", []byte("name,city\nalice,北京\n"), nil)
	r, err := OpenCSV(p, CSVOptions{Encoding: "gbk"})
	if err != nil {
		t.Fatalf("OpenCSV: %v", err)
	}
	defer r.Close()
	if r.Encoding() != "gbk" {
		t.Fatalf("override 没生效，Encoding=%s", r.Encoding())
	}
	// 不验证内容，只确认能不报错地迭代完
	for r.Next() {
	}
	if err := r.Err(); err != nil {
		t.Fatalf("迭代不应报错: %v", err)
	}
}

// 分隔符自定义：分号 / Tab。
func TestCSVReader_DelimiterSemicolon(t *testing.T) {
	dir := t.TempDir()
	p := writeEncoded(t, dir, "semi.csv", []byte("a;b;c\n1;2;3\n"), nil)
	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: ";"})
	defer r.Close()
	if len(rows[0]) != 3 || rows[0][1] != "b" {
		t.Fatalf("分号分隔失败 rows=%v", rows)
	}
}

func TestCSVReader_DelimiterTab(t *testing.T) {
	dir := t.TempDir()
	p := writeEncoded(t, dir, "tab.csv", []byte("a\tb\tc\n1\t2\t3\n"), nil)
	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: "\t"})
	defer r.Close()
	if len(rows[0]) != 3 || rows[1][2] != "3" {
		t.Fatalf("Tab 分隔失败 rows=%v", rows)
	}
}

// chardet 也能识别繁体 Big5（覆盖 traditionalchinese 解码路径）。
func TestCSVReader_Big5_Manual(t *testing.T) {
	dir := t.TempDir()
	src := "姓名,城市\n王小明,台北\n李大華,台中\n"
	enc := encode(t, src, traditionalchinese.Big5.NewEncoder())
	p := writeEncoded(t, dir, "big5.csv", enc, nil)
	// 手动指定 big5（chardet 可能误判为 gbk，所以用 override 验证 big5 路径）
	rows, r := readAllRecords(t, p, CSVOptions{Encoding: "big5"})
	defer r.Close()
	if r.Encoding() != "big5" {
		t.Fatalf("encoding got=%s want=big5", r.Encoding())
	}
	if rows[1][0] != "王小明" {
		t.Fatalf("Big5 解码错误 rows=%v", rows)
	}
}
