package excelio

// inplace_zip.go：纯 archive/zip + xml 手术，把 N 个"源 sheet 的过滤副本"作为
// **新 Sheet** 追加到源 xlsx，写出到 dst。
//
// 与 CopySheetWithin + FilterRowsInSheet 路径相比，本函数完全绕开 excelize：
//   - 不会触发 excelize.RemoveRow 的 O(N²) 性能陷阱
//   - 不会丢条件格式 / 图片 editAs 等元数据（excelize Save 已知 bug）
//   - 处理 5000 行 × 多个新 Sheet 从分钟级降到秒级
//
// 复用 zipsurgery_sheet.go 的 rewriteSheetXML 和 zipsurgery_drawing.go 的
// rewriteDrawingXML 做 sheetN.xml / drawingM.xml 的"行过滤 + 行号重映射"。
//
// 限制（与 CloneAndExtractZip 一致）：
//   - 跨行公式不偏移；跨行 mergeCell 被丢弃
//   - 不更新 dimension；Excel 自动适配

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"excel-master/internal/core"
)

// InplaceSheetSpec 描述要追加到源 xlsx 的一个新 Sheet。
//   - SourceSheet:  源 sheet 名，必须存在于源 xlsx
//   - NewSheetName: 新 sheet 名（≤31 字符且与源现有 sheet 名+其他 spec 唯一，调用方保证）
//   - KeepRows:     源 sheet 中保留的 1-based 行号；nil 表示完整复制（不过滤）
type InplaceSheetSpec struct {
	SourceSheet  string
	NewSheetName string
	KeepRows     []int
}

// AddFilteredSheetsZip 把 specs 描述的新 Sheet 追加到源 xlsx，写到 dstPath。
//
// 实现要点：
//   - workbook.xml / workbook.xml.rels / [Content_Types].xml 末尾追加新条目
//   - 新 sheetN.xml = rewriteSheetXML(源 sheet, rowMap)
//   - 新 sheetN.xml.rels：若源 sheet 有 drawing，则追加 1 条指向新 drawingM.xml 的 rels
//   - 新 drawingM.xml = rewriteDrawingXML(源 drawing, rowMap)
//   - 新 drawingM.xml.rels = bytewise 复制源 drawing rels（图片引用复用，无需复制图片字节）
//   - 其余 zip 条目 bytewise 原样复制
//
// dst 不能已存在；失败时清掉半成品。
func AddFilteredSheetsZip(srcPath, dstPath string, specs []InplaceSheetSpec) error {
	if len(specs) == 0 {
		return core.New("NO_SPECS", "specs 不能为空")
	}

	src, err := zip.OpenReader(srcPath)
	if err != nil {
		return core.Wrap("SRC_OPEN_FAILED", "打开源 xlsx 失败: "+srcPath, err)
	}
	defer src.Close()

	if _, err := os.Stat(dstPath); err == nil {
		return core.Wrap("OUTPUT_CONFLICT", "输出文件已存在: "+dstPath, core.ErrOutputConflict)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return core.Wrap("OUTPUT_MKDIR_FAILED", "创建输出目录失败", err)
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return core.Wrap("OUTPUT_CREATE_FAILED", "创建输出文件失败", err)
	}
	success := false
	defer func() {
		_ = out.Close()
		if !success {
			_ = os.Remove(dstPath)
		}
	}()

	layout, err := readFullXlsxLayout(&src.Reader)
	if err != nil {
		return err
	}

	plans, err := planInplaceSpecs(layout, specs)
	if err != nil {
		return err
	}

	// 改写后的 3 个元数据文件
	newWorkbook, err := appendSheetsToWorkbook(layout.workbookData, plans)
	if err != nil {
		return err
	}
	newWBRels := appendWorkbookRels(layout.workbookRelsData, plans)
	newCT := appendContentTypeOverrides(layout.contentTypesData, plans)

	dst := zip.NewWriter(out)
	closed := false
	defer func() {
		if !closed {
			_ = dst.Close()
		}
	}()

	// 1. 复制源 zip 的所有条目（除 3 个要替换的元数据文件）
	replaced := map[string]bool{
		"xl/workbook.xml":            true,
		"xl/_rels/workbook.xml.rels": true,
		"[Content_Types].xml":        true,
	}
	for _, entry := range src.File {
		if replaced[entry.Name] {
			continue
		}
		if err := copyZipEntry(dst, entry, entry.Name); err != nil {
			return err
		}
	}

	// 2. 写 3 个改写后的元数据文件
	if err := writeZipEntry(dst, "xl/workbook.xml", newWorkbook); err != nil {
		return err
	}
	if err := writeZipEntry(dst, "xl/_rels/workbook.xml.rels", newWBRels); err != nil {
		return err
	}
	if err := writeZipEntry(dst, "[Content_Types].xml", newCT); err != nil {
		return err
	}

	// 3. 写新 sheet/drawing/rels 条目
	for _, p := range plans {
		if err := writePlannedSheet(&src.Reader, dst, p); err != nil {
			return err
		}
	}

	if err := dst.Close(); err != nil {
		return core.Wrap("ZIP_CLOSE_FAILED", "关闭输出 zip 失败", err)
	}
	closed = true
	success = true
	return nil
}

