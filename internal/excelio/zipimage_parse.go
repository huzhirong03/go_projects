package excelio

// zipimage_parse.go：drawing.xml 的 anchor 解析。
// 单独一个文件是为了让 zipimage.go 的流程更清晰。
//
// drawing.xml 里 OOXML 有 3 种 anchor：
//   - twoCellAnchor：有 <xdr:from> 和 <xdr:to>，图片跟单元格伸缩；
//   - oneCellAnchor：有 <xdr:from> 和 <xdr:ext cx=.. cy=..>，只定位不跟伸缩；
//   - absoluteAnchor：用绝对坐标 <xdr:pos>+<xdr:ext>，不锚 cell，我们按 row=0 记一笔。
//
// 我们关心的最小信息：from.row/from.col + 渲染尺寸 cx/cy + blip r:embed（rId）+
// 可选的 AltText/Name/LockAspect。不关心 Chart/Shape，遇到 <xdr:pic> 才记录。

import (
	"encoding/xml"
	"strings"

	"excel-master/internal/core"
)

// parseDrawingAnchors 扫一遍 drawing.xml，返回所有图片 anchor。
// 用 xml.Decoder 流式解析，避免一次把 DOM 全部展开（drawing.xml 通常也不大，
// 但我们追求最低开销）。
func parseDrawingAnchors(data []byte) ([]parsedAnchor, error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	var out []parsedAnchor
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "twoCellAnchor":
			a, ok, err := decodeTwoCellAnchor(dec, se)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, a)
			}
		case "oneCellAnchor":
			a, ok, err := decodeOneCellAnchor(dec, se)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, a)
			}
		case "absoluteAnchor":
			// 跳过（不按行绑定）
			if err := dec.Skip(); err != nil {
				return nil, core.Wrap("DRAWING_PARSE_FAILED", "跳过 absoluteAnchor 失败", err)
			}
		}
	}
	return out, nil
}

// twoCellAnchorXML 是 twoCellAnchor 的 xml 结构体定义，仅提取我们需要的字段。
// 名字空间处理：XML 里 xdr/a/r 都是 namespace 前缀，encoding/xml 靠 struct tag 的 Space 字段匹配。
// 我们这里用 Local-name only（不绑定 namespace），encoding/xml 会忽略 namespace 前缀。
type twoCellAnchorXML struct {
	EditAs string      `xml:"editAs,attr"`
	From   *anchorCell `xml:"from"`
	To     *anchorCell `xml:"to"`
	Pic    *picXML     `xml:"pic"`
}

type oneCellAnchorXML struct {
	From *anchorCell `xml:"from"`
	Ext  *extXML     `xml:"ext"`
	Pic  *picXML     `xml:"pic"`
}

// anchorCell 就是 <xdr:from> / <xdr:to>，row/col 是 0-based。
type anchorCell struct {
	Col    int `xml:"col"`
	ColOff int `xml:"colOff"`
	Row    int `xml:"row"`
	RowOff int `xml:"rowOff"`
}

// extXML 是 oneCellAnchor 直接挂的 <xdr:ext cx=.. cy=..>。
type extXML struct {
	CX int64 `xml:"cx,attr"`
	CY int64 `xml:"cy,attr"`
}

// picXML 是 <xdr:pic>，只挖我们要的字段。
type picXML struct {
	NvPicPr struct {
		CNvPr struct {
			Name  string `xml:"name,attr"`
			Descr string `xml:"descr,attr"`
		} `xml:"cNvPr"`
		CNvPicPr struct {
			PicLocks struct {
				NoChangeAspect string `xml:"noChangeAspect,attr"`
			} `xml:"picLocks"`
		} `xml:"cNvPicPr"`
	} `xml:"nvPicPr"`
	BlipFill struct {
		Blip struct {
			// <a:blip r:embed="rIdN"/>，xml 库会用 Local-name "embed" 匹配任何 namespace 的 embed 属性。
			Embed string `xml:"embed,attr"`
		} `xml:"blip"`
	} `xml:"blipFill"`
	SpPr struct {
		Xfrm struct {
			Ext extXML `xml:"ext"`
		} `xml:"xfrm"`
	} `xml:"spPr"`
}

