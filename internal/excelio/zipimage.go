package excelio

// zipimage.go：绕过 excelize 的图片读取路径，直接从 xlsx zip 里按需取图。
//
// 为什么需要这个：excelize `GetPictures(sheet, cell)` 内部每次线性遍历全部
// drawing anchor（O(N)），再对命中的 anchor 调 image.DecodeConfig 做尺寸计算。
// 当 sheet 有 3000 张图、我们要取 500 张时，总耗时 ≈ 500 × 3000 anchor 比对 +
// 500 × image.DecodeConfig，实测 2m36s。
//
// 本文件的做法：
//  1. 一次 archive/zip 打开 xlsx，惰性读；
//  2. 一次 xml.Decode 扫完 drawingN.xml，建 row → anchor 索引（O(N)）；
//  3. 给命中行时从 zip 直接 ReadFile 取 xl/media/imageK.png 的 bytes；
//  4. image.DecodeConfig 仍然需要（为算 ScaleX/Y 还原源渲染尺寸），但只对命中行调；
//  5. 构造 excelize.Picture 交给下游 MigratePicture（AddPictureFromBytes）。

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"io"
	"math"
	"path"
	"strings"

	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"
)

// EMU 是 English Metric Unit，Office 在 drawing 里一律用 EMU 表示长度。1 像素 ≈ 9525 EMU。
const EMU = 9525

// ZipImageSource 持有 xlsx 的 zip 读句柄 + 若干 sheet 的 drawing 索引。
// 用完必须 Close。对外是线程不安全的：只在单个 processFile 里用。
type ZipImageSource struct {
	zr         *zip.ReadCloser
	fileByPath map[string]*zip.File // "xl/media/image1.png" → zip.File
	// workbook 级别的 sheetName → sheetPath 映射（"xl/worksheets/sheet1.xml"）
	sheetPath map[string]string
	// 已解析过的 sheet 的 drawing 索引
	sheetIdx map[string]*sheetDrawingIdx
}

// sheetDrawingIdx 是单个 sheet 的 drawing 信息。
type sheetDrawingIdx struct {
	drawingXML string // "xl/drawings/drawing1.xml"
	// rId → "xl/media/image5.png"
	rIdToMedia map[string]string
	// 1-based 行号 → 该行的所有 anchor（一格多图时多条）
	anchorsByRow map[int][]parsedAnchor
}

// parsedAnchor 是 drawing.xml 里一个图片 anchor 的精简视图。
// 我们只抓下游 MigratePicture 真正要用的字段。
type parsedAnchor struct {
	row, col    int    // 1-based（drawing 里 from.row/col 是 0-based，这里已 +1）
	cxEMU       int64  // 渲染宽（EMU）；来自 twoCellAnchor 的 to+ext 推导或 oneCellAnchor 的 ext
	cyEMU       int64  // 渲染高（EMU）
	rId         string // pic → blipFill → blip r:embed
	positioning string // "oneCell" 或 ""（twoCell 默认留空，excelize 自己用默认）
	lockAspect  bool
	altText     string
	name        string
}

// OpenZipImageSource 用 archive/zip 打开 xlsx，只读中央目录，不加载 media。
func OpenZipImageSource(xlsxPath string) (*ZipImageSource, error) {
	zr, err := zip.OpenReader(xlsxPath)
	if err != nil {
		return nil, core.Wrap("ZIP_OPEN_FAILED", "zip 打开失败: "+xlsxPath, err)
	}
	s := &ZipImageSource{
		zr:         zr,
		fileByPath: make(map[string]*zip.File, len(zr.File)),
		sheetPath:  map[string]string{},
		sheetIdx:   map[string]*sheetDrawingIdx{},
	}
	for _, f := range zr.File {
		s.fileByPath[f.Name] = f
	}
	if err := s.parseWorkbook(); err != nil {
		_ = zr.Close()
		return nil, err
	}
	return s, nil
}

