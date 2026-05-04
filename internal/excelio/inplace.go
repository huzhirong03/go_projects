package excelio

// inplace.go: 支持"在同一个 xlsx 里克隆出一个新 Sheet + 图片跟随 + 原子替换源文件"
// 的底层能力，用于"批量提取单文件模式 / 单文件拆分"的 inplace 输出目标。

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"excel-master/internal/core"

	"github.com/xuri/excelize/v2"
)

// CopySheetWithin 在同一个 excelize.File 内把 fromSheet 的内容复制成 toSheet。
//
// excelize 原生 CopySheet 会带：单元格值、样式、合并单元格、行列宽高、数据验证、条件格式；
// **不会**带：图片、图表、透视表、表格 (ListObject) 等 drawing 对象。
// 本函数会在 CopySheet 之后手动复制图片到新 Sheet 的相同单元格上，保证"原汁原味"。
//
// 返回错误的情况：
//   - fromSheet 不存在
//   - toSheet 已存在（调用方需先做去重）
//   - CopySheet / 图片复制失败
func CopySheetWithin(f *excelize.File, fromSheet, toSheet string) error {
	if f == nil {
		return core.New("NIL_FILE", "excelize.File 为空")
	}
	if fromSheet == toSheet {
		return core.New("INVALID_SHEET_NAME", "源 Sheet 和目标 Sheet 不能同名")
	}
	fromIdx, err := f.GetSheetIndex(fromSheet)
	if err != nil || fromIdx < 0 {
		return core.New("SHEET_NOT_FOUND", "源 Sheet 不存在: "+fromSheet)
	}
	if idx, _ := f.GetSheetIndex(toSheet); idx >= 0 {
		return core.New("SHEET_EXISTS", "目标 Sheet 已存在: "+toSheet)
	}
	toIdx, err := f.NewSheet(toSheet)
	if err != nil {
		return core.Wrap("NEW_SHEET_FAILED", "创建目标 Sheet 失败: "+toSheet, err)
	}
	if err := f.CopySheet(fromIdx, toIdx); err != nil {
		return core.Wrap("COPY_SHEET_FAILED", "CopySheet 失败", err)
	}
	// 复制图片：原 Sheet 上每个带图 cell → 目标 Sheet 同 cell 再贴一次
	cells, err := f.GetPictureCells(fromSheet)
	if err != nil {
		return core.Wrap("GET_PIC_CELLS_FAILED", "读取图片单元格失败: "+fromSheet, err)
	}
	for _, cell := range cells {
		pics, err := f.GetPictures(fromSheet, cell)
		if err != nil {
			return core.Wrap("GET_PICTURES_FAILED", "读取图片失败: "+cell, err)
		}
		for i := range pics {
			pic := pics[i] // 值拷贝，避免 AddPicture 修改底层
			if err := f.AddPictureFromBytes(toSheet, cell, &pic); err != nil {
				return core.Wrap("ADD_PICTURE_FAILED", "插入图片失败: "+cell, err)
			}
		}
	}
	return nil
}

// UniqueSheetName 给 base 追加 _2 / _3 ... 直到在 f 中不冲突。
// base 本身不冲突就直接返回；base 超长会被 SanitizeSheetName 截断。
func UniqueSheetName(f *excelize.File, base string) string {
	base = SanitizeSheetName(base)
	if idx, _ := f.GetSheetIndex(base); idx < 0 {
		return base
	}
	// 逐个尝试 _2, _3, ... 注意 Excel Sheet 名上限 31 字符
	for i := 2; i < 10000; i++ {
		suffix := "_" + strconv.Itoa(i)
		// 保证拼接后仍 ≤ 31
		prefix := base
		if len(prefix)+len(suffix) > 31 {
			prefix = prefix[:31-len(suffix)]
		}
		candidate := prefix + suffix
		if idx, _ := f.GetSheetIndex(candidate); idx < 0 {
			return candidate
		}
	}
	// 极度退化：返回带 _999 的版本，调用方会得到冲突错误
	return base + "_overflow"
}

