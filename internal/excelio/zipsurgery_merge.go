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
	"regexp"
	"strconv"
	"strings"

	"excel-master/internal/core"
)

// MergeSource 描述一个参与合并的源文件。
type MergeSource struct {
	SrcPath   string // 源 xlsx 绝对路径
	SheetName string // 用哪个 sheet（必须与 primary 的 SheetName 相同）
	KeepRows  []int  // 1-based 源行号（不含表头；表头由 primary 提供）
}

// CloneAndMergePreserved 以 primary 为"模板母体"做 zip 手术，把 secondaries 里的命中行 +
// 图片"嫁接"到模板，输出到 dstPath。所有 source 必须用同一个 SheetName。
//
// 输出文件特点：
//   - 表头来自 primary（蓝底白字等样式 100% 保留）
//   - primary 的命中行 100% 复刻（样式、公式、合并单元格、图片锚点）
//   - 其他 secondary 源的命中行追加到 primary sheetData 末尾：
//     · 单元格值用 inlineStr 嵌入（不依赖 secondary 的 sharedStrings）
//     · 单元格样式 ID 重用 primary 第一数据行（row 2）对应列的 styleID
//     · 因此 secondary 的"特殊单元格样式"会丢失，只继承 primary 的统一行样式
//   - 其他 secondary 的图片：image 字节拷到 primary 的 xl/media/，
//     在 primary drawing1.xml 末尾追加 anchor，from.row/to.row 指向新行号
//
// 限制（MVP）：
//   - 要求所有 source 的 sheet 列数相同、列顺序一致；不做按列名对齐
//   - secondary 的公式只取缓存值（<v>），不再做行号偏移
//   - 不处理 secondary 的 sheet 内 mergeCells / 条件格式 / 数据验证
//   - 不处理 absoluteAnchor 类型图片
func CloneAndMergePreserved(primary MergeSource, dstPath string, secondaries []MergeSource) error {
	if primary.SrcPath == "" || primary.SheetName == "" {
		return core.New("MERGE_BAD_PRIMARY", "primary 必须指定 SrcPath 与 SheetName")
	}
	for i, s := range secondaries {
		if s.SrcPath == "" || s.SheetName == "" {
			return core.New("MERGE_BAD_SECONDARY", fmt.Sprintf("secondaries[%d] 必须指定 SrcPath 与 SheetName", i))
		}
		if s.SheetName != primary.SheetName {
			return core.New("MERGE_SHEETNAME_MISMATCH",
				fmt.Sprintf("secondaries[%d].SheetName=%q 与 primary.SheetName=%q 不一致", i, s.SheetName, primary.SheetName))
		}
	}

	if _, err := os.Stat(dstPath); err == nil {
		return core.Wrap("OUTPUT_CONFLICT", "输出文件已存在: "+dstPath, core.ErrOutputConflict)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return core.Wrap("OUTPUT_MKDIR_FAILED", "创建输出目录失败", err)
	}

	// Pass 1: 用 primary 生成临时基底
	tmpPath := dstPath + ".tmp"
	_ = os.Remove(tmpPath)
	if err := CloneAndExtractZipMulti(primary.SrcPath, tmpPath,
		map[string][]int{primary.SheetName: primary.KeepRows}); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()

	// 没有 secondaries：直接 rename 完事
	if len(secondaries) == 0 {
		return os.Rename(tmpPath, dstPath)
	}

	// Pass 2: 把 secondaries 的命中行 + 图片"嫁接"到临时基底，输出 dst
	return mergeSecondariesInto(tmpPath, dstPath, primary.SheetName, secondaries)
}

