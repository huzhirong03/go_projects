package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"excel-master/internal/core"
)

// captureEmitter 记录所有 Log 调用，用于断言 banner 行为。
type captureEmitter struct {
	mu   sync.Mutex
	logs []logRecord
}

type logRecord struct {
	level string
	msg   string
}

func (c *captureEmitter) Progress(core.Progress) {}
func (c *captureEmitter) Log(level, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, logRecord{level, msg})
}
func (c *captureEmitter) Done(any)     {}
func (c *captureEmitter) Error(error)  {}
func (c *captureEmitter) lastLog() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.logs) == 0 {
		return "", ""
	}
	r := c.logs[len(c.logs)-1]
	return r.level, r.msg
}

// writeFile 在临时目录创建一个指定大小的文件用于 stat 测试。
func writeFile(t *testing.T, dir, name string, sizeBytes int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, sizeBytes), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

func TestFormatSizeBanner_SmallFile(t *testing.T) {
	level, msg := formatSizeBanner(1, 1024) // 1KB
	if level != core.LogInfo {
		t.Errorf("小文件应是 INFO，得到 %s", level)
	}
	if !strings.Contains(msg, "1 KB") {
		t.Errorf("应展示 KB 单位，得到：%s", msg)
	}
}

func TestFormatSizeBanner_InfoThreshold(t *testing.T) {
	// 80MB → 命中 INFO 阈值（>= 50MB）
	level, msg := formatSizeBanner(1, 80*1024*1024)
	if level != core.LogInfo {
		t.Errorf("80MB 应是 INFO，得到 %s", level)
	}
	if !strings.Contains(msg, "80 MB") {
		t.Errorf("应展示 80 MB，得到：%s", msg)
	}
	if !strings.Contains(msg, "预计") {
		t.Errorf("应包含预计耗时，得到：%s", msg)
	}
}

func TestFormatSizeBanner_WarnThreshold(t *testing.T) {
	// 500MB → 命中 WARN 阈值（>= 200MB）
	level, msg := formatSizeBanner(3, 500*1024*1024)
	if level != core.LogWarn {
		t.Errorf("500MB 应是 WARN，得到 %s", level)
	}
	if !strings.Contains(msg, "3 个文件") {
		t.Errorf("多文件应展示文件数：%s", msg)
	}
}

func TestFormatSizeBanner_HugeThreshold(t *testing.T) {
	// 2GB → 命中 HUGE 阈值（>= 1GB）
	level, msg := formatSizeBanner(1, 2*1024*1024*1024)
	if level != core.LogWarn {
		t.Errorf("2GB 应是 WARN，得到 %s", level)
	}
	if !strings.Contains(msg, "GB") {
		t.Errorf("应用 GB 单位：%s", msg)
	}
	if !strings.Contains(msg, "保持窗口") {
		t.Errorf("超大文件应有保持窗口提示：%s", msg)
	}
}

func TestSizeBanner_SkipsMissingFiles(t *testing.T) {
	tmp := t.TempDir()
	good := writeFile(t, tmp, "ok.xlsx", 1024)
	bad := filepath.Join(tmp, "no-such.xlsx")

	e := &captureEmitter{}
	SizeBanner(e, []string{good, bad})

	level, msg := e.lastLog()
	if level == "" {
		t.Fatalf("应该 emit 一条 log")
	}
	// stat 失败的文件被忽略，所以总大小只算 good 的 1KB
	if !strings.Contains(msg, "源文件大小") {
		t.Errorf("单文件应显示源文件大小（bad 被跳过）：%s", msg)
	}
}

func TestSizeBanner_NoEmitWhenAllMissing(t *testing.T) {
	e := &captureEmitter{}
	SizeBanner(e, []string{"/no/such/path1", "/no/such/path2"})

	if _, msg := e.lastLog(); msg != "" {
		t.Errorf("全部文件无效时不应 emit 任何 log，得到：%s", msg)
	}
}

func TestSizeBanner_NilEmitterIsSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil emitter 不应 panic：%v", r)
		}
	}()
	SizeBanner(nil, []string{"foo"})
}

func TestSizeBanner_EmptyPathsIsSafe(t *testing.T) {
	e := &captureEmitter{}
	SizeBanner(e, nil)
	if _, msg := e.lastLog(); msg != "" {
		t.Errorf("空路径不应 emit log，得到：%s", msg)
	}
}

func TestHumanizeBytes(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1 KB"},
		{1536, "2 KB"}, // round
		{2 * 1024 * 1024, "2 MB"},
		{int64(1.5 * 1024 * 1024 * 1024), "1.5 GB"},
	}
	for _, c := range cases {
		got := humanizeBytes(c.bytes)
		if got != c.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", c.bytes, got, c.want)
		}
	}
}
