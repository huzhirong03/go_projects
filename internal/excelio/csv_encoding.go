package excelio

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// CSVDetectHeadSize 嗅探编码时读取文件头部的字节数。
// 8KB 是 chardet 文档推荐的最小可信样本，太小（< 1KB）容易把 GBK 误判为 ISO-8859。
const CSVDetectHeadSize = 8192

// EncodingDetect 编码识别结果。
type EncodingDetect struct {
	Enc     encoding.Encoding // 解码用的 Encoding；UTF-8 为 unicode.UTF8 占位（实际无需 transform）
	Name    string            // 规范化编码名（utf-8 / utf-8-bom / gbk / gb18030 / big5 / utf-16le / utf-16be）
	SkipBOM bool              // 是否需要在解码后跳过 BOM（UTF-8 BOM 才需要）
}

// DetectCSVEncoding 三级识别 CSV 文件编码：
//
//	1) BOM 优先（最快最准）
//	2) 用户 override 显式指定
//	3) chardet 嗅探头 8KB（置信度 ≥ 50 才采用）
//	4) 兜底：UTF-8 valid 则 UTF-8，否则 GBK（中文 Windows 最常见）
//
// override 是用户在 UI 显式指定的编码名（"" / "auto" 视为不指定）。
func DetectCSVEncoding(path, override string) (EncodingDetect, error) {
	f, err := os.Open(path)
	if err != nil {
		return EncodingDetect{}, err
	}
	defer f.Close()

	head := make([]byte, CSVDetectHeadSize)
	n, _ := io.ReadFull(f, head)
	head = head[:n]

	// 1. BOM
	if e, ok := detectBOM(head); ok {
		return e, nil
	}

	// 2. 用户 override
	if e, ok := encodingByName(override); ok {
		return e, nil
	}

	// 3. chardet 嗅探
	d := chardet.NewTextDetector()
	if r, err := d.DetectBest(head); err == nil && r.Confidence >= 50 {
		if e, ok := encodingByName(r.Charset); ok {
			return e, nil
		}
	}

	// 4. 兜底
	if utf8.Valid(head) {
		return EncodingDetect{Enc: unicode.UTF8, Name: "utf-8"}, nil
	}
	return EncodingDetect{Enc: simplifiedchinese.GBK, Name: "gbk"}, nil
}

// detectBOM 仅根据头几个字节认 BOM。返回是否命中。
func detectBOM(head []byte) (EncodingDetect, bool) {
	switch {
	case len(head) >= 3 && head[0] == 0xEF && head[1] == 0xBB && head[2] == 0xBF:
		return EncodingDetect{Enc: unicode.UTF8, Name: "utf-8-bom", SkipBOM: true}, true
	case len(head) >= 2 && head[0] == 0xFF && head[1] == 0xFE:
		return EncodingDetect{
			Enc:  unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM),
			Name: "utf-16le",
		}, true
	case len(head) >= 2 && head[0] == 0xFE && head[1] == 0xFF:
		return EncodingDetect{
			Enc:  unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM),
			Name: "utf-16be",
		}, true
	}
	return EncodingDetect{}, false
}

// encodingByName 把名字（来自 chardet 或用户输入）映射到 encoding.Encoding。
// 接受常见别名：utf8 / utf-8 / utf_8、gbk、gb-18030 / gb18030、big5 / big-5 等。
func encodingByName(name string) (EncodingDetect, bool) {
	if name == "" {
		return EncodingDetect{}, false
	}
	key := normalizeEncodingName(name)
	switch key {
	case "auto":
		return EncodingDetect{}, false
	case "utf-8", "utf8":
		return EncodingDetect{Enc: unicode.UTF8, Name: "utf-8"}, true
	case "utf-8-bom", "utf8bom":
		return EncodingDetect{Enc: unicode.UTF8, Name: "utf-8-bom", SkipBOM: true}, true
	case "utf-16le", "utf16le":
		return EncodingDetect{
			Enc:  unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM),
			Name: "utf-16le",
		}, true
	case "utf-16be", "utf16be":
		return EncodingDetect{
			Enc:  unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM),
			Name: "utf-16be",
		}, true
	case "gbk", "gb2312", "gb-2312":
		// gb2312 是 gbk 的子集，统一按 gbk 解（兼容更宽）
		return EncodingDetect{Enc: simplifiedchinese.GBK, Name: "gbk"}, true
	case "gb18030", "gb-18030":
		return EncodingDetect{Enc: simplifiedchinese.GB18030, Name: "gb18030"}, true
	case "big5", "big-5":
		return EncodingDetect{Enc: traditionalchinese.Big5, Name: "big5"}, true
	}
	return EncodingDetect{}, false
}

func normalizeEncodingName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
