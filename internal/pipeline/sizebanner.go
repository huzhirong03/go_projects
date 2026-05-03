package pipeline

import (
	"fmt"
	"os"

	"excel-master/internal/core"
)

// 大文件警告阈值（单位：字节）。
//
// 设计原则：
//   - 阈值不要太低，避免给"几 MB 普通文件"也弹警告造成噪音
//   - 跨级别用日志级别区分（INFO 提示 / WARN 强调），让用户感知严重程度
//   - 不阻断业务，只是预先告知，避免学员以为程序卡死
const (
	sizeThresholdInfo = 50 * 1024 * 1024   // 50MB 起，开始给提示
	sizeThresholdWarn = 200 * 1024 * 1024  // 200MB 起，给警告
	sizeThresholdHuge = 1024 * 1024 * 1024 // 1GB 起，给强警告
)

// SizeBanner 在任务开始时发一条概览日志，让用户知道：
//
//	"我要处理多大数据，需要等多久"
//
// 调用方传"将要处理的文件路径列表"，通常是源文件列表。
// 任何 stat 失败的文件被忽略（不阻断业务）。
//
// 不发 Progress 事件，只发 Log。Log 会在前端进度面板的滚动列表里持续可见，
// 而 Progress.message 会被下一条 Progress 覆盖掉，不适合做"开场白"。
func SizeBanner(emitter core.EventEmitter, paths []string) {
	if emitter == nil || len(paths) == 0 {
		return
	}
	var totalBytes int64
	var counted int
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		totalBytes += fi.Size()
		counted++
	}
	if counted == 0 {
		return
	}

	level, msg := formatSizeBanner(counted, totalBytes)
	emitter.Log(level, msg)
}

// formatSizeBanner 把"文件数 + 总字节数"转成对用户友好的中文摘要。
// 拆出来纯函数方便单元测试。
func formatSizeBanner(fileCount int, totalBytes int64) (level, msg string) {
	sizeStr := humanizeBytes(totalBytes)
	prefix := "源文件大小 " + sizeStr
	if fileCount > 1 {
		prefix = fmt.Sprintf("处理 %d 个文件，共 %s", fileCount, sizeStr)
	}

	switch {
	case totalBytes >= sizeThresholdHuge:
		return core.LogWarn, fmt.Sprintf(
			"⚠️ %s（数据量较大，预计 %s，请保持窗口开着不要关闭）",
			prefix, estimateDuration(totalBytes))
	case totalBytes >= sizeThresholdWarn:
		return core.LogWarn, fmt.Sprintf(
			"%s（预计 %s，请耐心等待）",
			prefix, estimateDuration(totalBytes))
	case totalBytes >= sizeThresholdInfo:
		return core.LogInfo, fmt.Sprintf(
			"%s（预计 %s）",
			prefix, estimateDuration(totalBytes))
	default:
		return core.LogInfo, prefix
	}
}

// humanizeBytes 把字节数转成 "12 KB" / "3.5 MB" / "1.2 GB" 形式。
func humanizeBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.0f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.0f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// estimateDuration 按经验值粗略估时长。
// 经验：流式处理 xlsx 大约 50 MB/s（带图片解码 / zip 手术 / 写盘）。
// 不要太精确，用户只要"几秒/几分钟/十几分钟"的量级感知。
func estimateDuration(totalBytes int64) string {
	const bytesPerSec = 50 * 1024 * 1024 // 50 MB/s 经验值
	seconds := totalBytes / bytesPerSec
	if seconds < 1 {
		seconds = 1
	}
	switch {
	case seconds < 30:
		return "几十秒内完成"
	case seconds < 90:
		return fmt.Sprintf("约 %d 秒", seconds)
	case seconds < 600:
		return fmt.Sprintf("约 %d 分钟", (seconds+59)/60)
	default:
		min := (seconds + 59) / 60
		return fmt.Sprintf("约 %d-%d 分钟", min, min+min/4)
	}
}