// ListSheetNamesZip 不依赖 excelize，纯 zip+xml 读源 xlsx 的 sheet 名列表（按 workbook.xml 顺序）。
// 给上层 inplace 流程在批量 spec 之前做唯一化用。
func ListSheetNamesZip(srcPath string) ([]string, error) {
	r, err := zip.OpenReader(srcPath)
	if err != nil {
		return nil, core.Wrap("SRC_OPEN_FAILED", "打开源 xlsx 失败: "+srcPath, err)
	}
	defer r.Close()
	wbData, err := readEntryByName(&r.Reader, "xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	type sheetNode struct {
		Name string `xml:"name,attr"`
	}
	type wbXML struct {
		Sheets struct {
			Sheet []sheetNode `xml:"sheet"`
		} `xml:"sheets"`
	}
	var wb wbXML
	if err := xml.Unmarshal(wbData, &wb); err != nil {
		return nil, core.Wrap("WB_PARSE_FAILED", "解析 workbook.xml 失败", err)
	}
	out := make([]string, 0, len(wb.Sheets.Sheet))
	for _, s := range wb.Sheets.Sheet {
		out = append(out, s.Name)
	}
	return out, nil
}

// UniqueNameInSet 在已有 set 之外为 base 生成唯一 sheet 名（_2, _3 ...），并把结果加入 set。
// 长度仍按 31 字符截断（SanitizeSheetName 只做非法字符过滤；这里再裁一次）。
func UniqueNameInSet(base string, set map[string]struct{}) string {
	base = SanitizeSheetName(base)
	if _, ok := set[base]; !ok {
		set[base] = struct{}{}
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s_%d", base, i)
		// 31 字符限制
		if len(cand) > 31 {
			// 把 base 尾部削掉给后缀腾位
			suffix := fmt.Sprintf("_%d", i)
			keep := 31 - len(suffix)
			if keep < 1 {
				keep = 1
			}
			runes := []rune(base)
			if len(runes) > keep {
				cand = string(runes[:keep]) + suffix
			}
		}
		if _, ok := set[cand]; !ok {
			set[cand] = struct{}{}
			return cand
		}
	}
}

// ============== 内部实现 ==============

// fullXlsxLayout 描述源 xlsx 的全量布局（用于追加新 sheet 时定位/分配编号）。
type fullXlsxLayout struct {
	workbookData     []byte
	workbookRelsData []byte
	contentTypesData []byte
	// sheet 名 -> (sheet xml 路径, drawing xml 路径或 "", 现有 rId)
	sheetByName   map[string]*sheetEntry
	maxSheetIdx   int // 现有 xl/worksheets/sheetN.xml 最大 N
	maxDrawingIdx int // 现有 xl/drawings/drawingM.xml 最大 M
	maxRIDNum     int // workbook rels 现有 rIdX 最大 X
	maxSheetID    int // workbook.xml <sheet sheetId=...> 最大值
}

