package excelio

// formula_probe.go：单 sheet 级的"是否包含公式 cell"快速探测。
//
// 背景：extractor.processFile 在每个命中行上调用 readRowFormulas，而 readRowFormulas
// 内部对每列 cell 都调一次 excelize.CellFormula。fixture 01 命中 14286 行 x 14 列
// = 200k 次 CellFormula，而该 sheet 根本没有任何公式——这 10-40 秒完全浪费。
//
// 本文件的做法：
//  1. 在 processFile 扫描开始前调用 Reader.SheetHasFormulas(sheet)，一次性探测整张 sheet。
//  2. 探测走 archive/zip 直读 sheet XML，用 xml.Decoder 流式扫 `<f>` 元素。
//     命中第一个 <f> 即退出；无公式时要扫完但只 O(XML 字节数)，对 65MB 文件约 2 秒。
//  3. extractor 把结果传给 readRowFormulas，false 时跳过整个查询循环直接返回 nil slice。
//
// 对 fixture 02 等含公式文件：探测返回 true，readRowFormulas 保持原行为，公式零回归。
// 对 fixture 01 等无公式文件：200k 次 CellFormula → 0 次，节省 10-40 秒。

import (
	"archive/zip"
	"encoding/xml"
	"path"

	"excel-master/internal/core"
)

// SheetHasFormulas 判断指定 sheet 是否含任何 `<f>` 公式元素。
//
// 该方法用 archive/zip 打开原始 xlsx，独立于 r.f 的 excelize 状态，
// 读完即释放 zip 句柄（不影响 Reader 正在进行的流式迭代）。
//
// 结果在 Reader 上缓存；同一 Reader 多次询问同一 sheet 只会扫一次。
// 出错时返回 (false, err)，调用方可选择忽略错误回退到"假设有公式"（读公式路径保持原样）。
func (r *Reader) SheetHasFormulas(sheet string) (bool, error) {
	if r == nil || r.path == "" {
		return false, core.New("EXCEL_READ_FAILED", "Reader 未关联源文件路径")
	}
	if v, ok := r.formulaProbeCache[sheet]; ok {
		return v, nil
	}
	if r.formulaProbeCache == nil {
		r.formulaProbeCache = map[string]bool{}
	}

	zr, err := zip.OpenReader(r.path)
	if err != nil {
		return false, core.Wrap("ZIP_OPEN_FAILED", "打开 xlsx zip 失败", err)
	}
	defer zr.Close()

	sheetXMLPath, err := findSheetXMLPath(&zr.Reader, sheet)
	if err != nil {
		return false, err
	}

	// 流式读 sheetN.xml，一遇到 <f> 立即退出
	var sheetFile *zip.File
	for _, f := range zr.File {
		if f.Name == sheetXMLPath {
			sheetFile = f
			break
		}
	}
	if sheetFile == nil {
		// sheet XML 在 zip 里找不到——安全起见假设"有公式"走原路径
		r.formulaProbeCache[sheet] = true
		return true, nil
	}

	rc, err := sheetFile.Open()
	if err != nil {
		return false, core.Wrap("ZIP_READ_FAILED", "打开 sheet XML 失败", err)
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err != nil {
			// EOF 或解析错误都按"扫完没发现"处理
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// OOXML 公式 cell 形如 <c r="A1"><f>=B1*C1</f><v>...</v></c>
		// Name.Local == "f" 就是公式元素；其它以 f 开头的标签（如 formControl）不是 Local name
		if se.Name.Local == "f" {
			r.formulaProbeCache[sheet] = true
			return true, nil
		}
	}
	r.formulaProbeCache[sheet] = false
	return false, nil
}

// findSheetXMLPath 解析 xl/workbook.xml + xl/_rels/workbook.xml.rels 得到
// 给定 sheet 名对应的 zip 内路径（如 "xl/worksheets/sheet1.xml"）。
//
// 复用 zipimage.go 里同样的逻辑但独立实现，避免把 ZipImageSource 拖进纯粹的
// 公式探测场景——ZipImageSource 另外还要加载 fileByPath 索引、drawing 关系等，
// 对"只想知道这个 sheet 有没有公式"来说过重。
func findSheetXMLPath(zr *zip.Reader, sheetName string) (string, error) {
	wbBytes, err := readEntryByName(zr, "xl/workbook.xml")
	if err != nil {
		return "", err
	}
	relsBytes, err := readEntryByName(zr, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return "", err
	}

	type wbSheet struct {
		Name string `xml:"name,attr"`
		RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	}
	type wbSheets struct {
		XMLName xml.Name  `xml:"workbook"`
		Sheets  []wbSheet `xml:"sheets>sheet"`
	}
	var wb wbSheets
	if err := xml.Unmarshal(wbBytes, &wb); err != nil {
		return "", core.Wrap("ZIP_PARSE_FAILED", "解析 workbook.xml 失败", err)
	}

	type wbRel struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	}
	type wbRels struct {
		XMLName xml.Name `xml:"Relationships"`
		Rels    []wbRel  `xml:"Relationship"`
	}
	var rels wbRels
	if err := xml.Unmarshal(relsBytes, &rels); err != nil {
		return "", core.Wrap("ZIP_PARSE_FAILED", "解析 workbook.xml.rels 失败", err)
	}

	ridToTarget := map[string]string{}
	for _, r := range rels.Rels {
		ridToTarget[r.ID] = r.Target
	}

	for _, sh := range wb.Sheets {
		if sh.Name != sheetName {
			continue
		}
		tgt, ok := ridToTarget[sh.RID]
		if !ok {
			return "", core.New("ZIP_PARSE_FAILED", "sheet rId 找不到对应 target: "+sheetName)
		}
		// OOXML 规则：Target 以 "/" 开头为绝对包路径，否则相对 "xl/"（rels 所在目录）
		if len(tgt) > 0 && tgt[0] == '/' {
			return path.Clean(tgt[1:]), nil
		}
		return path.Clean(path.Join("xl", tgt)), nil
	}
	return "", core.New("SHEET_NOT_FOUND", "workbook.xml 里没有 sheet: "+sheetName)
}
