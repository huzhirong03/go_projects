package service

import (
	"fmt"
	"runtime/debug"

	"excel-master/internal/core"
)

// recoverToEmitter 在异步任务 goroutine 顶层 defer 调用，
// 把 panic 转成事件，避免整个进程被未捕获 panic 拖崩。
//
// 用法（service 层启动任务的 goroutine 标准头）：
//
//	go func() {
//	    defer s.unregister(taskID)
//	    defer cancel()
//	    defer recoverToEmitter(emitter)  // 必须在 unregister/cancel 之后注册，先执行
//	    ...
//	}()
//
// 注意 defer 执行顺序：后注册的先执行，所以 recover 必须**最后**注册，
// 这样 panic 会先被它捕获、转成 emitter.Error，然后 cancel/unregister 正常执行。
func recoverToEmitter(emitter core.EventEmitter) {
	r := recover()
	if r == nil {
		return
	}
	stack := string(debug.Stack())
	msg := fmt.Sprintf("内部错误（已恢复，进程未崩溃）: %v\n\n堆栈追踪：\n%s", r, stack)
	err := core.Wrap("INTERNAL_PANIC", msg, fmt.Errorf("%v", r))
	// emitter.Error 把错误推给前端 UI；进程继续存活，下次任务能正常跑。
	emitter.Error(err)
}