func readFullXlsxLayout(zr *zip.Reader) (*fullXlsxLayout, error) {
	wbData, err := readEntryByName(zr, "xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	relsData, err := readEntryByName(zr, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}
	ctData, err := readEntryByName(zr, "[Content_Types].xml")
	if err != nil {
		return nil, err
	}

	type sheetNode struct {
		Name    string `xml:"name,attr"`
		SheetID string `xml:"sheetId,attr"`
		RID     string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	}
	type wbXML struct {
		Sheets struct {
			Sheet []sheetNode `xml:"sheet"`
		} `xml:"sheets"`
	}
	var wb wbXML
	if err := xml.Unmarshal(wbData, &wb); err != nil {
		return nil, core.Wrap("WB_PARSE_FAILED", "解析 workbook.xml 失败", err)
	}

	type rel struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
		Type   string `xml:"Type,attr"`
	}
	type relsXML struct {
		Relationship []rel `xml:"Relationship"`
	}
	var rels relsXML
	if err := xml.Unmarshal(relsData, &rels); err != nil {
		return nil, core.Wrap("WB_RELS_PARSE_FAILED", "解析 workbook.xml.rels 失败", err)
	}
	ridToTarget := map[string]string{}
	for _, r := range rels.Relationship {
		ridToTarget[r.ID] = r.Target
	}

	layout := &fullXlsxLayout{
		workbookData:     wbData,
		workbookRelsData: relsData,
		contentTypesData: ctData,
		sheetByName:      map[string]*sheetEntry{},
	}

	// 扫所有 zip 条目找最大 sheetN / drawingM 编号
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			if m := sheetIdxRegexp.FindStringSubmatch(f.Name); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil && n > layout.maxSheetIdx {
					layout.maxSheetIdx = n
				}
			}
		}
		if strings.HasPrefix(f.Name, "xl/drawings/drawing") && strings.HasSuffix(f.Name, ".xml") {
			if m := drawingIdxRegexp.FindStringSubmatch(f.Name); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil && n > layout.maxDrawingIdx {
					layout.maxDrawingIdx = n
				}
			}
		}
	}

	// 找最大 rId 数字
	for _, r := range rels.Relationship {
		if m := ridNumRegexp.FindStringSubmatch(r.ID); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > layout.maxRIDNum {
				layout.maxRIDNum = n
			}
		}
	}

	// 解析每个 sheet 的路径 + drawing
	type relEntry struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
		Type   string `xml:"Type,attr"`
	}
	type sheetRelsXML struct {
		Relationship []relEntry `xml:"Relationship"`
	}
	for _, s := range wb.Sheets.Sheet {
		if sid, err := strconv.Atoi(s.SheetID); err == nil && sid > layout.maxSheetID {
			layout.maxSheetID = sid
		}
		target := ridToTarget[s.RID]
		if target == "" {
			continue
		}
		sheetXMLPath := normalizeTarget("xl/_rels/workbook.xml.rels", target)
		ent := &sheetEntry{
			sheetXMLPath: sheetXMLPath,
			keepRID:      s.RID,
		}
		// 读 sheet 自己的 rels 找 drawing target
		srPath := sheetRelsPath(sheetXMLPath)
		srData, err := readEntryByNameOptional(zr, srPath)
		if err != nil {
			return nil, err
		}
		if srData != nil {
			var sr sheetRelsXML
			if err := xml.Unmarshal(srData, &sr); err == nil {
				for _, r := range sr.Relationship {
					if strings.HasSuffix(r.Type, "/drawing") {
						ent.drawingXMLPath = normalizeTarget(srPath, r.Target)
						break
					}
				}
			}
		}
		layout.sheetByName[s.Name] = ent
	}

	return layout, nil
}

var (
	sheetIdxRegexp   = regexp.MustCompile(`sheet(\d+)\.xml$`)
	drawingIdxRegexp = regexp.MustCompile(`drawing(\d+)\.xml$`)
	ridNumRegexp     = regexp.MustCompile(`^rId(\d+)$`)
)

// plannedSheet 是把 InplaceSheetSpec 翻译成"具体 zip 条目分配"的结果。
type plannedSheet struct {
	spec       InplaceSheetSpec
	rowMap     map[int]int // 源行号 -> 新行号；nil 表示"全保留不重写"
	srcSheet   string      // 源 sheetN.xml 路径
	srcDrawing string      // 源 drawingM.xml 路径（无图则空）
	srcDrawRel string      // 源 drawingM.xml.rels 路径（无图则空）

	newSheet   string // xl/worksheets/sheetN.xml
	newSheetRl string // xl/worksheets/_rels/sheetN.xml.rels
	newDraw    string // xl/drawings/drawingM.xml（无图则空）
	newDrawRl  string // xl/drawings/_rels/drawingM.xml.rels（无图则空）

	rID     string // 新 sheet 的 workbook 级 rId（如 rId7）
	sheetID int    // 新 sheet 的 sheetId 属性（递增整数）
}

