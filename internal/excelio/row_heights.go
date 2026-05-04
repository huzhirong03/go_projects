package excelio

// row_heights.go：sheet 级批量预读所有自定义行高。
//
// 背景：extractor.processFile 在每个命中行上调 r.RowHeight → excelize.GetRowHeight。
// excelize 内部的 GetRowHeight 实现是 O(N) 的 linear scan sheet data；对 100k 行
// 的 fixture 01，每次调用平均 832µs，14286 个命中行累计 11.89 秒 ——
// 占 B2 优化后扫描阶段 16.5s 的 72%，是下一个需要拿掉的大头。
//
// 本文件的做法：
//  1. 在 processFile 扫描开始前调一次 Reader.RowHeights(sheet)；
//  2. 方法用 archive/zip 流式扫 sheetN.xml 里的 <row> 元素，抓 r="N" 和 ht="..." 属性，
//     构建 map[1-based row]height。这一次扫描对 6MB xlsx (65MB XML) 约 2 秒。
//  3. 命中行查 map：O(1) hash lookup 代替 O(N) linear scan。
//
// 净收益（fixture 01）：去掉 11.9s 的 GetRowHeight + 多出 2s 的 zip 扫描 ≈ 净省 10s。
//
// 注意：方法失败时返回 (nil, err)，调用方可选择忽略错误回退到 r.RowHeight 逐次查询
// （行为保持完全一致），性能差但业务零回归。

import (
	"archive/zip"
	"encoding/xml"
	"strconv"

	"excel-master/internal/core"
)

// RowHeights 返回 sheet 里所有显式设置了 ht 属性的行高。
// map key 为 1-based 行号，value 为行高（点）。
// 没有 ht 属性的行（默认高度）不会出现在 map 里，调用方查不到按默认处理。
//
// 语义与 r.RowHeight 保持一致：height == 15.0 或 height == 0 的行被视为"未自定义"，
// 故这些行也不放进 map。这样 extractor 里的行为对单行查询来说完全等价。
//
// 结果在 Reader 上缓存；同一 Reader 上同 sheet 只扫一次。
func (r *Reader) RowHeights(sheet string) (map[int]float64, error) {
	if r == nil || r.path == "" {
		return nil, core.New("EXCEL_READ_FAILED", "Reader 未关联源文件路径")
	}
	if r.rowHeightMapCache != nil {
		if m, ok := r.rowHeightMapCache[sheet]; ok {
			return m, nil
		}
	}
	if r.rowHeightMapCache == nil {
		r.rowHeightMapCache = map[string]map[int]float64{}
	}

	zr, err := zip.OpenReader(r.path)
	if err != nil {
		return nil, core.Wrap("ZIP_OPEN_FAILED", "打开 xlsx zip 失败", err)
	}
	defer zr.Close()

	sheetXMLPath, err := findSheetXMLPath(&zr.Reader, sheet)
	if err != nil {
		return nil, err
	}

	var sheetFile *zip.File
	for _, f := range zr.File {
		if f.Name == sheetXMLPath {
			sheetFile = f
			break
		}
	}
	if sheetFile == nil {
		return nil, core.New("SHEET_XML_MISSING", "zip 里找不到 sheet XML: "+sheetXMLPath)
	}

	rc, err := sheetFile.Open()
	if err != nil {
		return nil, core.Wrap("ZIP_READ_FAILED", "打开 sheet XML 失败", err)
	}
	defer rc.Close()

	m := map[int]float64{}
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// <row r="N" ht="..." customHeight="1">...</row>
		// 注意：只有 ht 属性出现才是"自定义行高"；customHeight 在 Excel 里更保守，
		// 但我们只关心 ht 本身，符合 excelize.GetRowHeight 返回值的一致性。
		if se.Name.Local != "row" {
			continue
		}
		var rowNum int
		var ht float64
		var hasHt bool
		for _, attr := range se.Attr {
			switch attr.Name.Local {
			case "r":
				rn, perr := strconv.Atoi(attr.Value)
				if perr == nil {
					rowNum = rn
				}
			case "ht":
				h, perr := strconv.ParseFloat(attr.Value, 64)
				if perr == nil {
					ht = h
					hasHt = true
				}
			}
		}
		if rowNum > 0 && hasHt && ht != 15.0 && ht != 0 {
			m[rowNum] = ht
		}
	}
	r.rowHeightMapCache[sheet] = m
	return m, nil
}
