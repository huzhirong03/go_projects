package excelio

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// 自动嗅探分隔符的测试集。
//
// 覆盖路径：
//   - opts.Delimiter = "" / "auto" → 触发嗅探
//   - 4 种主流分隔符：',' ';' '\t' '|'
//   - UTF-8 / UTF-8 BOM / GBK 编码下嗅探都正确（嗅探在解码层之后）
//   - 显式指定不嗅探（即使指定了"错的"分隔符，也按用户给的来）
//   - quote 包裹的字段含分隔符不会干扰（csv.Reader 处理 quote）
//   - 单字段文件（无分隔符）回退到 ','

func TestSniffDelimiter_UTF8_Comma(t *testing.T) {
	dir := t.TempDir()
	body := "name,age,city\nAlice,30,Beijing\nBob,25,Shanghai\nCarol,28,Shenzhen\n"
	p := filepath.Join(dir, "comma.csv")
	if err := writeBytes(t, p, []byte(body)); err != nil {
		t.Fatal(err)
	}

	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != ',' {
		t.Errorf("Delimiter()=%q, want ','", r.Delimiter())
	}
	if len(rows) != 4 || rows[1][1] != "30" {
		t.Errorf("rows=%v", rows)
	}
}

func TestSniffDelimiter_UTF8_Semicolon(t *testing.T) {
	dir := t.TempDir()
	body := "name;age;city\nAlice;30;Beijing\nBob;25;Shanghai\nCarol;28;Shenzhen\n"
	p := filepath.Join(dir, "semi.csv")
	if err := writeBytes(t, p, []byte(body)); err != nil {
		t.Fatal(err)
	}

	rows, r := readAllRecords(t, p, CSVOptions{}) // 空 = auto
	defer r.Close()
	if r.Delimiter() != ';' {
		t.Errorf("Delimiter()=%q, want ';'", r.Delimiter())
	}
	if len(rows) != 4 || rows[1][1] != "30" {
		t.Errorf("rows=%v", rows)
	}
}

func TestSniffDelimiter_UTF8_Tab(t *testing.T) {
	dir := t.TempDir()
	body := "name\tage\tcity\nAlice\t30\tBeijing\nBob\t25\tShanghai\nCarol\t28\tShenzhen\n"
	p := filepath.Join(dir, "tab.csv")
	if err := writeBytes(t, p, []byte(body)); err != nil {
		t.Fatal(err)
	}

	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != '\t' {
		t.Errorf("Delimiter()=%q, want '\\t'", r.Delimiter())
	}
	if len(rows) != 4 || rows[1][1] != "30" {
		t.Errorf("rows=%v", rows)
	}
}

func TestSniffDelimiter_UTF8_Pipe(t *testing.T) {
	dir := t.TempDir()
	body := "name|age|city\nAlice|30|Beijing\nBob|25|Shanghai\nCarol|28|Shenzhen\n"
	p := filepath.Join(dir, "pipe.csv")
	if err := writeBytes(t, p, []byte(body)); err != nil {
		t.Fatal(err)
	}

	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != '|' {
		t.Errorf("Delimiter()=%q, want '|'", r.Delimiter())
	}
	if len(rows) != 4 || rows[1][1] != "30" {
		t.Errorf("rows=%v", rows)
	}
}

// GBK 编码 + Tab 分隔：嗅探必须发生在解码后；如果错误地在原始字节上嗅探，
// GBK 中文字节里可能含 0x09（Tab）的字节使分布混乱导致选错。
func TestSniffDelimiter_GBK_Tab(t *testing.T) {
	dir := t.TempDir()
	src := "姓名\t年龄\t城市\n张三\t30\t北京\n李四\t25\t上海\n王五\t28\t深圳\n"
	gbkBytes := encode(t, src, simplifiedchinese.GBK.NewEncoder())
	p := writeEncoded(t, dir, "gbk_tab.csv", gbkBytes, nil)

	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != '\t' {
		t.Errorf("Delimiter()=%q, want '\\t'", r.Delimiter())
	}
	if r.Encoding() != "gbk" && r.Encoding() != "gb18030" {
		t.Errorf("Encoding()=%q, want gbk/gb18030", r.Encoding())
	}
	if len(rows) != 4 || rows[1][0] != "张三" || rows[1][2] != "北京" {
		t.Errorf("rows=%v", rows)
	}
}

