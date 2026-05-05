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
	"strings"

	"excel-master/internal/core"
)

// ProbeSheet 一次 zip 扫描同时返回 sheet 的公式存在性和自定义行高 map。
// 两个结果都会被写入 Reader 的独立 cache，后续 SheetHasFormulas / RowHeights
// 对同 sheet 的调用可以零 I/O 命中缓存。
//
// 同一次扫描还会**顺手**收集"有 <f> 无 <v>"的 cell 信息并写入 r.uncachedFormulasCache，
// 外层通过 r.UncachedFormulas(sheet) 读取。真实业务文件（>99% 公式有缓存）该 map
// 为空，extractor 据此零开销跳过回退求值；fixture 04 / 未保存的文件该 map 非空，
// extractor 只对其中的 cell 调 CalcCellValue。本函数签名不变，保持向后兼容。
//
// 失败时返回 (false, nil, err)；调用方通常选择按"有公式 + 无行高 map"保守回退，
// 保证公式/行高的业务行为仍然正确，只是性能降级。
func (r *Reader) ProbeSheet(sheet string) (hasFormulas bool, heightMap map[int]float64, err error) {
	if r == nil || r.path == "" {
		return false, nil, core.New("EXCEL_READ_FAILED", "Reader 未关联源文件路径")
	}

	// 三个 cache 都命中就直接组合返回，省掉 zip 开销。
	_, hasHF := r.formulaProbeCache[sheet]
	_, hasRH := r.rowHeightMapCache[sheet]
	_, hasUC := r.uncachedFormulasCache[sheet]
	if hasHF && hasRH && hasUC {
		return r.formulaProbeCache[sheet], r.rowHeightMapCache[sheet], nil
	}

	// 初始化缓存字段（延迟到首次写入时分配）
	if r.formulaProbeCache == nil {
		r.formulaProbeCache = map[string]bool{}
	}
	if r.rowHeightMapCache == nil {
		r.rowHeightMapCache = map[string]map[int]float64{}
	}
	if r.uncachedFormulasCache == nil {
		r.uncachedFormulasCache = map[string]map[string]string{}
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
		// 找不到 sheet XML：保守返回"有公式 + 空行高 + 空 uncached"走原路径
		r.formulaProbeCache[sheet] = true
		r.rowHeightMapCache[sheet] = map[int]float64{}
		r.uncachedFormulasCache[sheet] = map[string]string{}
		return true, map[int]float64{}, nil
	}

	rc, oerr := sheetFile.Open()
	if oerr != nil {
		return false, nil, core.Wrap("ZIP_READ_FAILED", "打开 sheet XML 失败", oerr)
	}
	defer rc.Close()

	heights := map[int]float64{}
	uncached := map[string]string{}

	// <c> cell 级状态：
	//   curCellRef：当前 cell 的 ref (形如 "K2")，空串表示当前不在 cell 内
	//   seenF / seenV：当前 cell 内是否已经见过 <f> / <v> StartElement
	//   curFormula：当前 cell 的公式文本（<f> 内的 CharData，供回退求值使用）
	//   inFormula：当前 xml.Decoder 的游标是否在 <f>...</f> 内（用于捕获 CharData）
	var curCellRef string
	var seenF, seenV, inFormula bool
	var curFormula strings.Builder

	dec := xml.NewDecoder(rc)
	for {
		tok, derr := dec.Token()
		if derr != nil {
			// EOF / 解析错误都当扫完处理——保留已收集的结果
			break
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "c":
				// 进入新 cell：重置所有 cell 级状态，提取 r 属性
				curCellRef = ""
				seenF = false
				seenV = false
				inFormula = false
				curFormula.Reset()
				for _, attr := range el.Attr {
					if attr.Name.Local == "r" {
						curCellRef = attr.Value
						break
					}
				}
			case "f":
				// OOXML 公式 cell 形如 <c r="A1"><f>...</f><v>...</v></c>
				hasFormulas = true
				seenF = true
				inFormula = true
				curFormula.Reset()
			case "v":
				seenV = true
			case "row":
				// <row r="N" ht="X" customHeight="1">
				var rowNum int
				var ht float64
				var hasHt bool
				for _, attr := range el.Attr {
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
		case xml.CharData:
			// 只有 <f>...</f> 内部的文本才收进 curFormula
			if inFormula {
				curFormula.Write(el)
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "f":
				inFormula = false
			case "c":
				// cell 结束：如果见过 <f> 但没见过 <v>，记入 uncached
				if seenF && !seenV && curCellRef != "" {
					uncached[curCellRef] = curFormula.String()
				}
			}
		}
	}

	// 回填三个 cache
	r.formulaProbeCache[sheet] = hasFormulas
	r.rowHeightMapCache[sheet] = heights
	r.uncachedFormulasCache[sheet] = uncached
	return hasFormulas, heights, nil
}
