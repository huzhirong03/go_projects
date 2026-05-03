package extractor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"excel-master/internal/core"
)

type fileOpenDecision int

const (
	fileOpenRetry fileOpenDecision = iota
	fileOpenSkip
	fileOpenCancel
	fileOpenAbort
)

func askFileOpenDecision(ctx context.Context, emitter core.EventEmitter, filePath string, err error) fileOpenDecision {
	if !isLikelyFileOccupied(err) {
		return fileOpenAbort
	}
	prompter, ok := emitter.(core.FileBlockedPrompter)
	if !ok {
		return fileOpenAbort
	}
	choice := prompter.PromptFileBlocked(ctx, core.FileBlockedRequest{
		Path:    filePath,
		Message: fmt.Sprintf("文件暂时无法读取，可能正在被 Excel/WPS 占用。请先保存并关闭该文件，然后选择重试。\n\n%s", err.Error()),
	})
	switch choice {
	case core.FileBlockedRetry:
		return fileOpenRetry
	case core.FileBlockedSkip:
		return fileOpenSkip
	default:
		return fileOpenCancel
	}
}

func askOfficeLockDecision(ctx context.Context, emitter core.EventEmitter, filePath string) fileOpenDecision {
	lockPath, ok := officeLockFile(filePath)
	if !ok {
		return fileOpenAbort
	}
	prompter, ok := emitter.(core.FileBlockedPrompter)
	if !ok {
		return fileOpenAbort
	}
	choice := prompter.PromptFileBlocked(ctx, core.FileBlockedRequest{
		Path: filePath,
		Message: fmt.Sprintf(
			"检测到 Office/WPS 锁文件，说明该 Excel 可能正在被打开。\n请先保存并关闭文件，然后选择重试。\n\n锁文件: %s",
			lockPath,
		),
	})
	switch choice {
	case core.FileBlockedRetry:
		return fileOpenRetry
	case core.FileBlockedSkip:
		return fileOpenSkip
	default:
		return fileOpenCancel
	}
}

func officeLockFile(filePath string) (string, bool) {
	base := filepath.Base(filePath)
	if strings.HasPrefix(base, "~$") {
		return "", false
	}
	dir := filepath.Dir(filePath)
	candidates := []string{filepath.Join(dir, "~$"+base)}
	if len([]rune(base)) > 2 {
		runes := []rune(base)
		candidates = append(candidates, filepath.Join(dir, "~$"+string(runes[2:])))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

func isLikelyFileOccupied(err error) bool {
	var appErr *core.AppError
	if !errors.As(err, &appErr) {
		return false
	}
	switch appErr.Code {
	case "EXCEL_OPEN_FAILED", "SRC_OPEN_FAILED", "MERGE_OPEN_SECONDARY_FAILED":
	default:
		return false
	}
	s := strings.ToLower(err.Error())
	needles := []string{
		"being used by another process",
		"used by another process",
		"access is denied",
		"permission denied",
		"cannot access the file",
		"另一个程序正在使用",
		"正由另一进程使用",
		"正在使用",
		"拒绝访问",
		"权限",
	}
	for _, needle := range needles {
		if strings.Contains(s, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