func planInplaceSpecs(layout *fullXlsxLayout, specs []InplaceSheetSpec) ([]plannedSheet, error) {
	nextSheetIdx := layout.maxSheetIdx + 1
	nextDrawingIdx := layout.maxDrawingIdx + 1
	nextRID := layout.maxRIDNum + 1
	nextSheetID := layout.maxSheetID + 1

	plans := make([]plannedSheet, 0, len(specs))
	for _, spec := range specs {
		ent, ok := layout.sheetByName[spec.SourceSheet]
		if !ok {
			return nil, core.New("SHEET_NOT_FOUND", "源 xlsx 里找不到 sheet: "+spec.SourceSheet)
		}
		var rm map[int]int
		if spec.KeepRows != nil {
			uniq := SortedUnique(spec.KeepRows)
			if len(uniq) == 0 {
				return nil, core.New("NO_KEEP_ROWS", spec.NewSheetName+" 的 KeepRows 为空")
			}
			rm = make(map[int]int, len(uniq))
			for i, r := range uniq {
				rm[r] = i + 1
			}
		}
		p := plannedSheet{
			spec:       spec,
			rowMap:     rm,
			srcSheet:   ent.sheetXMLPath,
			srcDrawing: ent.drawingXMLPath,
			newSheet:   fmt.Sprintf("xl/worksheets/sheet%d.xml", nextSheetIdx),
			newSheetRl: fmt.Sprintf("xl/worksheets/_rels/sheet%d.xml.rels", nextSheetIdx),
			rID:        fmt.Sprintf("rId%d", nextRID),
			sheetID:    nextSheetID,
		}
		if ent.drawingXMLPath != "" {
			p.srcDrawRel = drawingRelsPath(ent.drawingXMLPath)
			p.newDraw = fmt.Sprintf("xl/drawings/drawing%d.xml", nextDrawingIdx)
			p.newDrawRl = fmt.Sprintf("xl/drawings/_rels/drawing%d.xml.rels", nextDrawingIdx)
			nextDrawingIdx++
		}
		plans = append(plans, p)
		nextSheetIdx++
		nextRID++
		nextSheetID++
	}
	return plans, nil
}

func writePlannedSheet(zr *zip.Reader, dst *zip.Writer, p plannedSheet) error {
	// 新 sheet xml = 源 sheet xml 经 rewriteSheetXML 过滤
	srcSheetData, err := readEntryByName(zr, p.srcSheet)
	if err != nil {
		return err
	}
	newSheetData, err := rewriteSheetXML(srcSheetData, p.rowMap)
	if err != nil {
		return err
	}
	// 若新 sheet 关联 drawing，需要把源 sheet 的 <drawing r:id="..."/> 节点的 rId
	// 改成"在新 sheet rels 里固定使用的 rId1"，避免源 rId 冲突。
	if p.newDraw != "" {
		newSheetData = rewriteSheetDrawingRID(newSheetData, "rId1")
	}
	if err := writeZipEntry(dst, p.newSheet, newSheetData); err != nil {
		return err
	}

	if p.newDraw == "" {
		return nil
	}

	// 新 sheet rels：指向新 drawing
	relsXML := fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/%s"/>`+
			`</Relationships>`,
		path.Base(p.newDraw))
	if err := writeZipEntry(dst, p.newSheetRl, []byte(relsXML)); err != nil {
		return err
	}

	// 新 drawing xml = 源 drawing xml 经 rewriteDrawingXML 过滤
	srcDrawData, err := readEntryByName(zr, p.srcDrawing)
	if err != nil {
		return err
	}
	newDrawData, err := rewriteDrawingXML(srcDrawData, p.rowMap)
	if err != nil {
		return err
	}
	if err := writeZipEntry(dst, p.newDraw, newDrawData); err != nil {
		return err
	}

	// 新 drawing rels：bytewise 复制源 drawing rels（image rId 命名空间是
	// 每个 drawing rels 文件独立的，源里的 rId1/rId2 在新文件里仍可用，
	// 因此可以无脑复制）。
	if p.srcDrawRel != "" {
		srcDrawRelData, err := readEntryByNameOptional(zr, p.srcDrawRel)
		if err != nil {
			return err
		}
		if srcDrawRelData != nil {
			if err := writeZipEntry(dst, p.newDrawRl, srcDrawRelData); err != nil {
				return err
			}
		}
	}
	return nil
}

