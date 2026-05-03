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

// PictureIndexProgressFn 在构建图片索引时被周期性调用，让上层把进度同步给前端。
// done 是已扫描的图片单元格数，total 是该 Sheet 上图片单元格总数。
type PictureIndexProgressFn func(done, total int)

// BuildPictureIndex 扫描指定 Sheet，构建行级图片索引。
// 这是主动扫描操作，只在任务开始时构建一次，不要在循环里反复调用。
//
// 可选 progress 参数：传入回调即可周期性收到进度。一张大图 1MB 在 excelize 内部
// 全字节读到内存里，3000 张就是 3GB，对前端必须有进度反馈，否则 UI 看起来卡死。
func BuildPictureIndex(f *excelize.File, sheet string, progress ...PictureIndexProgressFn) (*PictureIndex, error) {
	cells, err := f.GetPictureCells(sheet)
	if err != nil {
		return nil, core.Wrap("IMAGE_INDEX_FAILED", "获取图片单元格列表失败: "+sheet, err)
	}
	var pfn PictureIndexProgressFn
	if len(progress) > 0 {
		pfn = progress[0]
	}
	total := len(cells)
	if pfn != nil {
		pfn(0, total)
	}
	idx := &PictureIndex{byRow: make(map[int][]CellPictures, total)}
	for i, cell := range cells {
		col, row, err := excelize.CellNameToCoordinates(cell)
		if err != nil {
			return nil, core.Wrap("IMAGE_INDEX_FAILED", "单元格名解析失败: "+cell, err)
		}
		pics, err := f.GetPictures(sheet, cell)
		if err != nil {
			return nil, core.Wrap("IMAGE_INDEX_FAILED", "读取图片失败: "+cell, err)
		}
		if len(pics) > 0 {
			idx.byRow[row] = append(idx.byRow[row], CellPictures{
				Row:      row,
				Col:      col,
				Pictures: pics,
			})
		}
		// 每 50 张发一次进度，频率太高会刷屏，太低又看起来卡死。
		if pfn != nil && ((i+1)%50 == 0 || i == total-1) {
			pfn(i+1, total)
		}
	}
	return idx, nil
}

// PictureCellRef 是图片单元格的轻量坐标（**不含图片字节**）。
// 用 CollectPictureCellsByRow 一次扫完，再按需 LoadPicturesForRows 按行加载。
type PictureCellRef struct {
	Row  int    // 1-based
	Col  int    // 1-based
	Cell string // A1-style，用于调 GetPictures
}

// PictureLoadProgressFn 每加载 N 个图片单元格回调一次。
type PictureLoadProgressFn func(done, total int)

// CollectPictureCellsByRow 一次 O(N) 拿该 Sheet 所有图片 cell 的坐标（仅元数据，不加载字节）。
// 按源行号分桶。用于"先扫描匹配、再按命中行按需加载图片"的懒加载模式，
// 配合 LoadPicturesForRows 把 BuildPictureIndex 的 O(N²) 降到 O(匹配行数 × N)。
func CollectPictureCellsByRow(f *excelize.File, sheet string) (map[int][]PictureCellRef, error) {
	cells, err := f.GetPictureCells(sheet)
	if err != nil {
		return nil, core.Wrap("IMAGE_INDEX_FAILED", "获取图片单元格列表失败: "+sheet, err)
	}
	out := make(map[int][]PictureCellRef, len(cells))
	for _, cell := range cells {
		col, row, err := excelize.CellNameToCoordinates(cell)
		if err != nil {
			return nil, core.Wrap("IMAGE_INDEX_FAILED", "单元格名解析失败: "+cell, err)
		}
		out[row] = append(out[row], PictureCellRef{Row: row, Col: col, Cell: cell})
	}
	return out, nil
}

// LoadPicturesForRows 按需加载指定行的图片字节。
// rowToRefs 来自 CollectPictureCellsByRow；rows 是需要加载图片的源行号列表（一般是命中行）。
// progress 可选，每完成若干个 cell 回调一次（done/total 按图片 cell 数）。
func LoadPicturesForRows(
	f *excelize.File,
	sheet string,
	rowToRefs map[int][]PictureCellRef,
	rows []int,
	progress PictureLoadProgressFn,
) (map[int][]CellPictures, error) {
	totalCells := 0
	for _, r := range rows {
		totalCells += len(rowToRefs[r])
	}
	out := make(map[int][]CellPictures, len(rows))
	if progress != nil {
		progress(0, totalCells)
	}
	done := 0
	for _, r := range rows {
		for _, ref := range rowToRefs[r] {
			pics, err := f.GetPictures(sheet, ref.Cell)
			if err != nil {
				return nil, core.Wrap("IMAGE_LOAD_FAILED", "读取图片失败: "+ref.Cell, err)
			}
			if len(pics) > 0 {
				out[r] = append(out[r], CellPictures{
					Row: ref.Row, Col: ref.Col, Pictures: pics,
				})
			}
			done++
			if progress != nil && (done%5 == 0 || done == totalCells) {
				progress(done, totalCells)
			}
		}
	}
	return out, nil
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
