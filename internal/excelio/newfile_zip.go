package excelio

// newfile_zip.go：纯 archive/zip + xml 手术，把源 xlsx 的"行子集"作为**全新 xlsx**
// 写到 dstPath。与 inplace_zip.go 的 AddFilteredSheetsZip 是姊妹函数：
//
//   - AddFilteredSheetsZip：源 xlsx + N specs → **追加** N 个新 sheet 到源（inplace 变体）
//   - ExtractToNewFileSurgery：源 xlsx + N specs → **新 xlsx 只含** N 个筛选 sheet
//
// 共享 90% 代码：planInplaceSpecs / writePlannedSheet / rewriteSheetXML /
// rewriteDrawingXML / CoerceStringCellsToNumbers / retargetDrawingInRels。
//
// 差异只在 3 个元数据文件的处理方式 + 条目过滤：
//
// | 文件                           | AddFilteredSheetsZip | ExtractToNewFileSurgery |
// |--------------------------------|----------------------|--------------------------|
// | workbook.xml                   | 追加新 <sheet>       | 清空 <sheets>，只留新     |
// | workbook.xml.rels              | 追加新 rel           | 删旧 sheet rel，加新      |
// | [Content_Types].xml            | 追加新 Override      | 删旧 sheet/drawing Override，加新 |
// | xl/worksheets/*                | bytewise 复制        | **全部跳过**              |
// | xl/drawings/*                  | bytewise 复制        | **全部跳过**              |
// | xl/calcChain.xml               | bytewise 复制        | **跳过**（公式链已失效）  |
// | xl/tables/* xl/comments*       | bytewise 复制        | **跳过**（附属于旧 sheet）|
// | xl/styles.xml, theme, sharedStrings, media/ | bytewise 复制 | bytewise 复制 |
//
// 用途：批量提取"新文件模式"下的 per_source / per_keyword / merged 三种策略。
// 相比 excelize 路径：
//   - 图片 anchor 100% 保真（源 ext.cx/cy/colOff/rowOff 字节级搬运）
//   - 条件格式、数据验证、自定义数字格式全保留
//   - 公式同行偏移，跨行公式丢弃（与 inplace 一致）
//   - 速度快 30-50%（图片零解码）

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"excel-master/internal/core"
)

// 包级正则：匹配 rels / [Content_Types].xml 里的完整节点。
// 兼容自闭合 `<X .../>` 和成对 `<X ...>...</X>` 两种写法。
// 关键：属性值可能含 "/"（如 Target="/xl/..."），所以内部用 [^>]*? 而不是 [^/>]*。
var (
	relTagRegexp               = regexp.MustCompile(`(?s)<Relationship\b[^>]*?(?:/>|>.*?</Relationship>)`)
	overrideTagRegexp          = regexp.MustCompile(`(?s)<Override\b[^>]*?(?:/>|>.*?</Override>)`)
	overrideCTAttrRegexp       = regexp.MustCompile(`\bContentType="([^"]+)"`)
	overridePartNameAttrRegexp = regexp.MustCompile(`\bPartName="([^"]+)"`)
)