// drawingRelsPath: xl/drawings/drawing1.xml -> xl/drawings/_rels/drawing1.xml.rels
func drawingRelsPath(drawingXMLPath string) string {
	if drawingXMLPath == "" {
		return ""
	}
	dir := path.Dir(drawingXMLPath)
	base := path.Base(drawingXMLPath)
	return path.Join(dir, "_rels", base+".rels")
}

// appendSheetsToWorkbook 在 workbook.xml 的 </sheets> 前插入新 <sheet> 节点。
func appendSheetsToWorkbook(data []byte, plans []plannedSheet) ([]byte, error) {
	closeIdx := bytes.Index(data, []byte("</sheets>"))
	if closeIdx < 0 {
		return nil, core.New("WB_NO_SHEETS", "workbook.xml 没找到 </sheets> 节点")
	}
	var b bytes.Buffer
	b.Grow(len(data) + 200*len(plans))
	b.Write(data[:closeIdx])
	for _, p := range plans {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="%s"/>`,
			xmlAttrEscape(p.spec.NewSheetName), p.sheetID, p.rID)
	}
	b.Write(data[closeIdx:])
	return b.Bytes(), nil
}

// appendWorkbookRels 在 workbook.xml.rels 的 </Relationships> 前插入新 Relationship。
func appendWorkbookRels(data []byte, plans []plannedSheet) []byte {
	closeIdx := bytes.Index(data, []byte("</Relationships>"))
	if closeIdx < 0 {
		return data
	}
	var b bytes.Buffer
	b.Grow(len(data) + 200*len(plans))
	b.Write(data[:closeIdx])
	for _, p := range plans {
		target := "worksheets/" + path.Base(p.newSheet)
		fmt.Fprintf(&b,
			`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="%s"/>`,
			p.rID, target)
	}
	b.Write(data[closeIdx:])
	return b.Bytes()
}

// appendContentTypeOverrides 在 [Content_Types].xml 的 </Types> 前插入新 Override。
func appendContentTypeOverrides(data []byte, plans []plannedSheet) []byte {
	closeIdx := bytes.Index(data, []byte("</Types>"))
	if closeIdx < 0 {
		return data
	}
	var b bytes.Buffer
	b.Grow(len(data) + 300*len(plans))
	b.Write(data[:closeIdx])
	for _, p := range plans {
		fmt.Fprintf(&b,
			`<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`,
			p.newSheet)
		if p.newDraw != "" {
			fmt.Fprintf(&b,
				`<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/>`,
				p.newDraw)
		}
	}
	b.Write(data[closeIdx:])
	return b.Bytes()
}

// xmlAttrEscape 转义 XML 属性值。
func xmlAttrEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// rewriteSheetDrawingRID 把 sheetN.xml 中 <drawing r:id="..."/> 的 rId 替换为 newRID。
// xlsx 里 sheet 与 drawing 的关联是通过 sheet 自己的 rels 文件（xl/worksheets/_rels/sheetN.xml.rels）
// 中的 Id 引用，sheet 文件本身的 <drawing r:id="rIdX"/> 必须与 rels 里的 Id 对得上。
// 我们的新 sheet rels 固定写 rId1，所以这里把源 sheet xml 的 rId 替换成 rId1。
// 若源 sheet xml 没有 <drawing>（无图）—— 不会被调到这条路径，故无副作用。
func rewriteSheetDrawingRID(data []byte, newRID string) []byte {
	return drawingTagRIDRegexp.ReplaceAll(data,
		[]byte(`<drawing r:id="`+newRID+`"`))
}

// 匹配 <drawing r:id="rIdX"（自闭合或带子标签都覆盖前缀）。
var drawingTagRIDRegexp = regexp.MustCompile(`<drawing\s+r:id="[^"]+"`)

// 防止 io 未引用（仅 read/write 间接用到）
var _ = io.Copy
