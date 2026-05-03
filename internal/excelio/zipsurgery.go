package excelio

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"excel-master/internal/core"
)

// CloneAndExtractZip 是 multi-sheet zip 手术 API 的单 sheet 便捷封装。
// 等价于 CloneAndExtractZipMulti(src, dst, map[string][]int{sheetName: keepRows})。
//
// keepRows 含义：
//   - nil → 保留该 sheet 所有行（不过滤、不重写行号）
//   - 非空 []int → 只保留这些 1-based 源行号；其余被删，图片锚点同步过滤
func CloneAndExtractZip(srcPath, dstPath, sheetName string, keepRows []int) error {
	return CloneAndExtractZipMulti(srcPath, dstPath, map[string][]int{sheetName: keepRows})
}

// CloneAndExtractZipMulti 是 V1.4 的核心工具：以纯 archive/zip + xml 的方式从 srcPath
// 生成 dstPath，保留 keepSheetRows 里所有 sheet（key 是 sheet 名），
// 且每个 sheet 按对应的 1-based 行号过滤（value 为 nil 表示该 sheet 全保留）。
// 不在 keepSheetRows 里的 sheet 会被整体删除（含其 sheetN.xml、关联的 rels、drawing 等）。
//
// 与 V1.3 的 excelize OpenFile + Save 路径不同，本函数**不会经过 excelize**，
// 因此不会触发 excelize issue #2061 之类的样式丢失问题：
// 所有 xml 条目除了 `xl/worksheets/sheetN.xml` / `xl/drawings/drawingM.xml`
// / `xl/workbook.xml` / `xl/_rels/workbook.xml.rels` / `[Content_Types].xml`
// 会做最小必要改写外，其余全部 bytewise 原样复制。
//
// 支持（因为 bytewise 复制）：
//   - 所有单元格样式、填充、字体、边框
//   - 条件格式（含 x14 扩展的 dataBar 等）
//   - 合并单元格（若跨越被删行，该 merge 会被丢弃；不跨越则保留）
//   - 同行公式（`=G54*J54` → 自动偏移成 `=G3*J3`）
//   - 图片锚点（twoCellAnchor / oneCellAnchor / editAs=oneCell 等属性完整保留）
//   - 数据验证、表格 <tables>、主题、共享字符串
//
// 限制 / 未来补强：
//   - 跨行公式（`=SUM(A1:A100)`）不做行号偏移，保留原字符串
//   - 跨行 mergeCell 被丢弃（避免合并到被删行产生坏文件）
//   - 不更新 dimension / tableParts 范围；Excel 能自适配
func CloneAndExtractZipMulti(srcPath, dstPath string, keepSheetRows map[string][]int) error {
	if len(keepSheetRows) == 0 {
		return core.New("NO_KEEP_SHEETS", "keepSheetRows 不能为空：至少要保留一个 sheet")
	}
	// 校验每个 sheet 的 keepRows，构造 rowMap（nil 表示保留全部）
	keepRowMaps := map[string]map[int]int{} // sheetName -> rowMap
	keepSheets := make([]string, 0, len(keepSheetRows))
	for s, rows := range keepSheetRows {
		keepSheets = append(keepSheets, s)
		if rows == nil {
			keepRowMaps[s] = nil
			continue
		}
		uniq := SortedUnique(rows)
		if len(uniq) == 0 {
			return core.New("NO_KEEP_ROWS", "sheet "+s+" 的 keepRows 长度为 0：要么传 nil 表示保留全部，要么传非空切片")
		}
		m := make(map[int]int, len(uniq))
		for i, r := range uniq {
			m[r] = i + 1
		}
		keepRowMaps[s] = m
	}

	// 打开源
	src, err := zip.OpenReader(srcPath)
	if err != nil {
		return core.Wrap("SRC_OPEN_FAILED", "打开源 xlsx 失败: "+srcPath, err)
	}
	defer src.Close()

	// 禁止覆盖已有输出
	if _, err := os.Stat(dstPath); err == nil {
		return core.Wrap("OUTPUT_CONFLICT", "输出文件已存在: "+dstPath, core.ErrOutputConflict)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return core.Wrap("OUTPUT_MKDIR_FAILED", "创建输出目录失败", err)
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return core.Wrap("OUTPUT_CREATE_FAILED", "创建输出文件失败: "+dstPath, err)
	}
	// 任何后续失败都要清掉半成品
	success := false
	defer func() {
		_ = out.Close()
		if !success {
			_ = os.Remove(dstPath)
		}
	}()

	dst := zip.NewWriter(out)
	closed := false
	defer func() {
		if !closed {
			_ = dst.Close()
		}
	}()

	// 解析 xlsx 结构：找到要保留的所有 sheet 的 xml/drawing 路径，以及要丢弃的资源
	layout, err := readXlsxLayout(&src.Reader, keepSheets)
	if err != nil {
		return err
	}

	// 把 sheet 路径 -> rowMap 的映射重新组织（layout 里 sheet 路径对应 sheet 名）
	sheetPathRowMap := map[string]map[int]int{}
	drawingPathRowMap := map[string]map[int]int{}
	for sheetName, ent := range layout.sheetEntries {
		sheetPathRowMap[ent.sheetXMLPath] = keepRowMaps[sheetName]
		if ent.drawingXMLPath != "" {
			drawingPathRowMap[ent.drawingXMLPath] = keepRowMaps[sheetName]
		}
	}

	for _, entry := range src.File {
		name := entry.Name

		// 跳过被丢弃的 sheet / 关联 rels
		if layout.shouldDrop(name) {
			continue
		}

		switch name {
		case "xl/workbook.xml":
			data, err := readZipEntry(entry)
			if err != nil {
				return err
			}
			newData, err := rewriteWorkbookXML(data, layout.keepRIDSet())
			if err != nil {
				return err
			}
			if err := writeZipEntry(dst, name, newData); err != nil {
				return err
			}
		case "xl/_rels/workbook.xml.rels":
			data, err := readZipEntry(entry)
			if err != nil {
				return err
			}
			newData, err := rewriteWorkbookRels(data, "", layout.dropRIDs)
			if err != nil {
				return err
			}
			if err := writeZipEntry(dst, name, newData); err != nil {
				return err
			}
		case "[Content_Types].xml":
			data, err := readZipEntry(entry)
			if err != nil {
				return err
			}
			newData, err := rewriteContentTypes(data, layout.dropSheetPaths)
			if err != nil {
				return err
			}
			if err := writeZipEntry(dst, name, newData); err != nil {
				return err
			}
		case "xl/calcChain.xml":
			// calcChain.xml 记录公式计算顺序，引用具体 cell（如 r="K1500"）。
			// 过滤行之后这些引用会指向被删的 cell → Excel 打开报"部分内容有问题"
			// 并强制删除整个 calcChain（在修复对话框里写成"已删除...公式(计算属性)"）。
			// 最专业做法：不复制 calcChain.xml 到输出。Excel 首次打开时会根据公式
			// 自动重建 calcChain，用户完全无感知。
			continue
		default:
			if rowMap, ok := sheetPathRowMap[name]; ok {
				data, err := readZipEntry(entry)
				if err != nil {
					return err
				}
				// 先展开共享公式（t="shared" + si="N" 机制）：
				// 如果不展开，主公式所在行被过滤后，其他 follower cell 的
				// <f t="shared" si="0"/> 找不到 si 定义 → Excel 报"部分内容有问题"
				// 并删除整列公式（在修复对话框里写成"已删除...共享公式"）。
				data = unshareFormulasInSheet(data)
				newData, err := rewriteSheetXML(data, rowMap)
				if err != nil {
					return err
				}
				if err := writeZipEntry(dst, name, newData); err != nil {
					return err
				}
				continue
			}
			if rowMap, ok := drawingPathRowMap[name]; ok {
				data, err := readZipEntry(entry)
				if err != nil {
					return err
				}
				newData, err := rewriteDrawingXML(data, rowMap)
				if err != nil {
					return err
				}
				if err := writeZipEntry(dst, name, newData); err != nil {
					return err
				}
				continue
			}
			// 其他条目 bytewise 原样复制
			if err := copyZipEntry(dst, entry, name); err != nil {
				return err
			}
		}
	}

	if err := dst.Close(); err != nil {
		return core.Wrap("ZIP_CLOSE_FAILED", "关闭输出 zip 失败", err)
	}
	closed = true
	success = true
	return nil
}

