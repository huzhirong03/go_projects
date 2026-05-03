package excelio

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"excel-master/internal/core"
)

// absorbSecondary 处理一个 secondary 源：
//   - 解析 secondary 的 sharedStrings + sheet xml，提取 KeepRows 的每行
//   - 把每行重写成 inline string + 模板列样式，分配新行号，加到 state.appendRows
//   - 解析 secondary 的 drawing + rels + media，把 KeepRows 命中的图片：
//     · image 字节拷到 state.newMedia（避免文件名冲突）
//     · drawing rels 追加 + drawing anchor 追加（用新 rId / 新行号）
func (st *mergeState) absorbSecondary(s MergeSource) error {
	zr, err := zip.OpenReader(s.SrcPath)
	if err != nil {
		return core.Wrap("MERGE_OPEN_SECONDARY_FAILED", "打开 secondary 失败: "+s.SrcPath, err)
	}
	defer zr.Close()

	// 1) 找 secondary 的 sheet xml + drawing
	layout, err := readXlsxLayout(zr, []string{s.SheetName})
	if err != nil {
		return err
	}
	ent := layout.sheetEntries[s.SheetName]
	if ent == nil {
		return core.New("MERGE_SHEET_NOT_FOUND", "secondary 里找不到 sheet: "+s.SheetName)
	}

	// 2) 读 sharedStrings（解引用 t="s" 的 cell）
	sst, _ := readSharedStrings(zr)

	// 3) 读 secondary sheet xml，对每个 KeepRow 抽出来转成 inline-string row
	sheetData, err := readEntryByName(zr, ent.sheetXMLPath)
	if err != nil {
		return err
	}
	keepSet := map[int]bool{}
	for _, r := range s.KeepRows {
		keepSet[r] = true
	}
	rowMap := map[int]int{} // secondary 源行号 -> 输出新行号
	for _, srcRow := range s.KeepRows {
		rowBytes, err := extractRow(sheetData, srcRow)
		if err != nil {
			// 跳过不存在的行（容错）
			continue
		}
		newRow := st.nextRow
		st.nextRow++
		converted := st.convertSecondaryRow(rowBytes, srcRow, newRow, sst)
		st.appendRows = append(st.appendRows, converted)
		rowMap[srcRow] = newRow
	}

	// 4) 处理图片（仅当 secondary 有 drawing 且我们有 KeepRows 命中）
	if ent.drawingXMLPath == "" || len(rowMap) == 0 {
		return nil
	}
	if err := st.absorbSecondaryImages(zr, ent.drawingXMLPath, rowMap); err != nil {
		return err
	}
	return nil
}