// mergeSecondariesInto 读 tmpPath（已经是 primary 的产物），把 secondaries 嫁接进去，写到 dstPath。
func mergeSecondariesInto(tmpPath, dstPath, sheetName string, secondaries []MergeSource) error {
	tmpZip, err := zip.OpenReader(tmpPath)
	if err != nil {
		return core.Wrap("MERGE_OPEN_TMP_FAILED", "打开临时基底失败: "+tmpPath, err)
	}
	defer tmpZip.Close()

	// 1) 解析 tmp 的状态
	state, err := readMergeState(tmpZip, sheetName)
	if err != nil {
		return err
	}

	// 2) 提取每个 secondary 的命中行 + 图片，累加到 state（构造新 sheetData rows / drawing anchors / media）
	for i, s := range secondaries {
		if err := state.absorbSecondary(s); err != nil {
			return core.Wrap("MERGE_ABSORB_FAILED", fmt.Sprintf("处理 secondaries[%d] (%s) 失败", i, s.SrcPath), err)
		}
	}

	// 3) 把 tmpZip 的所有条目写入 dst，关键 entry 用 state 重写后的版本替换
	out, err := os.Create(dstPath)
	if err != nil {
		return core.Wrap("OUTPUT_CREATE_FAILED", "创建输出文件失败: "+dstPath, err)
	}
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

	written := map[string]bool{}

	for _, entry := range tmpZip.File {
		name := entry.Name
		switch {
		case name == state.sheetXMLPath:
			data, err := readZipEntry(entry)
			if err != nil {
				return err
			}
			newData, err := state.rewriteSheet(data)
			if err != nil {
				return err
			}
			if err := writeZipEntry(dst, name, newData); err != nil {
				return err
			}
		case state.drawingXMLPath != "" && name == state.drawingXMLPath:
			data, err := readZipEntry(entry)
			if err != nil {
				return err
			}
			newData, err := state.rewriteDrawing(data)
			if err != nil {
				return err
			}
			if err := writeZipEntry(dst, name, newData); err != nil {
				return err
			}
		case state.drawingRelsPath != "" && name == state.drawingRelsPath:
			data, err := readZipEntry(entry)
			if err != nil {
				return err
			}
			newData, err := state.rewriteDrawingRels(data)
			if err != nil {
				return err
			}
			if err := writeZipEntry(dst, name, newData); err != nil {
				return err
			}
		case name == "[Content_Types].xml":
			data, err := readZipEntry(entry)
			if err != nil {
				return err
			}
			newData := state.augmentContentTypes(data)
			if err := writeZipEntry(dst, name, newData); err != nil {
				return err
			}
		default:
			if err := copyZipEntry(dst, entry, name); err != nil {
				return err
			}
		}
		written[name] = true
	}

	// 4) 写入新增的 media 文件 + 如果 primary 没 drawing 则新建 drawing/rels（暂不支持，要求 primary 必须有图）
	for _, img := range state.newMedia {
		if written[img.dstPath] {
			continue
		}
		if err := writeZipEntry(dst, img.dstPath, img.data); err != nil {
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

// ====================================================================
// mergeState：累积合并过程的所有可变状态
// ====================================================================

// mergeState 记录"以 tmp 为基底，准备嫁接 secondaries"的所有可变状态。
type mergeState struct {
	sheetName string

	// 关键 zip 路径
	sheetXMLPath    string // xl/worksheets/sheet1.xml
	drawingXMLPath  string // xl/drawings/drawing1.xml（若没有图，则为空）
	drawingRelsPath string // xl/drawings/_rels/drawing1.xml.rels

	// 累积写入的 sheet 行（已经是最终 row 字节，包含 r/s 属性，已重写行号）
	appendRows [][]byte

	// 累积写入的 drawing anchors（已经是最终字节，from/to.row 已重写）
	appendAnchors [][]byte

	// 累积写入的 drawing rels（每条是 <Relationship Id=".." Target="../media/.."/>）
	appendRels [][]byte

	// 累积要新增的 media 文件
	newMedia []mediaEntry

	// 用于分配新 ID
	nextRow         int            // 下一个可用的 1-based 行号（追加到 sheetData 末尾）
	nextRID         int            // 下一个 drawing rels 里可用的 rId 序号
	nextPicID       int            // 下一个 drawing.xml 里 cNvPr id 属性
	usedMediaNames  map[string]bool // 已存在的 media 文件名（包括 tmp 里的 + 新增的）

	// 模板第一数据行（row 2）的列样式 ID：col 索引 (0-based) -> styleID 字符串（如 "3"）
	colStyles map[int]string
	// 模板列总数（按表头列数；secondaries 同列数即可）
	headerCols int

	// 用于追加新增的 image 扩展名的 [Content_Types].xml Default 节点
	imageExts map[string]bool // ext (e.g. "png") -> true 表示 tmp 里已有
	addedExts map[string]bool // 本次新增的扩展名
}

type mediaEntry struct {
	dstPath string // xl/media/imageMerged_N.png
	data    []byte
}

// readMergeState 解析 tmp 基底，初始化 state。
func readMergeState(zr *zip.ReadCloser, sheetName string) (*mergeState, error) {
	st := &mergeState{
		sheetName:      sheetName,
		usedMediaNames: map[string]bool{},
		colStyles:      map[int]string{},
		imageExts:      map[string]bool{},
		addedExts:      map[string]bool{},
	}

	// 1) 找 sheet xml + drawing xml 路径（用 layout 解析 tmp，但 tmp 只剩 1 个 sheet）
	layout, err := readXlsxLayout(zr, []string{sheetName})
	if err != nil {
		return nil, err
	}
	ent, ok := layout.sheetEntries[sheetName]
	if !ok {
		return nil, core.New("MERGE_SHEET_NOT_FOUND", "tmp 里找不到 sheet: "+sheetName)
	}
	st.sheetXMLPath = ent.sheetXMLPath
	st.drawingXMLPath = ent.drawingXMLPath
	if st.drawingXMLPath != "" {
		st.drawingRelsPath = sheetRelsPath(st.drawingXMLPath)
	}

	// 2) 读 sheet xml 找最后一行 + 第一数据行 (row 2) 的列样式
	sheetData, err := readEntryByName(zr, st.sheetXMLPath)
	if err != nil {
		return nil, err
	}
	lastRow, headerCols, colStyles, err := analyzeSheetForMerge(sheetData)
	if err != nil {
		return nil, err
	}
	st.nextRow = lastRow + 1
	st.headerCols = headerCols
	st.colStyles = colStyles

	// 3) 读 drawing 找最大 rId / picID（如果有 drawing）
	if st.drawingXMLPath != "" {
		drawData, err := readEntryByName(zr, st.drawingXMLPath)
		if err != nil {
			return nil, err
		}
		st.nextPicID = analyzeDrawingNextPicID(drawData)

		relsData, err := readEntryByNameOptional(zr, st.drawingRelsPath)
		if err != nil {
			return nil, err
		}
		if relsData != nil {
			st.nextRID = analyzeRelsNextRID(relsData)
		} else {
			st.nextRID = 1
		}
	} else {
		st.nextPicID = 1
		st.nextRID = 1
	}

	// 4) 收集 tmp 里已有的 media 文件名
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/media/") {
			base := path.Base(f.Name)
			st.usedMediaNames[base] = true
			ext := strings.TrimPrefix(strings.ToLower(path.Ext(base)), ".")
			if ext != "" {
				st.imageExts[ext] = true
			}
		}
	}

	return st, nil
}

// ====================================================================
// 解析 tmp 状态的辅助（这一轮的最简实现）
// ====================================================================

var (
	rowOpenRe   = regexp.MustCompile(`<row r="(\d+)"`)
	cellRowRe   = regexp.MustCompile(`<c r="([A-Z]+)2"[^/>]*?(?:s="(\d+)")?[^/>]*?(?:/>|>)`) // row 2 的 cell + style
	picIDRe     = regexp.MustCompile(`<xdr:cNvPr\s+id="(\d+)"`)
	relIDRe     = regexp.MustCompile(`Id="rId(\d+)"`)
)

// analyzeSheetForMerge 扫描 sheet xml 找：
//   - lastRow: 最后一个 <row r="N"> 的 N
//   - headerCols: row 1 的 cell 数
//   - colStyles: row 2 每列的 s 属性（0-based 列索引 -> styleID）
//
// 这里用相对宽松的正则，对常见 xlsx 已经够用。
func analyzeSheetForMerge(data []byte) (lastRow, headerCols int, colStyles map[int]string, err error) {
	colStyles = map[int]string{}

	// lastRow
	rows := rowOpenRe.FindAllSubmatch(data, -1)
	if len(rows) == 0 {
		return 0, 0, colStyles, core.New("MERGE_NO_ROWS", "tmp sheet 里没有任何 row")
	}
	for _, m := range rows {
		n, _ := strconv.Atoi(string(m[1]))
		if n > lastRow {
			lastRow = n
		}
	}

	// 提取 row 1 / row 2 的字节段
	row1, _ := extractRow(data, 1)
	row2, _ := extractRow(data, 2)
	if len(row1) == 0 {
		return 0, 0, colStyles, core.New("MERGE_NO_HEADER", "tmp sheet 里找不到 row 1（表头）")
	}

	// headerCols 用 row 1 cell 计数
	headerCols = countCells(row1)

	// row 2 的列样式
	if len(row2) > 0 {
		cells := allCellsRowN(row2, 2)
		for col, styleID := range cells {
			colStyles[col] = styleID
		}
	}

	return lastRow, headerCols, colStyles, nil
}

// extractRow 从 sheet xml 字节里提取指定 1-based 行号的 <row ...>...</row> 字节。
func extractRow(data []byte, rowNum int) ([]byte, error) {
	pat := regexp.MustCompile(fmt.Sprintf(`(?s)<row r="%d"[^>]*>.*?</row>`, rowNum))
	m := pat.Find(data)
	if m == nil {
		return nil, core.New("ROW_NOT_FOUND", fmt.Sprintf("找不到 row %d", rowNum))
	}
	return m, nil
}

// countCells 数一行里的 <c> 节点数（含自闭合）。
func countCells(row []byte) int {
	return bytes.Count(row, []byte("<c "))
}

// allCellsRowN 解析 row 字节，返回每个 cell 的 col 索引 (0-based) -> s 属性值
// （没 s 属性返回空字符串，调用方应跳过或用默认）。
// 用 xml decoder 严格解析，避免正则在自闭合 / 嵌套子元素上踩坑。
func allCellsRowN(row []byte, rowNum int) map[int]string {
	out := map[int]string{}
	dec := xml.NewDecoder(bytes.NewReader(row))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local != "c" {
			continue
		}
		var cellRef, styleID string
		for _, a := range se.Attr {
			if a.Name.Local == "r" {
				cellRef = a.Value
			}
			if a.Name.Local == "s" {
				styleID = a.Value
			}
		}
		col := colFromCellRef(cellRef)
		if col >= 0 {
			out[col] = styleID
		}
	}
	return out
}