// sheetEntry 描述一个保留 sheet 的关键路径。
type sheetEntry struct {
	sheetXMLPath   string // 如 "xl/worksheets/sheet1.xml"
	drawingXMLPath string // 如 "xl/drawings/drawing1.xml"，若无图片则为 ""
	keepRID        string // workbook.xml 里该 sheet 的 r:id
}

// xlsxLayout 描述一个 xlsx 的多 sheet 保留 / 丢弃布局。
type xlsxLayout struct {
	sheetEntries   map[string]*sheetEntry // sheet name -> entry
	dropRIDs       []string               // 要删除的 sheet 的 r:id（rewriteWorkbookRels 会移除）
	dropSheetPaths []string               // 要删除的 sheet xml 路径（用于 [Content_Types].xml 清理）
	dropPrefixes   []string               // 要跳过的 zip 路径（其他 sheet 的 xml + rels）
}

// shouldDrop 判断该 zip 条目是否需要整个跳过（不写入 dst）。
func (l *xlsxLayout) shouldDrop(name string) bool {
	for _, p := range l.dropPrefixes {
		if name == p {
			return true
		}
	}
	return false
}

// keepRIDSet 返回所有保留 sheet 的 r:id 集合，给 rewriteWorkbookXML 用。
func (l *xlsxLayout) keepRIDSet() map[string]struct{} {
	s := make(map[string]struct{}, len(l.sheetEntries))
	for _, e := range l.sheetEntries {
		s[e.keepRID] = struct{}{}
	}
	return s
}