// UTF-8 BOM + 分号：嗅探在 BOM 已剥离之后进行，不该被 BOM 字节干扰。
func TestSniffDelimiter_UTF8BOM_Semicolon(t *testing.T) {
	dir := t.TempDir()
	src := "name;age;city\nAlice;30;Beijing\nBob;25;Shanghai\nCarol;28;Shenzhen\n"
	bom := []byte{0xEF, 0xBB, 0xBF}
	p := writeEncoded(t, dir, "bom_semi.csv", []byte(src), bom)

	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != ';' {
		t.Errorf("Delimiter()=%q, want ';'", r.Delimiter())
	}
	if r.Encoding() != "utf-8-bom" {
		t.Errorf("Encoding()=%q, want utf-8-bom", r.Encoding())
	}
	// BOM 必须已剥离：第一个字段不该有 \uFEFF
	if len(rows) > 0 && len(rows[0]) > 0 && rows[0][0] != "name" {
		t.Errorf("BOM 未剥离: rows[0][0]=%q", rows[0][0])
	}
}

// 显式指定优先级：用户选了 ","，即使文件实际是分号分隔，也按用户的来（不嗅探）。
// 这样保留用户的"我比软件聪明"路径。
func TestSniffDelimiter_ExplicitOverridesAuto(t *testing.T) {
	dir := t.TempDir()
	body := "name;age;city\nAlice;30;Beijing\n"
	p := filepath.Join(dir, "semi.csv")
	if err := writeBytes(t, p, []byte(body)); err != nil {
		t.Fatal(err)
	}

	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: ","}) // 用户硬要用逗号
	defer r.Close()
	if r.Delimiter() != ',' {
		t.Errorf("Delimiter()=%q, want ',' (用户显式指定)", r.Delimiter())
	}
	// 用逗号切分号文件 → 每行只有 1 个字段（整行）
	if len(rows) != 2 {
		t.Errorf("rows count=%d", len(rows))
	}
	if len(rows[0]) != 1 {
		t.Errorf("用户显式 ',' 切分号文件应该是 1 字段/行: %v", rows[0])
	}
}

// quote 包裹的字段含分隔符：不能让 quote 内的字符干扰嗅探统计。
// 此文件实际是分号分隔，但每行的某个字段（quote 包裹）里有 5 个逗号。
// 期望嗅探依然选 ';'（因为按 ';' 切字段数稳定 = 3，按 ',' 切字段数 = 6 但每行不一致或更多）。
func TestSniffDelimiter_QuotedFieldDoesNotConfuse(t *testing.T) {
	dir := t.TempDir()
	body := "name;tags;score\n" +
		"Alice;\"red,green,blue,yellow,black\";100\n" +
		"Bob;\"a,b,c,d,e\";90\n" +
		"Carol;\"x,y,z,w,v\";85\n"
	p := filepath.Join(dir, "quoted.csv")
	if err := writeBytes(t, p, []byte(body)); err != nil {
		t.Fatal(err)
	}

	_, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != ';' {
		t.Errorf("Delimiter()=%q, want ';' (quote 内的逗号不该干扰嗅探)", r.Delimiter())
	}
}

// 没有任何分隔符的文件（每行一个字段）：嗅探应失败，回退到 ','。
func TestSniffDelimiter_NoDelimiter_FallbackComma(t *testing.T) {
	dir := t.TempDir()
	body := "header\nrow1\nrow2\nrow3\n"
	p := filepath.Join(dir, "single.csv")
	if err := writeBytes(t, p, []byte(body)); err != nil {
		t.Fatal(err)
	}

	_, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != ',' {
		t.Errorf("Delimiter()=%q, want ',' (无分隔符 fallback)", r.Delimiter())
	}
}

// 极小文件（< 4 字节）：sniffDelimiter 应直接返回 false，调用方 fallback ','。
func TestSniffDelimiter_TinyFile_FallbackComma(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tiny.csv")
	if err := writeBytes(t, p, []byte("a")); err != nil {
		t.Fatal(err)
	}

	_, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != ',' {
		t.Errorf("Delimiter()=%q, want ','", r.Delimiter())
	}
}

// 验证 UTF-16LE 编码下嗅探也走解码后的路径。
// UTF-16 中 ',' 编码为 0x2C 0x00，如果错误地在原始字节嗅探会找到一堆 \x00。
func TestSniffDelimiter_UTF16LE_Comma(t *testing.T) {
	dir := t.TempDir()
	src := "name,age,city\nAlice,30,Beijing\nBob,25,Shanghai\nCarol,28,Shenzhen\n"
	utf16Enc := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder()
	bin := encode(t, src, utf16Enc)
	p := writeEncoded(t, dir, "utf16le.csv", bin, nil)

	rows, r := readAllRecords(t, p, CSVOptions{Delimiter: "auto"})
	defer r.Close()
	if r.Delimiter() != ',' {
		t.Errorf("Delimiter()=%q, want ',' (UTF-16LE 解码后嗅探)", r.Delimiter())
	}
	if len(rows) != 4 || rows[1][1] != "30" {
		t.Errorf("rows=%v", rows)
	}
}

// 工具：把 bytes 写到指定路径（测试辅助）。
func writeBytes(t *testing.T, path string, data []byte) error {
	t.Helper()
	return os.WriteFile(path, data, 0o644)
}