// convertSecondaryRow 把 secondary 的 row 字节重写成"inline string + 模板列样式"的 row。
// 输入 row 形如 <row r="54" ...><c r="A54" s="3"><v>10</v></c>...</row>。
//
// 处理规则：
//   - 遍历每个 <c>，按 r="A54" 提取 col 索引
//   - 若 col >= st.headerCols：丢弃该 cell（secondary 列多于模板）
//   - 计算新 cellRef（如 "A78"），style ID 用 st.colStyles[col]（缺失则不写 s）
//   - 若原 cell t="s"：把 <v>idx</v> 解出字符串，用 inlineStr 形式写入
//   - 若原 cell t="str" 或 公式（含 <f>）：用缓存值（<v>）作为文本，没 <v> 就 skip
//   - 若原 cell 是 boolean / date / 普通数字（无 t 或 t="n"）：保留为 <v>...</v>
//   - 其他类型：转 inline string 处理
func (st *mergeState) convertSecondaryRow(rowBytes []byte, srcRow, newRow int, sst []string) []byte {
	// 解析所有 <c>
	cells := parseRowCells(rowBytes, srcRow)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<row r="%d"`, newRow)
	// 保留行高属性（如 ht / customHeight / ht="58"）—— 从原 row 标签头提取
	hdrRe := regexp.MustCompile(`<row r="\d+"([^>]*)>`)
	if m := hdrRe.FindSubmatch(rowBytes); len(m) >= 2 {
		extra := string(m[1])
		// 去掉可能存在的 spans（不重要、xlsx 自适应）
		extra = regexp.MustCompile(`\s*spans="[^"]*"`).ReplaceAllString(extra, "")
		buf.WriteString(extra)
	}
	buf.WriteString(`>`)

	for _, c := range cells {
		col := colFromCellRef(c.ref)
		if col < 0 || col >= st.headerCols {
			continue
		}
		newRef := colLetters(col) + strconv.Itoa(newRow)

		// 决定 style ID：用模板的列样式
		styleAttr := ""
		if sid := st.colStyles[col]; sid != "" {
			styleAttr = ` s="` + sid + `"`
		}

		// 公式 cell：保留公式（同行偏移）+ 缓存值如果有。
		// 原公式里的 `<letters>srcRow` 等于在本行引用同行其他列，合并后要偏移到 newRow。
		// 跳行引用（如 SUM(A1:A100)）不动，Excel 重算时可能出 #REF!。
		if c.hasFmla {
			newFormula := offsetFormula(c.formula, srcRow, newRow)
			tAttr := ""
			if c.tAttr != "" && c.tAttr != "n" {
				tAttr = ` t="` + c.tAttr + `"`
			}
			if c.value != "" {
				fmt.Fprintf(&buf, `<c r="%s"%s%s><f>%s</f><v>%s</v></c>`,
					newRef, styleAttr, tAttr, xmlEscape(newFormula), xmlEscape(c.value))
			} else {
				fmt.Fprintf(&buf, `<c r="%s"%s%s><f>%s</f></c>`,
					newRef, styleAttr, tAttr, xmlEscape(newFormula))
			}
			continue
		}

		// 决定值：sharedString / inline / 数字 / 公式缓存值
		text, isNumeric, isEmpty := decodeSecondaryCellValue(c, sst)
		if isEmpty {
			fmt.Fprintf(&buf, `<c r="%s"%s/>`, newRef, styleAttr)
			continue
		}
		if isNumeric {
			fmt.Fprintf(&buf, `<c r="%s"%s><v>%s</v></c>`, newRef, styleAttr, text)
			continue
		}
		// inline string
		fmt.Fprintf(&buf, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
			newRef, styleAttr, xmlEscape(text))
	}

	buf.WriteString(`</row>`)
	return buf.Bytes()
}

// rawCell 一个 <c> 节点的原始字段。
type rawCell struct {
	ref     string // 如 "A54"
	tAttr   string // t 属性值（"s" / "str" / "inlineStr" / "n" / "" 等）
	value   string // <v> 的内容（如果有）
	inline  string // <is><t>...</t></is> 的纯文本（如果是 inlineStr）
	formula string // <f>...</f> 的内容（如果是公式 cell）
	hasFmla bool   // 是否含有 <f> 元素
}

