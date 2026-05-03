package pipeline

import (
	"context"

	"excel-master/internal/core"
)

// CheckCancel 检查 context 是否已取消，若已取消返回 ErrCanceled，否则返回 nil。
// 所有长循环每处理 N 行都应调用一次，N 建议 1000。
func CheckCancel(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return core.ErrCanceled
	default:
		return nil
	}
}
