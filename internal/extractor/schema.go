package extractor

import (
	"strings"

	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"
)

// excelizeFormulaCell 构造 excelize.Cell，带公式和缓存值。
// outputStream 在写入时会把 Formula 按"同行偏移"重写到目标行。
func excelizeFormulaCell(formula, cachedValue string) excelize.Cell {
	return excelize.Cell{Formula: formula, Value: cachedValue}
}

// FileSchema 描述某个源文件在统一列上的映射关系。
type FileSchema struct {
	File      FileInfo
	ColumnMap []int // 长度 == len(UnifiedSchema.Columns)；值为源文件 0-based 列号，-1 表示缺失
}

// UnifiedSchema 是整个任务的"统一列表头"。
// 构造策略：
//   - HeaderRow > 0：按"首次出现的顺序"合并各文件表头形成 union。
//   - HeaderRow == 0：列名为 "列1" "列2" ... 宽度取所有文件最大列宽。
type UnifiedSchema struct {
	Columns             []string
	Files               []FileSchema
	UnifiedColumnWidths map[int]float64 // 1-based 统一列号 -> 列宽（best effort，取自 Files[0]）
}

// BuildSchema 根据扫描结果构造统一表头。
func BuildSchema(files []FileInfo, headerRow int) (*UnifiedSchema, error) {
	if len(files) == 0 {
		return nil, core.New("NO_FILES", "没有可处理的 Excel 文件")
	}
	if headerRow <= 0 {
		return buildSchemaNoHeader(files), nil
	}

	// 以列名合并 union。
	var unified []string
	seen := make(map[string]int) // 列名 -> 在 unified 中的索引
	for _, f := range files {
		for _, h := range f.Headers {
			key := normalizeHeader(h)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = len(unified)
			unified = append(unified, h)
		}
	}
	if len(unified) == 0 {
		return nil, core.New("EMPTY_HEADERS", "所有文件的表头都是空的")
	}

	schemas := make([]FileSchema, 0, len(files))
	for _, f := range files {
		m := make([]int, len(unified))
		for i := range m {
			m[i] = -1
		}
		for colIdx, h := range f.Headers {
			key := normalizeHeader(h)
			if uIdx, ok := seen[key]; ok {
				m[uIdx] = colIdx
			}
		}
		schemas = append(schemas, FileSchema{File: f, ColumnMap: m})
	}
	return &UnifiedSchema{
		Columns:             unified,
		Files:               schemas,
		UnifiedColumnWidths: deriveUnifiedWidths(schemas, len(unified)),
	}, nil
}

// deriveUnifiedWidths 把第一个有列宽信息的文件的源列宽，按 ColumnMap 投射到统一列。
// 多文件时只参考"首个有数据的文件"，避免不同源文件列宽冲突。
func deriveUnifiedWidths(schemas []FileSchema, unifiedWidth int) map[int]float64 {
	if len(schemas) == 0 {
		return nil
	}
	for _, fs := range schemas {
		if len(fs.File.ColumnWidths) == 0 {
			continue
		}
		out := map[int]float64{}
		for u := 0; u < unifiedWidth && u < len(fs.ColumnMap); u++ {
			srcCol0 := fs.ColumnMap[u]
			if srcCol0 < 0 {
				continue
			}
			if w, ok := fs.File.ColumnWidths[srcCol0+1]; ok {
				out[u+1] = w
			}
		}
		return out
	}
	return nil
}

// AlignRowWithFormulas 把源行 cells 重排成统一列顺序。
// 当 formulas[c] 非空时，对应统一列输出 excelize.Cell{Formula: f, Value: cell}，
// 让下游 outputStream 在写入时按 dstRow 重写公式（同行偏移）。
// 没传 formulas 或 formulas[c] 为空时退化为字符串值，与 AlignRow 等价。
func (fs *FileSchema) AlignRowWithFormulas(cells, formulas []string, unifiedWidth int) []any {
	out := make([]any, unifiedWidth)
	for u := 0; u < unifiedWidth && u < len(fs.ColumnMap); u++ {
		c := fs.ColumnMap[u]
		if c < 0 || c >= len(cells) {
			out[u] = ""
			continue
		}
		if c < len(formulas) && formulas[c] != "" {
			out[u] = excelizeFormulaCell(formulas[c], cells[c])
			continue
		}
		out[u] = cells[c]
	}
	return out
}

func buildSchemaNoHeader(files []FileInfo) *UnifiedSchema {
	// 无表头模式：取所有文件第一行列数的最大值作为宽度。
	// 注：本函数不读数据行；此处无法预知宽度。
	// 折中方案：先假定 64 列上限，实际运行时若某文件更宽会被截断。
	// V1.0 用户场景很少无表头，这是可接受的简化。
	const fallbackWidth = 64
	cols := make([]string, fallbackWidth)
	for i := 0; i < fallbackWidth; i++ {
		cols[i] = "列" + itoaFast(i+1)
	}
	schemas := make([]FileSchema, 0, len(files))
	for _, f := range files {
		m := make([]int, fallbackWidth)
		for i := range m {
			m[i] = i
		}
		schemas = append(schemas, FileSchema{File: f, ColumnMap: m})
	}
	return &UnifiedSchema{Columns: cols, Files: schemas}
}

// ResolveSearchColumns 将用户指定的列名（task.SearchColumns）翻译为统一列索引。
// 返回 nil 表示"全列搜索"（与 SearchAllCols=true 或 SearchColumns 为空等价）。
func (s *UnifiedSchema) ResolveSearchColumns(names []string) []int {
	if len(names) == 0 {
		return nil
	}
	idx := make(map[string]int, len(s.Columns))
	for i, c := range s.Columns {
		idx[normalizeHeader(c)] = i
	}
	var out []int
	for _, n := range names {
		if i, ok := idx[normalizeHeader(n)]; ok {
			out = append(out, i)
		}
	}
	return out
}

// FileSearchColumns 把统一列索引翻译回某个源文件的行内列索引。
// unifiedIdx 里为 nil 时返回 nil（全列），否则映射非 -1 的项。
func (fs *FileSchema) FileSearchColumns(unifiedIdx []int) []int {
	if unifiedIdx == nil {
		return nil // 全列
	}
	out := make([]int, 0, len(unifiedIdx))
	for _, u := range unifiedIdx {
		if u < 0 || u >= len(fs.ColumnMap) {
			continue
		}
		if c := fs.ColumnMap[u]; c >= 0 {
			out = append(out, c)
		}
	}
	return out
}

// AlignRow 把源文件一行的 cells 重排成统一列顺序。
// 缺失列填空串。
func (fs *FileSchema) AlignRow(cells []string, unifiedWidth int) []any {
	out := make([]any, unifiedWidth)
	for u := 0; u < unifiedWidth && u < len(fs.ColumnMap); u++ {
		c := fs.ColumnMap[u]
		if c < 0 || c >= len(cells) {
			out[u] = ""
			continue
		}
		out[u] = cells[c]
	}
	return out
}

// UnifiedColFromSource 把源文件 0-based 列号翻译为统一列 0-based 索引。
// 找不到返回 -1。图片迁移时用。
func (fs *FileSchema) UnifiedColFromSource(srcCol0Based int) int {
	for u, c := range fs.ColumnMap {
		if c == srcCol0Based {
			return u
		}
	}
	return -1
}

func normalizeHeader(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func itoaFast(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
