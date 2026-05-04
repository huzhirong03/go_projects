package excelio

// probe_sheet.go：一次 zip 流式扫描同时完成两件事：
//   (1) 判断 sheet 是否含任一 <f> 公式元素（B2 SheetHasFormulas 的职责）
//   (2) 收集所有显式 ht 属性的行高 map（B3 RowHeights 的职责）
//
// 为什么合并：两个函数原本各自 zip.OpenReader + xml.Decoder 扫同一份 sheetN.xml，
// fixture 01 合计 2.39s + 2.24s = 4.63s；实测磁盘 I/O + XML token 解析是绝对大头，
// 合并成一次扫描能把这 4.63s 压到 ~2.4s（ext extractor.processFile 调用点少一半）。
//
// 原有 SheetHasFormulas / RowHeights 仍保留作为独立 API（单元测试 + 未来只需要
// 其中一个结果的场景）；本函数负责 extractor 主路径的"两个都要"场景。
//
// 扫描策略：
//   - 一旦发现 <f> 元素就把 hasFormulas=true，但不提前退出 —— 还要继续收集后续行的 ht
//   - 遇到 <row r="N" ht="X"> 元素，把 N -> X 收进 map（过滤 15.0 / 0 保持与 r.RowHeight 语义一致）
//   - EOF 或解析错误都按"扫到哪算哪"处理，形式上失败返回 (false, nil, err)
//
// 结果回填两个缓存，后续任一独立方法命中 cache 直接返回，0 次磁盘 I/O。

import (
	"archive/zip"
	"encoding/xml"
	"strconv"

	"excel-master/internal/core"
)

// ProbeSheet 一次 zip 扫描同时返回 sheet 的公式存在性和自定义行高 map。
// 两个结果都会被写入 Reader 的独立 cache，后续 SheetHasFormulas / RowHeights
// 对同 sheet 的调用可以零 I/O 命中缓存。
//
// 失败时返回 (false, nil, err)；调用方通常选择按"有公式 + 无行高 map"保守回退，
// 保证公式/行高的业务行为仍然正确，只是性能降级。
func (r *Reader) ProbeSheet(sheet string) (hasFormulas bool, heightMap map[int]float64, err error) {
	if r == nil || r.path == "" {
		return false, nil, core.New("EXCEL_READ_FAILED", "Reader 未关联源文件路径")
	}

	// 两个 cache 都命中就直接组合返回，省掉 zip 开销。
	_, hasHF := r.formulaProbeCache[sheet]
	_, hasRH := r.rowHeightMapCache[sheet]
	if hasHF && hasRH {
		return r.formulaProbeCache[sheet], r.rowHeightMapCache[sheet], nil
	}

	// 初始化缓存字段（延迟到首次写入时分配）
	if r.formulaProbeCache == nil {
		r.formulaProbeCache = map[string]bool{}
	}
	if r.rowHeightMapCache == nil {
		r.rowHeightMapCache = map[string]map[int]float64{}
	}

	zr, zerr := zip.OpenReader(r.path)
	if zerr != nil {
		return false, nil, core.Wrap("ZIP_OPEN_FAILED", "打开 xlsx zip 失败", zerr)
	}
	defer zr.Close()

	sheetXMLPath, perr := findSheetXMLPath(&zr.Reader, sheet)
	if perr != nil {
		return false, nil, perr
	}

	var sheetFile *zip.File
	for _, f := range zr.File {
		if f.Name == sheetXMLPath {
			sheetFile = f
			break
		}
	}
	if sheetFile == nil {
		// 找不到 sheet XML：保守返回"有公式 + 空行高"走原路径，确保业务不丢数据
		r.formulaProbeCache[sheet] = true
		r.rowHeightMapCache[sheet] = map[int]float64{}
		return true, map[int]float64{}, nil
	}

	rc, oerr := sheetFile.Open()
	if oerr != nil {
		return false, nil, core.Wrap("ZIP_READ_FAILED", "打开 sheet XML 失败", oerr)
	}
	defer rc.Close()

	heights := map[int]float64{}
	dec := xml.NewDecoder(rc)
	for {
		tok, derr := dec.Token()
		if derr != nil {
			// EOF / 解析错误都当扫完处理——保留已收集的结果
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "f":
			// OOXML 公式 cell 形如 <c r="A1"><f>...</f><v>...</v></c>
			hasFormulas = true
		case "row":
			// <row r="N" ht="X" customHeight="1">
			var rowNum int
			var ht float64
			var hasHt bool
			for _, attr := range se.Attr {
				switch attr.Name.Local {
				case "r":
					if n, perr := strconv.Atoi(attr.Value); perr == nil {
						rowNum = n
					}
				case "ht":
					if h, perr := strconv.ParseFloat(attr.Value, 64); perr == nil {
						ht = h
						hasHt = true
					}
				}
			}
			// 与 r.RowHeight 的语义一致：默认 15.0 / 0 视为未自定义，不入 map
			if rowNum > 0 && hasHt && ht != 15.0 && ht != 0 {
				heights[rowNum] = ht
			}
		}
	}

	// 回填两个 cache
	r.formulaProbeCache[sheet] = hasFormulas
	r.rowHeightMapCache[sheet] = heights
	return hasFormulas, heights, nil
}