// Close 释放 zip 句柄。
func (s *ZipImageSource) Close() error {
	if s == nil || s.zr == nil {
		return nil
	}
	err := s.zr.Close()
	s.zr = nil
	return err
}

// parseWorkbook 建立 sheetName → sheetPath 映射，为后续定位 drawing 做准备。
// 走的是 xl/workbook.xml + xl/_rels/workbook.xml.rels 的标准 OOXML 关系链。
func (s *ZipImageSource) parseWorkbook() error {
	wbBytes, err := s.readZipFile("xl/workbook.xml")
	if err != nil {
		return err
	}
	relsBytes, err := s.readZipFile("xl/_rels/workbook.xml.rels")
	if err != nil {
		return err
	}

	// workbook.xml 的 <sheet name="..." r:id="rIdN"/>
	type wbSheet struct {
		Name string `xml:"name,attr"`
		RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	}
	type wbSheets struct {
		Sheets []wbSheet `xml:"sheets>sheet"`
	}
	var wb wbSheets
	if err := xml.Unmarshal(wbBytes, &wb); err != nil {
		return core.Wrap("ZIP_PARSE_FAILED", "解析 workbook.xml 失败", err)
	}
	// rels 里 <Relationship Id=".." Target="worksheets/sheet1.xml" Type="...worksheet"/>
	type wbRel struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
		Type   string `xml:"Type,attr"`
	}
	type wbRels struct {
		Rels []wbRel `xml:"Relationship"`
	}
	var rels wbRels
	if err := xml.Unmarshal(relsBytes, &rels); err != nil {
		return core.Wrap("ZIP_PARSE_FAILED", "解析 workbook.xml.rels 失败", err)
	}
	ridToTarget := map[string]string{}
	for _, r := range rels.Rels {
		ridToTarget[r.ID] = r.Target
	}
	for _, sh := range wb.Sheets {
		tgt, ok := ridToTarget[sh.RID]
		if !ok {
			continue
		}
		s.sheetPath[sh.Name] = resolveRelTarget("xl/", tgt)
	}
	return nil
}