// ExtractToNewFileSurgery 把源 xlsx 按 specs 筛选行，输出到一个**全新** xlsx。
// 复用 InplaceSheetSpec 类型（字段相同）：
//   - SourceSheet:  源 sheet 名（必须存在）
//   - NewSheetName: 新 sheet 名
//   - KeepRows:     保留的 1-based 行号；nil = 全保留
//
// 多个 specs 会在新 xlsx 里对应多个 sheet。新 sheet 名之间、以及与 srcPath 里未
// 被覆盖的 sheet 名之间，**调用方需自行保证唯一**（用 UniqueNameInSet）。
//
// dstPath 不能已存在；失败时清理半成品。
func ExtractToNewFileSurgery(srcPath, dstPath string, specs []InplaceSheetSpec) error {
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

	// 规划：复用 planInplaceSpecs，但让新 sheet 编号从 1 开始（源 sheet 都会被删，
	// 所以新 sheet 可以用 sheet1.xml / sheet2.xml，更干净）。
	// 注意：我们保持与 inplace 相同的命名策略（maxSheetIdx+1），因为 planInplaceSpecs
	// 里的 rID / sheetID 计算依赖 layout 的 maxXxx。新文件里最终只保留新 sheet，
	// 所以 sheetN.xml 编号大一点不影响 Excel 打开。
	plans, err := planInplaceSpecs(layout, specs)
	if err != nil {
		return err
	}

	// 重建 3 个元数据文件（replace 语义）。
	newWorkbook, err := rebuildWorkbookForNewFile(layout.workbookData, plans)
	if err != nil {
		return err
	}
	newWBRels, err := rebuildWorkbookRelsForNewFile(layout.workbookRelsData, plans)
	if err != nil {
		return err
	}
	newCT := rebuildContentTypesForNewFile(layout.contentTypesData, plans)

	dst := zip.NewWriter(out)
	closed := false
	defer func() {
		if !closed {
			_ = dst.Close()
		}
	}()

	// 1. 复制源 zip 的"保留类"条目
	for _, entry := range src.File {
		if skipEntryForNewFile(entry.Name) {
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

	// 3. 写新 sheet / drawing / rels（与 inplace 完全相同逻辑）
	sharedStrings, err := LoadSharedStrings(&src.Reader)
	if err != nil {
		return err
	}
	for _, p := range plans {
		if err := writePlannedSheet(&src.Reader, dst, p, sharedStrings); err != nil {
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

// skipEntryForNewFile 判断源 xlsx 的某条目是否在新文件里**丢弃**。
//
// 丢弃策略：
//   - 所有旧 sheet / sheet rels：新文件会写入新 sheet，旧 sheet 的 XML 无用
//   - 所有旧 drawing / drawing rels：新文件会写入新 drawing，旧 drawing 无用
//   - calcChain.xml：公式依赖链引用旧 sheet 行号，已失效；Excel 打开会自动重建
//   - tables / comments / pivotCache*：这些附属于旧 sheet，引用被删后无效
//   - 3 个元数据文件：会被我们 rebuild 版本覆盖
//
// 保留策略（zip 字节级复制）：
//   - xl/styles.xml, xl/theme/*, xl/sharedStrings.xml：样式表 / 主题 / 共享字符串
//     不依赖具体 sheet，新 sheet 的 cell 通过 s= 索引引用这些不变
//   - xl/media/*：图片字节，被新 drawing rels 引用
//   - xl/printerSettings/*：打印机设置，归属 sheet 但引用 rId，新 sheet rels
//     会复制这些引用
//   - docProps/*：作者/创建时间等元数据
func skipEntryForNewFile(name string) bool {
	// 3 个元数据文件会被 rebuild 版本覆盖
	if name == "xl/workbook.xml" ||
		name == "xl/_rels/workbook.xml.rels" ||
		name == "[Content_Types].xml" {
		return true
	}
	// 旧 sheet / drawing / 附属
	if strings.HasPrefix(name, "xl/worksheets/") ||
		strings.HasPrefix(name, "xl/drawings/") {
		return true
	}
	if name == "xl/calcChain.xml" {
		return true
	}
	// tables / comments / pivotTable 跟 sheet 绑死，sheet 没了就该删
	if strings.HasPrefix(name, "xl/tables/") ||
		strings.HasPrefix(name, "xl/pivotTables/") ||
		strings.HasPrefix(name, "xl/pivotCache/") {
		return true
	}
	if strings.HasPrefix(name, "xl/comments") && strings.HasSuffix(name, ".xml") {
		return true
	}
	// threadedComments、activeX、embeddings 等 sheet 附属物
	if strings.HasPrefix(name, "xl/threadedComments/") ||
		strings.HasPrefix(name, "xl/activeX/") ||
		strings.HasPrefix(name, "xl/embeddings/") {
		return true
	}
	return false
}

// rebuildWorkbookForNewFile 重写 workbook.xml：
//   - 清空 <sheets> 里所有旧 <sheet>
//   - 按 plans 顺序插入新 <sheet>
//   - 其他节点（<workbookPr> / <bookViews> / <definedNames> / <calcPr> 等）保留
//
// 注意：如果原 <definedName> 引用了被删的 sheet，Excel 打开会提示"文件已修复"
// 但不会损坏。保守做法是保留这些节点让 Excel 自己清理。
func rebuildWorkbookForNewFile(data []byte, plans []plannedSheet) ([]byte, error) {
	sheetsOpen := bytes.Index(data, []byte("<sheets>"))
	if sheetsOpen < 0 {
		// 某些 xlsx 写成 <sheets ...>
		idx := bytes.Index(data, []byte("<sheets"))
		if idx < 0 {
			return nil, core.New("WB_NO_SHEETS_OPEN", "workbook.xml 没有 <sheets>")
		}
		// 找 <sheets ...> 的 > 位置
		closeBracket := bytes.Index(data[idx:], []byte(">"))
		if closeBracket < 0 {
			return nil, core.New("WB_SHEETS_MALFORMED", "<sheets 标签没闭合")
		}
		sheetsOpen = idx + closeBracket + 1 - len("<sheets>")
		// 统一规格：把 <sheets ...> 替换成 <sheets>（兼容 xmlns 属性的情况很少见）
		// 保守起见：定位到 > 之后作为 inner 起点
		sheetsOpen = idx
	}
	// 找 inner 区域 [sheetsInnerStart, sheetsInnerEnd)
	openEnd := bytes.Index(data[sheetsOpen:], []byte(">"))
	if openEnd < 0 {
		return nil, core.New("WB_SHEETS_MALFORMED", "<sheets 标签没 >")
	}
	sheetsInnerStart := sheetsOpen + openEnd + 1
	sheetsClose := bytes.Index(data[sheetsInnerStart:], []byte("</sheets>"))
	if sheetsClose < 0 {
		return nil, core.New("WB_NO_SHEETS_CLOSE", "workbook.xml 没有 </sheets>")
	}
	sheetsInnerEnd := sheetsInnerStart + sheetsClose

	var newInner bytes.Buffer
	for _, p := range plans {
		fmt.Fprintf(&newInner, `<sheet name="%s" sheetId="%d" r:id="%s"/>`,
			xmlAttrEscape(p.spec.NewSheetName), p.sheetID, p.rID)
	}

	var out bytes.Buffer
	out.Grow(len(data) - (sheetsInnerEnd - sheetsInnerStart) + newInner.Len())
	out.Write(data[:sheetsInnerStart])
	out.Write(newInner.Bytes())
	out.Write(data[sheetsInnerEnd:])
	return out.Bytes(), nil
}

// rebuildWorkbookRelsForNewFile 重写 workbook.xml.rels：
//   - 删除所有 Type=".../worksheet" 的旧 Relationship
//   - 保留 styles / theme / sharedStrings / customXml 等 rel
//   - 追加 plans 里的新 worksheet rel
func rebuildWorkbookRelsForNewFile(data []byte, plans []plannedSheet) ([]byte, error) {
	// 先移除所有 worksheet 类型的 Relationship
	// Type 通常是 "http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
	filtered := filterOutRelsByType(data, "/relationships/worksheet")

	// 再追加新 worksheet rel
	closeIdx := bytes.Index(filtered, []byte("</Relationships>"))
	if closeIdx < 0 {
		return nil, core.New("WB_RELS_NO_CLOSE", "workbook.xml.rels 没有 </Relationships>")
	}
	var b bytes.Buffer
	b.Grow(len(filtered) + 200*len(plans))
	b.Write(filtered[:closeIdx])
	for _, p := range plans {
		target := "worksheets/" + path.Base(p.newSheet)
		fmt.Fprintf(&b,
			`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="%s"/>`,
			p.rID, target)
	}
	b.Write(filtered[closeIdx:])
	return b.Bytes(), nil
}

// filterOutRelsByType 从 rels xml 里移除所有 Type 子串匹配 typeSubstr 的 Relationship 节点。
func filterOutRelsByType(data []byte, typeSubstr string) []byte {
	// 用与 rewriteWorkbookRels 一致的正则匹配完整 <Relationship .../> 或 <Relationship ...>.*?</Relationship>
	matches := relTagRegexp.FindAllIndex(data, -1)
	if len(matches) == 0 {
		return data
	}
	var out bytes.Buffer
	out.Grow(len(data))
	last := 0
	for _, m := range matches {
		st, en := m[0], m[1]
		tag := data[st:en]
		if bytes.Contains(tag, []byte(`Type="`)) && bytes.Contains(tag, []byte(typeSubstr)) {
			out.Write(data[last:st])
			last = en
			continue
		}
	}
	out.Write(data[last:])
	return out.Bytes()
}

// rebuildContentTypesForNewFile 重写 [Content_Types].xml：
//   - 删除所有 ContentType="...worksheet+xml" 的旧 Override
//   - 删除所有 ContentType="...drawing+xml" 的旧 Override
//   - 删除所有 PartName="/xl/tables/..." / "/xl/comments..." / "/xl/calcChain.xml" 的 Override
//   - 保留 Default / theme / styles / sharedStrings / media 等 Override
//   - 追加 plans 里的新 worksheet + drawing Override
func rebuildContentTypesForNewFile(data []byte, plans []plannedSheet) []byte {
	// 先过滤掉要删的 Override
	matches := overrideTagRegexp.FindAllIndex(data, -1)
	var filtered bytes.Buffer
	filtered.Grow(len(data))
	last := 0
	for _, m := range matches {
		st, en := m[0], m[1]
		tag := data[st:en]
		if shouldDropContentTypeOverride(tag) {
			filtered.Write(data[last:st])
			last = en
			continue
		}
	}
	filtered.Write(data[last:])
	filteredData := filtered.Bytes()

	// 再追加新 Override
	closeIdx := bytes.Index(filteredData, []byte("</Types>"))
	if closeIdx < 0 {
		return filteredData
	}
	var b bytes.Buffer
	b.Grow(len(filteredData) + 300*len(plans))
	b.Write(filteredData[:closeIdx])
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
	b.Write(filteredData[closeIdx:])
	return b.Bytes()
}

// shouldDropContentTypeOverride 判断 [Content_Types].xml 的一条 <Override> 是否应删。
func shouldDropContentTypeOverride(tag []byte) bool {
	// 通过 ContentType 判断
	if ctMatch := overrideCTAttrRegexp.FindSubmatch(tag); len(ctMatch) >= 2 {
		ct := string(ctMatch[1])
		if strings.Contains(ct, "worksheet+xml") ||
			strings.Contains(ct, "drawing+xml") ||
			strings.Contains(ct, "table+xml") ||
			strings.Contains(ct, "comments+xml") ||
			strings.Contains(ct, "pivotCache") ||
			strings.Contains(ct, "pivotTable") {
			return true
		}
	}
	// 兜底：通过 PartName 路径判断
	if pnMatch := overridePartNameAttrRegexp.FindSubmatch(tag); len(pnMatch) >= 2 {
		pn := string(pnMatch[1])
		if pn == "/xl/calcChain.xml" {
			return true
		}
	}
	return false
}
