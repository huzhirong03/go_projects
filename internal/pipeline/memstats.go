package pipeline

import (
	"fmt"
	"runtime"

	"excel-master/internal/core"
)

// LogMem 把当前进程的内存占用打到 [MEM] 日志里，用于性能调优时观测峰值。
//
// 字段含义：
//   - heap: 当前活跃的堆内存（HeapInuse），最反映"真实占用"
//   - sys:  Go 向 OS 申请的总内存（包括未归还），近似 RSS
//   - alloc-total: 进程启动以来累计分配过的字节（AllocTotal），反映 GC 压力
//
// 设计上不主动触发 GC（runtime.GC() 太贵），只读快照。
// emitter 为 nil 时静默跳过，方便测试桩。
func LogMem(emitter core.EventEmitter, label string) {
	if emitter == nil {
		return
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	emitter.Log(core.LogInfo, fmt.Sprintf("[MEM] %s heap=%dMB sys=%dMB alloc-total=%dMB gc=%d",
		label,
		m.HeapInuse>>20,
		m.Sys>>20,
		m.TotalAlloc>>20,
		m.NumGC,
	))
}