// decodeTwoCellAnchor 把游标停在 <twoCellAnchor> 开头处，用 DecodeElement 一口气解完整块。
// 返回 (anchor, isPic, error)：isPic=false 表示这个 anchor 不是图片（如 chart/shape），忽略。
func decodeTwoCellAnchor(dec *xml.Decoder, se xml.StartElement) (parsedAnchor, bool, error) {
	var v twoCellAnchorXML
	if err := dec.DecodeElement(&v, &se); err != nil {
		return parsedAnchor{}, false, core.Wrap("DRAWING_PARSE_FAILED", "解析 twoCellAnchor 失败", err)
	}
	if v.Pic == nil || v.From == nil || v.Pic.BlipFill.Blip.Embed == "" {
		return parsedAnchor{}, false, nil
	}
	a := parsedAnchor{
		row:        v.From.Row + 1,
		col:        v.From.Col + 1,
		rId:        v.Pic.BlipFill.Blip.Embed,
		lockAspect: parseXMLBool(v.Pic.NvPicPr.CNvPicPr.PicLocks.NoChangeAspect),
		altText:    v.Pic.NvPicPr.CNvPr.Descr,
		name:       v.Pic.NvPicPr.CNvPr.Name,

		fromColOff: int64(v.From.ColOff),
		fromRowOff: int64(v.From.RowOff),
	}
	if v.To != nil {
		a.toCol = v.To.Col + 1
		a.toRow = v.To.Row + 1
		a.toColOff = int64(v.To.ColOff)
		a.toRowOff = int64(v.To.RowOff)
	}
	// 渲染尺寸：pic 内部 spPr/xfrm/ext 才是真实渲染 cx/cy（跟 excelize 行为一致）
	a.cxEMU = v.Pic.SpPr.Xfrm.Ext.CX
	a.cyEMU = v.Pic.SpPr.Xfrm.Ext.CY
	return a, true, nil
}

// decodeOneCellAnchor：只定位不伸缩。positioning = "oneCell"。
func decodeOneCellAnchor(dec *xml.Decoder, se xml.StartElement) (parsedAnchor, bool, error) {
	var v oneCellAnchorXML
	if err := dec.DecodeElement(&v, &se); err != nil {
		return parsedAnchor{}, false, core.Wrap("DRAWING_PARSE_FAILED", "解析 oneCellAnchor 失败", err)
	}
	if v.Pic == nil || v.From == nil || v.Pic.BlipFill.Blip.Embed == "" {
		return parsedAnchor{}, false, nil
	}
	a := parsedAnchor{
		row:         v.From.Row + 1,
		col:         v.From.Col + 1,
		rId:         v.Pic.BlipFill.Blip.Embed,
		positioning: "oneCell",
		lockAspect:  parseXMLBool(v.Pic.NvPicPr.CNvPicPr.PicLocks.NoChangeAspect),
		altText:     v.Pic.NvPicPr.CNvPr.Descr,
		name:        v.Pic.NvPicPr.CNvPr.Name,
	}
	// 先看 spPr/xfrm/ext（优先），否则回落到 anchor 自带的 ext
	if v.Pic.SpPr.Xfrm.Ext.CX > 0 && v.Pic.SpPr.Xfrm.Ext.CY > 0 {
		a.cxEMU = v.Pic.SpPr.Xfrm.Ext.CX
		a.cyEMU = v.Pic.SpPr.Xfrm.Ext.CY
	} else if v.Ext != nil {
		a.cxEMU = v.Ext.CX
		a.cyEMU = v.Ext.CY
	}
	return a, true, nil
}

// parseXMLBool 把 OOXML 里的 "1"/"true"/"0"/"false" 转为 bool。
func parseXMLBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true"
}
