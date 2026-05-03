package excelio

import (
	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"
)

// StyleMigrator 在源文件和目标文件之间迁移单元格样式。
// 同一个源样式 ID 迁移一次即可，后续会复用缓存。
type StyleMigrator struct {
	src   *excelize.File
	dst   *excelize.File
	cache map[int]int // src styleID -> dst styleID
}

// NewStyleMigrator 构造迁移器。
func NewStyleMigrator(src, dst *excelize.File) *StyleMigrator {
	return &StyleMigrator{src: src, dst: dst, cache: make(map[int]int, 64)}
}

// Translate 把源文件的单元格样式 ID 转换到目标文件对应的 ID。
// 若 srcStyleID == 0，返回 0（默认样式）。
func (m *StyleMigrator) Translate(srcStyleID int) (int, error) {
	if srcStyleID == 0 {
		return 0, nil
	}
	if v, ok := m.cache[srcStyleID]; ok {
		return v, nil
	}
	style, err := m.src.GetStyle(srcStyleID)
	if err != nil {
		return 0, core.Wrap("STYLE_MIGRATE_FAILED", "读取源样式失败", err)
	}
	newID, err := m.dst.NewStyle(style)
	if err != nil {
		return 0, core.Wrap("STYLE_MIGRATE_FAILED", "创建目标样式失败", err)
	}
	m.cache[srcStyleID] = newID
	return newID, nil
}

// CopyColumnWidths 将源 Sheet 的列宽复制到目标 Sheet。
// 按列遍历，只复制被用户显式设置过的列宽（excelize GetColWidth 对未设置列返回默认值，
// 这里仍然原样复制以保证视觉一致）。
func (m *StyleMigrator) CopyColumnWidths(srcSheet, dstSheet string, maxCol int) error {
	if maxCol <= 0 {
		return nil
	}
	for col := 1; col <= maxCol; col++ {
		name, err := excelize.ColumnNumberToName(col)
		if err != nil {
			return core.Wrap("STYLE_MIGRATE_FAILED", "列号转换失败", err)
		}
		w, err := m.src.GetColWidth(srcSheet, name)
		if err != nil {
			// 缺失视为使用默认，跳过。
			continue
		}
		if err := m.dst.SetColWidth(dstSheet, name, name, w); err != nil {
			return core.Wrap("STYLE_MIGRATE_FAILED", "设置目标列宽失败", err)
		}
	}
	return nil
}