// readXlsxLayout 读 workbook.xml + rels + sheetN.xml.rels 定位多 sheet 保留 / 丢弃资源。
// keepSheets 是要保留的 sheet 名集合；其余 sheet 进 dropPrefixes / dropSheetPaths / dropRIDs。
func readXlsxLayout(zr *zip.Reader, keepSheets []string) (*xlsxLayout, error) {
	keepSet := map[string]struct{}{}
	for _, s := range keepSheets {
		keepSet[s] = struct{}{}
	}

	wbData, err := readEntryByName(zr, "xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	// 解析 <sheets><sheet name=".." sheetId=".." r:id=".." /></sheets>
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

	// 分类：keepRIDByName / dropRIDs
	keepRIDByName := map[string]string{} // sheetName -> rId
	var dropRIDs []string
	for _, s := range wb.Sheets.Sheet {
		if _, ok := keepSet[s.Name]; ok {
			keepRIDByName[s.Name] = s.RID
		} else {
			dropRIDs = append(dropRIDs, s.RID)
		}
	}
	// 校验：keep 集合每个名字都得在 workbook.xml 里找到
	for _, want := range keepSheets {
		if _, ok := keepRIDByName[want]; !ok {
			return nil, core.New("SHEET_NOT_FOUND", "workbook.xml 里找不到 sheet: "+want)
		}
	}

	// 读 workbook rels 找 rId 对应的 Target
	relsData, err := readEntryByName(zr, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
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

	layout := &xlsxLayout{
		sheetEntries: map[string]*sheetEntry{},
		dropRIDs:     dropRIDs,
	}

	// 处理保留 sheet：解析其 sheetXMLPath 和 drawingXMLPath
	for sheetName, rid := range keepRIDByName {
		target := ridToTarget[rid]
		if target == "" {
			return nil, core.New("SHEET_TARGET_MISSING", "rels 里找不到 keep rId target: "+rid+" (sheet "+sheetName+")")
		}
		sheetXMLPath := normalizeTarget("xl/_rels/workbook.xml.rels", target)
		ent := &sheetEntry{
			sheetXMLPath: sheetXMLPath,
			keepRID:      rid,
		}

		// 读该 sheet 的 rels 找 drawing Target
		sheetRels := sheetRelsPath(sheetXMLPath)
		sheetRelsData, err := readEntryByNameOptional(zr, sheetRels)
		if err != nil {
			return nil, err
		}
		if sheetRelsData != nil {
			var sRels relsXML
			if err := xml.Unmarshal(sheetRelsData, &sRels); err != nil {
				return nil, core.Wrap("SHEET_RELS_PARSE_FAILED", "解析 "+sheetRels+" 失败", err)
			}
			for _, r := range sRels.Relationship {
				if strings.HasSuffix(r.Type, "/drawing") {
					ent.drawingXMLPath = normalizeTarget(sheetRels, r.Target)
					break
				}
			}
		}
		layout.sheetEntries[sheetName] = ent
	}

	// 处理被丢弃 sheet：进 dropSheetPaths / dropPrefixes
	for _, rid := range dropRIDs {
		target := ridToTarget[rid]
		if target == "" {
			continue
		}
		dp := normalizeTarget("xl/_rels/workbook.xml.rels", target)
		layout.dropSheetPaths = append(layout.dropSheetPaths, dp)
		layout.dropPrefixes = append(layout.dropPrefixes, dp)
		layout.dropPrefixes = append(layout.dropPrefixes, sheetRelsPath(dp))
	}

	return layout, nil
}

// normalizeTarget 把 rels 里 Target 路径转成 zip 包内部的绝对路径（无前导 /）。
// 规则：若 Target 以 "/" 开头，视为包内绝对路径（去掉前导 /）；否则相对 rels 文件所在目录。
func normalizeTarget(relsPath, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	dir := path.Dir(relsPath) // e.g. xl/_rels
	// 上溯 _rels 目录，一般 rels 格式：xl/_rels/workbook.xml.rels, 其实际 dir 是 xl/
	if strings.HasSuffix(dir, "/_rels") {
		dir = strings.TrimSuffix(dir, "/_rels")
	}
	return path.Clean(path.Join(dir, target))
}

// sheetRelsPath 根据 sheet xml 路径推导出它的 rels 文件路径。
// e.g. xl/worksheets/sheet1.xml -> xl/worksheets/_rels/sheet1.xml.rels
func sheetRelsPath(sheetXMLPath string) string {
	dir := path.Dir(sheetXMLPath)
	base := path.Base(sheetXMLPath)
	return path.Join(dir, "_rels", base+".rels")
}

// readZipEntry 把 zip.File 读成完整字节。
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, core.Wrap("ZIP_ENTRY_OPEN_FAILED", "打开 zip 条目失败: "+f.Name, err)
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		return nil, core.Wrap("ZIP_ENTRY_READ_FAILED", "读 zip 条目失败: "+f.Name, err)
	}
	return buf.Bytes(), nil
}

