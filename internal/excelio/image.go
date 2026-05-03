package excelio

import (
	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"

	// 注册常见图片格式的解码器。excelize.AddPictureFromBytes 内部调用 image.Decode
	// 获取图片尺寸，该函数需要运行时已注册对应格式的解码器，否则报 "image: unknown format"。
	// 通过 blank import 把解码器随 excelio 包一起带入最终二进制，避免各 main 包重复 import。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// PictureIndex 缓存某个 Sheet 所有图片与其所在行/列的映射，
// 方便在提取/拆分时按"原行号"快速查找并迁移到目标文件。
//
// 使用方式：
//
//	idx, _ := excelio.BuildPictureIndex(reader.File(), "Sheet1")
//	for _, cp := range idx.PicturesOnRow(5) {
//	    excelio.MigratePicture(dstFile, "Sheet1", newRow, cp)
//	}
type PictureIndex struct {
	byRow map[int][]CellPictures
}

// CellPictures 描述某个单元格上的所有图片（支持一格多图）。
type CellPictures struct {
	Row      int                // 1-based
	Col      int                // 1-based
	Pictures []excelize.Picture // 一个单元格可能包含多张图
}

// BuildPictureIndex 扫描指定 Sheet，构建行级图片索引。
// 这是主动扫描操作，只在任务开始时构建一次，不要在循环里反复调用。
func BuildPictureIndex(f *excelize.File, sheet string) (*PictureIndex, error) {
	cells, err := f.GetPictureCells(sheet)
	if err != nil {
		return nil, core.Wrap("IMAGE_INDEX_FAILED", "获取图片单元格列表失败: "+sheet, err)
	}
	idx := &PictureIndex{byRow: make(map[int][]CellPictures, len(cells))}
	for _, cell := range cells {
		col, row, err := excelize.CellNameToCoordinates(cell)
		if err != nil {
			return nil, core.Wrap("IMAGE_INDEX_FAILED", "单元格名解析失败: "+cell, err)
		}
		pics, err := f.GetPictures(sheet, cell)
		if err != nil {
			return nil, core.Wrap("IMAGE_INDEX_FAILED", "读取图片失败: "+cell, err)
		}
		if len(pics) == 0 {
			continue
		}
		idx.byRow[row] = append(idx.byRow[row], CellPictures{
			Row:      row,
			Col:      col,
			Pictures: pics,
		})
	}
	return idx, nil
}

// PicturesOnRow 返回指定源行上所有的图片分组。
// 如果该行没有图片，返回 nil。
func (p *PictureIndex) PicturesOnRow(row int) []CellPictures {
	if p == nil {
		return nil
	}
	return p.byRow[row]
}

// TotalPictures 返回索引里所有图片的总数（用于进度估算）。
func (p *PictureIndex) TotalPictures() int {
	if p == nil {
		return 0
	}
	total := 0
	for _, cps := range p.byRow {
		for _, cp := range cps {
			total += len(cp.Pictures)
		}
	}
	return total
}

// MigratePicture 把源文件一个单元格上的所有图片重新插入到目标文件指定 Sheet 的新行同列位置。
// dstRow 为 1-based 行号；Col 保持与 cp.Col 一致。
func MigratePicture(dst *excelize.File, dstSheet string, dstRow int, cp CellPictures) error {
	dstCell, err := excelize.CoordinatesToCellName(cp.Col, dstRow)
	if err != nil {
		return core.Wrap("IMAGE_MIGRATE_FAILED", "目标单元格名生成失败", err)
	}
	for i := range cp.Pictures {
		pic := cp.Pictures[i] // 复制一份，避免 AddPicture 修改底层
		if err := dst.AddPictureFromBytes(dstSheet, dstCell, &pic); err != nil {
			return core.Wrap("IMAGE_MIGRATE_FAILED", "插入图片失败: "+dstCell, err)
		}
	}
	return nil
}