// SanitizeSheetName 把 Excel 不允许的字符替换成 _，并截断到 31 字符。
// Excel 禁用：\ / ? * [ ] ：  以及开头/结尾单引号。
func SanitizeSheetName(s string) string {
	if s == "" {
		return "Sheet"
	}
	runes := []rune(s)
	for i, r := range runes {
		switch r {
		case '\\', '/', '?', '*', '[', ']', ':':
			runes[i] = '_'
		}
	}
	// 去掉首尾单引号
	for len(runes) > 0 && runes[0] == '\'' {
		runes = runes[1:]
	}
	for len(runes) > 0 && runes[len(runes)-1] == '\'' {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		return "Sheet"
	}
	if len(runes) > 31 {
		runes = runes[:31]
	}
	return string(runes)
}

// AtomicReplace 用 tmp 替换 target：
//  1. target → target.old
//  2. tmp    → target
//  3. 删除 target.old
//
// 中途失败会尽力回滚：若第 2 步失败则把 target.old 改回 target。
// Windows 上 os.Rename 不能直接覆盖已存在文件，因此才走这条备份链。
//
// 调用方确保：
//   - target 和 tmp 在同一个卷（否则 Rename 退化为拷贝+删除，仍可工作但非原子）
//   - target 未被其他进程打开（Excel/WPS 会锁），否则 Rename 报错
func AtomicReplace(target, tmp string) error {
	if _, err := os.Stat(tmp); err != nil {
		return core.Wrap("TMP_NOT_FOUND", "临时文件不存在: "+tmp, err)
	}
	backup := target + ".old"
	_ = os.Remove(backup) // 清理遗留
	// 目标存在才备份（inplace 场景目标必然存在，但保底判断一下）
	targetExists := false
	if _, err := os.Stat(target); err == nil {
		targetExists = true
		if err := os.Rename(target, backup); err != nil {
			return core.Wrap("BACKUP_FAILED", "备份源文件失败: "+target, err)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		if targetExists {
			_ = os.Rename(backup, target) // 回滚
		}
		return core.Wrap("REPLACE_FAILED", "替换目标文件失败: "+target, err)
	}
	if targetExists {
		_ = os.Remove(backup)
	}
	return nil
}

// BackupCopy 把 src 复制成带时间戳同后缀的备份文件。用于 inplace 前用户勾选"自动备份"时。
//
// 命名规则：<dir>/<basename>_备份_<yyyyMMdd_HHmmss><ext>
//
//	例： G:/data/学生信息.xlsx → G:/data/学生信息_备份_20260505_063800.xlsx
//
// 设计考量：
//   - 保留原扩展名（.xlsx 还是 .xlsx）→ 双击可直接用 Excel 打开
//   - 多次 inplace 不互相覆盖（每次时间戳不同）→ 能保留历史作为"版本快照"
//   - 防同秒撞名（脚本连跑）：追加 _2 _3 ... 避让，走到 _99 仍冲突就复用最后一个
func BackupCopy(src string) (string, error) {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	ts := time.Now().Format("20060102_150405")

	dst := filepath.Join(dir, name+"_备份_"+ts+ext)
	// 同秒撞名避让
	for i := 2; i < 100; i++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			break
		}
		dst = filepath.Join(dir, name+"_备份_"+ts+"_"+strconv.Itoa(i)+ext)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", core.Wrap("BACKUP_MKDIR_FAILED", "创建备份目录失败", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return "", core.Wrap("BACKUP_OPEN_FAILED", "打开源文件失败: "+src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", core.Wrap("BACKUP_CREATE_FAILED", "创建备份文件失败: "+dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return "", core.Wrap("BACKUP_COPY_FAILED", "复制到备份文件失败", err)
	}
	return dst, nil
}