// LoadSheetAnchors 解析指定 sheet 的 drawing 信息。幂等。
// 若 sheet 没有关联 drawing（没图），返回 nil 且索引为空。
//
// 性能注意：旧实现先解压整个 sheetN.xml（10 万行可能 65MB+）只为读根节点
// 上的 <drawing r:id=".."/> 一个标签；现在改为直接看 sheetN.xml.rels（几 KB），
// 没有 Type 含 "/drawing" 的 Relationship 就立即返回——大幅降低无图文件的成本。
func (s *ZipImageSource) LoadSheetAnchors(sheet string) error {
	if _, ok := s.sheetIdx[sheet]; ok {
		return nil
	}
	idx := &sheetDrawingIdx{
		rIdToMedia:   map[string]string{},
		anchorsByRow: map[int][]parsedAnchor{},
	}
	s.sheetIdx[sheet] = idx

	sheetPath, ok := s.sheetPath[sheet]
	if !ok {
		return nil // 未知 sheet，允许静默跳过
	}
	// sheet 路径形如 "xl/worksheets/sheet1.xml"，rels 路径 "xl/worksheets/_rels/sheet1.xml.rels"
	sheetDir, sheetBase := pathSplit(sheetPath)
	relsPath := sheetDir + "_rels/" + sheetBase + ".rels"

	// FAST PATH: rels 文件不存在 = 该 sheet 完全没有外部关系（必然没图），直接返回。
	if _, ok := s.fileByPath[relsPath]; !ok {
		return nil
	}
	relsBytes, err := s.readZipFile(relsPath)
	if err != nil {
		return err
	}
	type wsRel struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
		Type   string `xml:"Type,attr"`
	}
	type wsRels struct {
		Rels []wsRel `xml:"Relationship"`
	}
	var sheetRels wsRels
	if err := xml.Unmarshal(relsBytes, &sheetRels); err != nil {
		return core.Wrap("ZIP_PARSE_FAILED", "解析 "+relsPath+" 失败", err)
	}
	// 直接找 Type 含 "/drawing" 的 Relationship——OOXML 标准里一个 sheet 至多
	// 一个 <drawing> 元素，所以 rels 里也至多一个 drawing 关系。这样省掉解压
	// sheet1.xml 拿 r:id 再回查 rels 这一整套大开销动作。
	var drawingRel string
	for _, r := range sheetRels.Rels {
		if strings.Contains(r.Type, "/drawing") {
			drawingRel = r.Target
			break
		}
	}
	if drawingRel == "" {
		return nil // 没图
	}
	drawingPath := resolveRelTarget(sheetDir, drawingRel)
	idx.drawingXML = drawingPath

	drawingDir, drawingBase := pathSplit(drawingPath)
	drawingRelsPath := drawingDir + "_rels/" + drawingBase + ".rels"

	// 解 drawing rels：rId → media target
	dRelsBytes, err := s.readZipFile(drawingRelsPath)
	if err != nil {
		return err
	}
	var dRels wsRels
	if err := xml.Unmarshal(dRelsBytes, &dRels); err != nil {
		return core.Wrap("ZIP_PARSE_FAILED", "解析 "+drawingRelsPath+" 失败", err)
	}
	for _, r := range dRels.Rels {
		idx.rIdToMedia[r.ID] = resolveRelTarget(drawingDir, r.Target)
	}

	// 解 drawing.xml：收集 anchor
	dBytes, err := s.readZipFile(drawingPath)
	if err != nil {
		return err
	}
	anchors, err := parseDrawingAnchors(dBytes)
	if err != nil {
		return err
	}
	for _, a := range anchors {
		idx.anchorsByRow[a.row] = append(idx.anchorsByRow[a.row], a)
	}
	return nil
}

// PictureCellsByRow 返回 row → PictureCellRef 列表，与 CollectPictureCellsByRow 兼容。
// 必须先调 LoadSheetAnchors(sheet)。
func (s *ZipImageSource) PictureCellsByRow(sheet string) map[int][]PictureCellRef {
	idx, ok := s.sheetIdx[sheet]
	if !ok {
		return nil
	}
	out := make(map[int][]PictureCellRef, len(idx.anchorsByRow))
	for row, anchors := range idx.anchorsByRow {
		for _, a := range anchors {
			cell, err := excelize.CoordinatesToCellName(a.col, a.row)
			if err != nil {
				continue
			}
			out[row] = append(out[row], PictureCellRef{Row: a.row, Col: a.col, Cell: cell})
		}
	}
	return out
}

// LoadPicturesForRowsZip 按指定行号加载图片字节，返回 row → []CellPictures。
// 不调 excelize.GetPictures，纯走 zip 读 + 一次 image.DecodeConfig 算 Scale。
func (s *ZipImageSource) LoadPicturesForRowsZip(
	sheet string, rows []int, progress PictureLoadProgressFn,
) (map[int][]CellPictures, error) {
	idx, ok := s.sheetIdx[sheet]
	if !ok {
		return nil, nil
	}
	// 估算 total 用于进度条
	totalCells := 0
	for _, r := range rows {
		totalCells += len(idx.anchorsByRow[r])
	}
	out := make(map[int][]CellPictures, len(rows))
	if progress != nil {
		progress(0, totalCells)
	}
	done := 0
	for _, row := range rows {
		anchors := idx.anchorsByRow[row]
		// 同一 cell 的多张图聚合到一个 CellPictures.Pictures
		type cellKey struct{ row, col int }
		grouped := map[cellKey][]excelize.Picture{}
		keyOrder := []cellKey{}
		for _, a := range anchors {
			mediaPath, ok := idx.rIdToMedia[a.rId]
			if !ok {
				done++
				continue
			}
			data, err := s.readZipFile(mediaPath)
			if err != nil {
				return nil, core.Wrap("ZIP_READ_MEDIA_FAILED", "读取图片字节失败: "+mediaPath, err)
			}
			pic := excelize.Picture{
				Extension: strings.ToLower(path.Ext(mediaPath)),
				File:      data,
				Format:    buildGraphicOptions(a, data),
			}
			k := cellKey{row: a.row, col: a.col}
			if _, exists := grouped[k]; !exists {
				keyOrder = append(keyOrder, k)
			}
			grouped[k] = append(grouped[k], pic)
			done++
			if progress != nil && (done%5 == 0 || done == totalCells) {
				progress(done, totalCells)
			}
		}
		for _, k := range keyOrder {
			out[row] = append(out[row], CellPictures{
				Row: k.row, Col: k.col, Pictures: grouped[k],
			})
		}
	}
	return out, nil
}

