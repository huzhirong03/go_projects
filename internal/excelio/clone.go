package excelio

import (
	"io"
	"os"
	"path/filepath"
	"sort"

	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"
)

// CloneFile 二进制级别复制 src 到 dst（xlsx 是 zip 结构，直接 io.Copy 即可）。
// 若 dst 已存在，返回 ErrOutputConflict。
// 自动创建 dst 所在目录。
func CloneFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return core.Wrap("OUTPUT_CONFLICT", "输出文件已存在: "+dst, core.ErrOutputConflict)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return core.Wrap("OUTPUT_MKDIR_FAILED", "创建输出目录失败", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return core.Wrap("SOURCE_OPEN_FAILED", "打开源文件失败: "+src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return core.Wrap("OUTPUT_CREATE_FAILED", "创建输出文件失败: "+dst, err)
	}
	// 注意：defer Close 顺序 + 错误传递：io.Copy 失败时也要关闭并删除半成品
	copyErr := func() error {
		defer out.Close()
		_, cerr := io.Copy(out, in)
		return cerr
	}()
	if copyErr != nil {
		_ = os.Remove(dst)
		return core.Wrap("FILE_COPY_FAILED", "复制文件内容失败", copyErr)
	}
	return nil
}

// KeepSheetsOnly 在已打开的 file 中只保留 keepSheets 指定的 Sheet，其它删除。
// 工作簿至少要留 1 个 Sheet；若 keepSheets 里没有任何一个存在于工作簿中，函数报错以避免误删所有。
// keepSheets 中不存在的名字被忽略；顺序无要求。
func KeepSheetsOnly(f *excelize.File, keepSheets []string) error {
	if f == nil {
		return core.New("NIL_FILE", "excelize.File 为空")
	}
	if len(keepSheets) == 0 {
		return core.New("NO_KEEP_SHEETS", "keepSheets 不能为空，否则会删光所有 Sheet")
	}
	keep := map[string]struct{}{}
	for _, s := range keepSheets {
		keep[s] = struct{}{}
	}
	all := f.GetSheetList()
	// 先确认至少有一个要保留的 Sheet 存在
	hasKept := false
	for _, s := range all {
		if _, ok := keep[s]; ok {
			hasKept = true
			break
		}
	}
	if !hasKept {
		return core.New("KEEP_SHEETS_NOT_FOUND", "keepSheets 指定的 Sheet 都不存在于工作簿")
	}
	// 激活第一个要保留的 Sheet（防止 DeleteSheet 删掉的是当前激活 Sheet）
	for _, s := range all {
		if _, ok := keep[s]; ok {
			idx, err := f.GetSheetIndex(s)
			if err == nil {
				f.SetActiveSheet(idx)
			}
			break
		}
	}
	for _, s := range all {
		if _, ok := keep[s]; ok {
			continue
		}
		if err := f.DeleteSheet(s); err != nil {
			return core.Wrap("DELETE_SHEET_FAILED", "删除 Sheet 失败: "+s, err)
		}
	}
	return nil
}

// FilterRowsInSheet 在已打开的 file 的指定 sheet 中，只保留 keepRows 指定的行（1-based），
// 其它行通过 RemoveRow 删除。倒序删除避免索引偏移。
//
// 重要：excelize 的 RemoveRow 只会把"被删行以下的图片"的锚点行号往上减 1，
// 但**不会删除"正好锚在被删行上的图片"**。所以我们必须在调 RemoveRow 之前，
// 先显式 DeletePicture 掉该行上的所有图片，否则它们会堆积到上一保留行上，
// 就像用户观察到的"补货建议图标不在正确的行"问题。
//
// 公式引用由 excelize 自动维护。合并单元格、样式、条件格式等也由 excelize 负责。
//
// 特殊情况：sheet 为空（没有任何行）时不做任何事，返回 nil。
func FilterRowsInSheet(f *excelize.File, sheet string, keepRows []int) error {
	if f == nil {
		return core.New("NIL_FILE", "excelize.File 为空")
	}
	// 获取最大行数：用 GetRows 虽然会构造切片，但 excelize OpenFile 已经全量加载了，开销可接受。
	rows, err := f.GetRows(sheet)
	if err != nil {
		return core.Wrap("EXCEL_READ_FAILED", "读取 sheet 行数失败: "+sheet, err)
	}
	maxRow := len(rows)
	if maxRow == 0 {
		return nil
	}
	keep := map[int]struct{}{}
	for _, r := range keepRows {
		keep[r] = struct{}{}
	}

	// 预建"行 -> 该行所有有图片的 cell 名"索引，供删行前清理图片使用。
	// GetPictureCells 一次返回整个 sheet 所有图片锚点 cell 名，比逐 cell 调 GetPictures 省一个数量级。
	picCellsByRow, err := buildPictureCellsByRow(f, sheet)
	if err != nil {
		return err
	}

	// 倒序遍历：删后行号会前移，从高往低删不会影响"下一个要检查的行"
	for r := maxRow; r >= 1; r-- {
		if _, ok := keep[r]; ok {
			continue
		}
		// 先删这一行上的图片，再删行
		for _, cell := range picCellsByRow[r] {
			if err := f.DeletePicture(sheet, cell); err != nil {
				return core.Wrap("DELETE_PICTURE_FAILED", "删除行上图片失败: "+cell, err)
			}
		}
		if err := f.RemoveRow(sheet, r); err != nil {
			return core.Wrap("REMOVE_ROW_FAILED", "删除行失败", err)
		}
	}
	return nil
}

