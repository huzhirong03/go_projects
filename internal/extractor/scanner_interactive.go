package extractor

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"excel-master/internal/core"
	"excel-master/internal/pipeline"
)

// scanFolderInteractive 扫描"源路径"：支持文件夹（批量）或单个 .xlsx 文件。
// 单文件输入时跳过遍历，直接对该文件走 probeFile/锁文件交互逻辑。
func scanFolderInteractive(ctx context.Context, folder string, headerRow int, allowSheets []string, emitter core.EventEmitter) ([]FileInfo, error) {
	if folder == "" {
		return nil, core.New("INVALID_FOLDER", "源路径为空")
	}
	stat, err := os.Stat(folder)
	if err != nil {
		return nil, core.Wrap("INVALID_FOLDER", "无法访问源路径: "+folder, err)
	}
	allow := newSheetFilter(allowSheets)
	// 单文件分支：构造一个只有 1 个虚拟"目录项"的循环，复用下面的 probe + 交互。
	type entryLike struct {
		isDir bool
		name  string
		full  string
	}
	var entries []entryLike
	if !stat.IsDir() {
		if !strings.EqualFold(filepath.Ext(folder), ".xlsx") {
			return nil, core.New("INVALID_FILE", "仅支持 .xlsx 文件: "+folder)
		}
		entries = []entryLike{{isDir: false, name: filepath.Base(folder), full: folder}}
	} else {
		raw, err := os.ReadDir(folder)
		if err != nil {
			return nil, core.Wrap("INVALID_FOLDER", "读取文件夹失败", err)
		}
		for _, e := range raw {
			entries = append(entries, entryLike{isDir: e.IsDir(), name: e.Name(), full: filepath.Join(folder, e.Name())})
		}
	}
	var units []FileInfo
	for _, e := range entries {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return nil, err
		}
		if e.isDir {
			continue
		}
		name := e.name
		if strings.HasPrefix(name, "~$") || !strings.EqualFold(filepath.Ext(name), ".xlsx") {
			continue
		}
		full := e.full
		for {
			skipFile := false
			switch askOfficeLockDecision(ctx, emitter, full) {
			case fileOpenRetry:
				continue
			case fileOpenSkip:
				emitter.Log(core.LogWarn, "已跳过正在打开的文件: "+full)
				skipFile = true
			case fileOpenCancel:
				return nil, core.ErrCanceled
			}
			if skipFile {
				break
			}
			fileUnits, err := probeFile(full, headerRow, allow)
			if err == nil {
				units = append(units, fileUnits...)
				break
			}
			switch askFileOpenDecision(ctx, emitter, full, err) {
			case fileOpenRetry:
				continue
			case fileOpenSkip:
				emitter.Log(core.LogWarn, "已跳过无法读取的文件: "+full)
				break
			case fileOpenAbort:
				return nil, err
			default:
				return nil, core.ErrCanceled
			}
			break
		}
	}
	sort.Slice(units, func(i, j int) bool {
		if units[i].Path != units[j].Path {
			return units[i].Path < units[j].Path
		}
		return units[i].SheetName < units[j].SheetName
	})
	return units, nil
}