// buildGraphicOptions 从 anchor 的 cx/cy 和图片自身尺寸算出 ScaleX/ScaleY。
// 无法算时返回默认 1.0，等价于"按图片原始尺寸插入"。
func buildGraphicOptions(a parsedAnchor, data []byte) *excelize.GraphicOptions {
	opts := &excelize.GraphicOptions{
		ScaleX: 1.0, ScaleY: 1.0,
	}
	if a.positioning != "" {
		opts.Positioning = a.positioning
	}
	opts.LockAspectRatio = a.lockAspect
	opts.AltText = a.altText
	_ = a.name // GraphicOptions 没有 Name 字段，保持解析到为日后扩展用
	if a.cxEMU > 0 && a.cyEMU > 0 && len(data) > 0 {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil &&
			cfg.Width > 0 && cfg.Height > 0 {
			opts.ScaleX = math.Round(float64(a.cxEMU)/float64(EMU)/float64(cfg.Width)*100) / 100
			opts.ScaleY = math.Round(float64(a.cyEMU)/float64(EMU)/float64(cfg.Height)*100) / 100
		}
	}
	return opts
}

// readZipFile 从 zip 里读取某 entry 的完整字节。
func (s *ZipImageSource) readZipFile(name string) ([]byte, error) {
	f, ok := s.fileByPath[name]
	if !ok {
		return nil, core.Wrap("ZIP_ENTRY_MISSING",
			"zip 里找不到文件: "+name, fmt.Errorf("not found"))
	}
	rc, err := f.Open()
	if err != nil {
		return nil, core.Wrap("ZIP_OPEN_FAILED", "打开 zip 条目失败: "+name, err)
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// normalizeXLPath 把路径里的 ".." 片段合并掉，统一用 "/"。
// 用于把 "xl/worksheets/../drawings/drawing1.xml" 解析成 "xl/drawings/drawing1.xml"。
func normalizeXLPath(p string) string {
	// path.Clean 用 "/"，对 OOXML 里的相对路径正合适
	return path.Clean(p)
}

// resolveRelTarget 把 OOXML Relationship 的 Target 解析成 zip 里的绝对路径。
// OOXML 规则：
//   - Target 以 "/" 开头 → 包绝对路径，剥掉前导 "/" 直接用（例 "/xl/worksheets/s.xml"）
//   - 否则 → 相对 .rels 所在目录（例 workbook.xml.rels 的 base 是 "xl/"）
//
// 这里用 path.Clean 处理 ".." 和双斜杠。
func resolveRelTarget(base, target string) string {
	if strings.HasPrefix(target, "/") {
		return path.Clean(strings.TrimPrefix(target, "/"))
	}
	return path.Clean(base + target)
}

// pathSplit 把 "xl/drawings/drawing1.xml" 拆成 ("xl/drawings/", "drawing1.xml")。
func pathSplit(p string) (dir, base string) {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "", p
	}
	return p[:i+1], p[i+1:]
}