// buildPictureCellsByRow 扫描 sheet，返回 map[行号] -> 该行所有有图片的 cell 名列表。
// sheet 中没有图片时返回空 map，不视作错误。
func buildPictureCellsByRow(f *excelize.File, sheet string) (map[int][]string, error) {
	cells, err := f.GetPictureCells(sheet)
	if err != nil {
		return nil, core.Wrap("GET_PICTURE_CELLS_FAILED", "获取图片单元格列表失败: "+sheet, err)
	}
	out := map[int][]string{}
	for _, cell := range cells {
		_, row, err := excelize.CellNameToCoordinates(cell)
		if err != nil {
			// 极端情况：cell 名无效，跳过而非整体失败
			continue
		}
		out[row] = append(out[row], cell)
	}
	return out, nil
}

// CloneFileAndFilterRows 高保真提取：复制源文件到 dst，然后只保留 keepRows 指定的行。
//
// 参数：
//   - srcPath / dstPath：源、目标路径
//   - sheet：要过滤行的 Sheet 名（其它 Sheet 不动）
//   - keepRows：要保留的 1-based 行号集合（重复/无序都可以）
//
// 内部会：
//  1. 二进制复制 src -> dst
//  2. excelize.OpenFile(dst)（全量加载，大文件慎用）
//  3. FilterRowsInSheet(sheet, keepRows)
//  4. Save()
//
// 因为使用文件级复制 + 基于原文件删除，所有样式/图片锚点/合并单元格/条件格式/数据验证/公式
// 都由 excelize 内部负责维护，达成"原汁原味"的输出。
func CloneFileAndFilterRows(srcPath, dstPath, sheet string, keepRows []int) error {
	if err := CloneFile(srcPath, dstPath); err != nil {
		return err
	}
	f, err := excelize.OpenFile(dstPath)
	if err != nil {
		// 已经复制成功但无法打开 -> 删除半成品
		_ = os.Remove(dstPath)
		return core.Wrap("EXCEL_OPEN_FAILED", "打开复制后的目标文件失败: "+dstPath, err)
	}
	if err := FilterRowsInSheet(f, sheet, keepRows); err != nil {
		_ = f.Close()
		_ = os.Remove(dstPath)
		return err
	}
	if err := f.Save(); err != nil {
		_ = f.Close()
		_ = os.Remove(dstPath)
		return core.Wrap("EXCEL_SAVE_FAILED", "保存目标文件失败: "+dstPath, err)
	}
	if err := f.Close(); err != nil {
		return core.Wrap("EXCEL_CLOSE_FAILED", "关闭目标文件失败", err)
	}
	return nil
}

// SortedUnique 把 []int 排序去重，用于调用 FilterRowsInSheet / CloneFileAndFilterRows
// 之前预处理 keepRows。也可以直接传未排序带重复的切片（FilterRowsInSheet 内部用 set 处理），
// 这个函数主要是给测试和调用方做可读性用。
func SortedUnique(ns []int) []int {
	if len(ns) == 0 {
		return nil
	}
	tmp := make([]int, len(ns))
	copy(tmp, ns)
	sort.Ints(tmp)
	out := tmp[:0]
	prev := -1
	for _, n := range tmp {
		if n != prev {
			out = append(out, n)
			prev = n
		}
	}
	return out
}