// colFromCellRef "B2" -> 1（0-based 列索引）；"AB2" -> 27。失败返回 -1。
func colFromCellRef(ref string) int {
	letters := ""
	for i := 0; i < len(ref); i++ {
		c := ref[i]
		if c >= 'A' && c <= 'Z' {
			letters += string(c)
		} else {
			break
		}
	}
	if letters == "" {
		return -1
	}
	col := 0
	for i := 0; i < len(letters); i++ {
		col = col*26 + int(letters[i]-'A'+1)
	}
	return col - 1
}

// colLetters 0-based 列索引 -> "A"/"B"/.../"AA"。
func colLetters(col int) string {
	col++
	out := ""
	for col > 0 {
		col--
		out = string(rune('A'+col%26)) + out
		col /= 26
	}
	return out
}

// analyzeDrawingNextPicID 扫描 drawing xml，返回 max(cNvPr id) + 1。
func analyzeDrawingNextPicID(data []byte) int {
	maxID := 0
	for _, m := range picIDRe.FindAllSubmatch(data, -1) {
		n, _ := strconv.Atoi(string(m[1]))
		if n > maxID {
			maxID = n
		}
	}
	return maxID + 1
}

// analyzeRelsNextRID 扫描 rels xml，返回 max(rId N) + 1。
func analyzeRelsNextRID(data []byte) int {
	maxID := 0
	for _, m := range relIDRe.FindAllSubmatch(data, -1) {
		n, _ := strconv.Atoi(string(m[1]))
		if n > maxID {
			maxID = n
		}
	}
	return maxID + 1
}
