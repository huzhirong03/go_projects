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

func scanFolderInteractive(ctx context.Context, folder string, headerRow int, allowSheets []string, emitter core.EventEmitter) ([]FileInfo, error) {
	if folder == "" {
		return nil, core.New("INVALID_FOLDER", "文件夹路径为空")
	}
	stat, err := os.Stat(folder)
	if err != nil {
		return nil, core.Wrap("INVALID_FOLDER", "无法访问文件夹: "+folder, err)
	}
	if !stat.IsDir() {
		return nil, core.New("INVALID_FOLDER", "路径不是文件夹: "+folder)
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, core.Wrap("INVALID_FOLDER", "读取文件夹失败", err)
	}
	allow := newSheetFilter(allowSheets)
	var units []FileInfo
	for _, e := range entries {
		if err := pipeline.CheckCancel(ctx); err != nil {
			return nil, err
		}
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "~$") || !strings.EqualFold(filepath.Ext(name), ".xlsx") {
			continue
		}
		full := filepath.Join(folder, name)
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