// parseRowCells 用 xml.Decoder 解析 row 内所有 <c>。失败返回空切片。
func parseRowCells(rowBytes []byte, srcRow int) []rawCell {
	var out []rawCell
	dec := xml.NewDecoder(bytes.NewReader(rowBytes))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "c" {
			continue
		}
		var c rawCell
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "r":
				c.ref = a.Value
			case "t":
				c.tAttr = a.Value
			}
		}
		// 解析子元素
		for {
			tok2, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			if end, ok := tok2.(xml.EndElement); ok && end.Name.Local == "c" {
				break
			}
			if se2, ok := tok2.(xml.StartElement); ok {
				switch se2.Name.Local {
				case "v":
					var v string
					_ = dec.DecodeElement(&v, &se2)
					c.value = v
				case "is":
					var is struct {
						T string `xml:"t"`
					}
					_ = dec.DecodeElement(&is, &se2)
					c.inline = is.T
				case "f":
					c.hasFmla = true
					var f string
					_ = dec.DecodeElement(&f, &se2)
					c.formula = f
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// decodeSecondaryCellValue 根据 cell 类型返回应当写入新 cell 的值。
// 返回 (text, isNumeric, isEmpty)：
//   - isEmpty=true 时直接写空 cell
//   - isNumeric=true 时直接写 <v>text</v>
//   - 否则当作 inline string 处理（text 已经是要嵌入的明文）
func decodeSecondaryCellValue(c rawCell, sst []string) (string, bool, bool) {
	switch c.tAttr {
	case "s":
		// shared string 引用
		idx, err := strconv.Atoi(c.value)
		if err != nil || idx < 0 || idx >= len(sst) {
			return "", false, true
		}
		return sst[idx], false, false
	case "inlineStr":
		if c.inline == "" {
			return "", false, true
		}
		return c.inline, false, false
	case "str":
		// 公式直接量字符串：用 <v> 作为文本
		if c.value == "" {
			return "", false, true
		}
		return c.value, false, false
	case "b", "e", "d":
		// 布尔/错误/日期：当作普通文本嵌入（保守做法）
		if c.value == "" {
			return "", false, true
		}
		return c.value, false, false
	default:
		// 默认为数字（无 t 属性 或 t="n"）
		if c.value == "" {
			return "", false, true
		}
		return c.value, true, false
	}
}

// offsetFormula 把 formula 中同一源行的引用偏移到新行。
// 例如 srcRow=54,newRow=3 时：G54*J54 -> G3*J3。
// 只替换 `<letters><srcRow>` 且右侧不是字母/数字的片段，避免误伤 G540。
func offsetFormula(formula string, srcRow, newRow int) string {
	srcStr := strconv.Itoa(srcRow)
	newStr := strconv.Itoa(newRow)
	re := regexp.MustCompile(`([A-Z]+)` + srcStr + `([^0-9A-Za-z]|$)`)
	return re.ReplaceAllString(formula, `${1}`+newStr+`${2}`)
}

// xmlEscape XML 文本转义（最小集合）。
func xmlEscape(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// readSharedStrings 读 xl/sharedStrings.xml 返回字符串数组（按 si 顺序）。
// 没有则返回 nil, nil。
func readSharedStrings(zr *zip.ReadCloser) ([]string, error) {
	data, err := readEntryByNameOptional(zr, "xl/sharedStrings.xml")
	if err != nil || data == nil {
		return nil, err
	}
	type tNode struct {
		Text string `xml:",chardata"`
	}
	type rNode struct {
		T tNode `xml:"t"`
	}
	type siNode struct {
		T tNode   `xml:"t"`
		R []rNode `xml:"r"`
	}
	type sst struct {
		SI []siNode `xml:"si"`
	}
	var s sst
	if err := xml.Unmarshal(data, &s); err != nil {
		return nil, core.Wrap("SST_PARSE_FAILED", "解析 sharedStrings.xml 失败", err)
	}
	out := make([]string, 0, len(s.SI))
	for _, si := range s.SI {
		if len(si.R) > 0 {
			var b strings.Builder
			for _, r := range si.R {
				b.WriteString(r.T.Text)
			}
			out = append(out, b.String())
		} else {
			out = append(out, si.T.Text)
		}
	}
	return out, nil
}

// ====================================================================
// 图片嫁接
// ====================================================================

// 形如 <Relationship Id="rId1" Type="..." Target="../media/image1.png"/>
var rels1Re = regexp.MustCompile(`<Relationship\s+[^>]*Id="(rId\d+)"[^>]*Target="([^"]+)"`)

// absorbSecondaryImages 处理 secondary 的图片：
//   - 读 drawing.xml，找 from.row 命中 rowMap 的 anchor
//   - 读对应 image 字节 + 拷到 state.newMedia
//   - 在 state.appendAnchors / appendRels 累加
func (st *mergeState) absorbSecondaryImages(zr *zip.ReadCloser, drawingXMLPath string, rowMap map[int]int) error {
	drawData, err := readEntryByName(zr, drawingXMLPath)
	if err != nil {
		return err
	}
	relsPath := sheetRelsPath(drawingXMLPath)
	relsData, err := readEntryByNameOptional(zr, relsPath)
	if err != nil {
		return err
	}

	// rId -> 源 image zip 路径
	ridToImagePath := map[string]string{}
	if relsData != nil {
		for _, m := range rels1Re.FindAllSubmatch(relsData, -1) {
			rid := string(m[1])
			target := string(m[2])
			imgPath := normalizeTarget(relsPath, target)
			ridToImagePath[rid] = imgPath
		}
	}

	// 提取 anchor 块并按 from.row 过滤
	anchorRe := regexp.MustCompile(`(?s)<xdr:twoCellAnchor\b[^>]*>.*?</xdr:twoCellAnchor>|<xdr:oneCellAnchor\b[^>]*>.*?</xdr:oneCellAnchor>`)
	embedRe := regexp.MustCompile(`r:embed="(rId\d+)"`)

	for _, anchor := range anchorRe.FindAll(drawData, -1) {
		fromMatch := fromRowRegexp.FindSubmatch(anchor)
		if len(fromMatch) < 2 {
			continue
		}
		fromRow0, _ := strconv.Atoi(string(fromMatch[1]))
		srcSheetRow := fromRow0 + 1
		newSheetRow, ok := rowMap[srcSheetRow]
		if !ok {
			continue
		}

		// 找 rId
		em := embedRe.FindSubmatch(anchor)
		if len(em) < 2 {
			continue
		}
		oldRID := string(em[1])
		imgPath := ridToImagePath[oldRID]
		if imgPath == "" {
			continue
		}

		// 读 image 字节
		imgData, err := readEntryByName(zr, imgPath)
		if err != nil {
			continue
		}

		// 分配新文件名（避免冲突）
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(imgPath)), ".")
		newName := st.allocMediaName(ext)
		newDstPath := "xl/media/" + newName
		st.newMedia = append(st.newMedia, mediaEntry{dstPath: newDstPath, data: imgData})
		if !st.imageExts[ext] {
			st.addedExts[ext] = true
		}

		// 分配新 rId
		newRID := fmt.Sprintf("rId%d", st.nextRID)
		st.nextRID++

		// 重写 anchor：from/to.row 改成 newSheetRow-1（0-based），r:embed 改 newRID
		// 同时也要重写 cNvPr id 避免冲突
		newPicID := st.nextPicID
		st.nextPicID++

		newAnchor := rewriteAnchorForMerge(anchor, newSheetRow-1, newRID, newPicID)
		st.appendAnchors = append(st.appendAnchors, newAnchor)

		// 追加 rel
		rel := fmt.Sprintf(
			`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/%s"/>`,
			newRID, newName)
		st.appendRels = append(st.appendRels, []byte(rel))
	}

	return nil
}

// allocMediaName 在 xl/media/ 下生成不冲突的文件名。
func (st *mergeState) allocMediaName(ext string) string {
	if ext == "" {
		ext = "png"
	}
	for i := 1; ; i++ {
		name := fmt.Sprintf("imageMerged_%d.%s", i, ext)
		if !st.usedMediaNames[name] {
			st.usedMediaNames[name] = true
			return name
		}
	}
}

// rewriteAnchorForMerge 把一个 anchor 块改成：
//   - from.row 和 to.row 都 = newRow0 (0-based)
//   - r:embed = newRID
//   - cNvPr id = newPicID
func rewriteAnchorForMerge(anchor []byte, newRow0 int, newRID string, newPicID int) []byte {
	// 改 from.row
	anchor = replaceFirstRowInSection(anchor, fromRowRegexp, newRow0)
	// 改 to.row（如果有）
	if toRowRegexp.Match(anchor) {
		anchor = replaceFirstRowInSection(anchor, toRowRegexp, newRow0)
	}
	// 改 r:embed
	anchor = regexp.MustCompile(`r:embed="rId\d+"`).ReplaceAll(anchor,
		[]byte(fmt.Sprintf(`r:embed="%s"`, newRID)))
	// 改 cNvPr id
	anchor = regexp.MustCompile(`<xdr:cNvPr\s+id="\d+"`).ReplaceAll(anchor,
		[]byte(fmt.Sprintf(`<xdr:cNvPr id="%d"`, newPicID)))
	return anchor
}

// replaceFirstRowInSection 把 anchor 里第一个 sectionRe 块（如 <xdr:from>...</xdr:from>）
// 内部第一个 <xdr:row>N</xdr:row> 的 N 替换成 newVal。复用 zipsurgery_drawing.go 里同名概念。
func replaceFirstRowInSection(anchor []byte, sectionRe *regexp.Regexp, newVal int) []byte {
	return sectionRe.ReplaceAllFunc(anchor, func(section []byte) []byte {
		return anyRowRegexp.ReplaceAll(section, []byte(fmt.Sprintf("<xdr:row>%d</xdr:row>", newVal)))
	})
}

// ====================================================================
// 写入阶段：sheet / drawing / drawing rels / Content_Types 的 rewrite
// ====================================================================

// rewriteSheet 在 </sheetData> 之前插入 appendRows。
func (st *mergeState) rewriteSheet(data []byte) ([]byte, error) {
	if len(st.appendRows) == 0 {
		return data, nil
	}
	idx := bytes.Index(data, []byte("</sheetData>"))
	if idx < 0 {
		return nil, core.New("SHEETDATA_END_NOT_FOUND", "sheet xml 里找不到 </sheetData>")
	}
	var buf bytes.Buffer
	buf.Grow(len(data) + 1024)
	buf.Write(data[:idx])
	for _, r := range st.appendRows {
		buf.Write(r)
	}
	buf.Write(data[idx:])
	return buf.Bytes(), nil
}

// rewriteDrawing 在 </xdr:wsDr> 之前插入 appendAnchors。
func (st *mergeState) rewriteDrawing(data []byte) ([]byte, error) {
	if len(st.appendAnchors) == 0 {
		return data, nil
	}
	idx := bytes.Index(data, []byte("</xdr:wsDr>"))
	if idx < 0 {
		return nil, core.New("DRAWING_END_NOT_FOUND", "drawing xml 里找不到 </xdr:wsDr>")
	}
	var buf bytes.Buffer
	buf.Grow(len(data) + 1024)
	buf.Write(data[:idx])
	for _, a := range st.appendAnchors {
		buf.Write(a)
	}
	buf.Write(data[idx:])
	return buf.Bytes(), nil
}

// rewriteDrawingRels 在 </Relationships> 之前插入 appendRels。
func (st *mergeState) rewriteDrawingRels(data []byte) ([]byte, error) {
	if len(st.appendRels) == 0 {
		return data, nil
	}
	idx := bytes.Index(data, []byte("</Relationships>"))
	if idx < 0 {
		return nil, core.New("RELS_END_NOT_FOUND", "drawing rels 里找不到 </Relationships>")
	}
	var buf bytes.Buffer
	buf.Grow(len(data) + 512)
	buf.Write(data[:idx])
	for _, r := range st.appendRels {
		buf.Write(r)
	}
	buf.Write(data[idx:])
	return buf.Bytes(), nil
}

// augmentContentTypes 给 [Content_Types].xml 追加新增图片扩展名的 Default 节点。
func (st *mergeState) augmentContentTypes(data []byte) []byte {
	if len(st.addedExts) == 0 {
		return data
	}
	idx := bytes.Index(data, []byte("</Types>"))
	if idx < 0 {
		return data
	}
	var ins bytes.Buffer
	for ext := range st.addedExts {
		ins.WriteString(fmt.Sprintf(`<Default Extension="%s" ContentType="image/%s"/>`, ext, ext))
	}
	var buf bytes.Buffer
	buf.Grow(len(data) + ins.Len())
	buf.Write(data[:idx])
	buf.Write(ins.Bytes())
	buf.Write(data[idx:])
	return buf.Bytes()
}