// readEntryByName 按名读某个条目；不存在返回错误。
func readEntryByName(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			return readZipEntry(f)
		}
	}
	return nil, core.New("ZIP_ENTRY_NOT_FOUND", fmt.Sprintf("xlsx 里找不到条目: %s", name))
}

// readEntryByNameOptional 按名读某个条目；不存在返回 nil, nil（不报错）。
func readEntryByNameOptional(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			return readZipEntry(f)
		}
	}
	return nil, nil
}

// writeZipEntry 把新数据写入 dst zip（使用 Deflate 压缩，和 xlsx 标准一致）。
func writeZipEntry(w *zip.Writer, name string, data []byte) error {
	hdr := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	fw, err := w.CreateHeader(hdr)
	if err != nil {
		return core.Wrap("ZIP_WRITE_FAILED", "创建输出 zip 条目失败: "+name, err)
	}
	if _, err := fw.Write(data); err != nil {
		return core.Wrap("ZIP_WRITE_FAILED", "写入输出 zip 条目失败: "+name, err)
	}
	return nil
}

// copyZipEntry 把源 zip 条目原样复制到 dst（bytewise）。
func copyZipEntry(w *zip.Writer, src *zip.File, name string) error {
	hdr := &zip.FileHeader{
		Name:   name,
		Method: src.Method,
	}
	hdr.Modified = src.Modified
	fw, err := w.CreateHeader(hdr)
	if err != nil {
		return core.Wrap("ZIP_WRITE_FAILED", "创建输出 zip 条目失败: "+name, err)
	}
	rc, err := src.Open()
	if err != nil {
		return core.Wrap("ZIP_ENTRY_OPEN_FAILED", "打开源 zip 条目失败: "+name, err)
	}
	defer rc.Close()
	if _, err := io.Copy(fw, rc); err != nil {
		return core.Wrap("ZIP_COPY_FAILED", "复制 zip 条目失败: "+name, err)
	}
	return nil
}
