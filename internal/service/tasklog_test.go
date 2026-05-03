package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"excel-master/internal/core"
)

// TestTaskLog_NilSafe：调用 nil 实例上的方法不应 panic。
func TestTaskLog_NilSafe(t *testing.T) {
	var tl *TaskLog
	tl.Write("info", "hi")
	tl.WriteProgress("scanning", 1, 10, "msg")
	if err := tl.Close(); err != nil {
		t.Errorf("nil Close 不应返回错误：%v", err)
	}
	if p := tl.Path(); p != "" {
		t.Errorf("nil Path 应返回空串：%s", p)
	}
}

// TestTaskLog_ZeroValueSafe：open 失败返回的零值 TaskLog 仍可调用所有方法。
func TestTaskLog_ZeroValueSafe(t *testing.T) {
	tl := &TaskLog{}
	tl.Write(core.LogInfo, "ghost write")
	tl.WriteProgress("foo", 0, 0, "ghost")
	_ = tl.Close()
}

// TestTaskLog_RealFile：完整链路：open → write → close → 读回内容。
func TestTaskLog_RealFile(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir()) // Windows 下让 UserCacheDir 落到 TempDir
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	tl, err := OpenTaskLog("t-test-001", "extract")
	if err != nil {
		// 可能因为 portable 路径写不到导致走 fallback；不致命，只验证仍能开
		t.Logf("OpenTaskLog 警告: %v（可能 fallback 失败，继续测试）", err)
	}
	if tl.Path() == "" {
		t.Skip("没拿到日志路径，跳过（dev 环境可能无任何可写位置）")
	}

	tl.Write(core.LogInfo, "start")
	tl.Write(core.LogWarn, "watch out")
	tl.Write(core.LogError, "boom\nwith\nnewlines")
	tl.WriteProgress("reading", 5, 10, "file.xlsx")
	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(tl.Path())
	if err != nil {
		t.Fatalf("读回日志文件: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"Task ID:", "t-test-001", "Task Kind:", "extract",
		"INFO ", "start",
		"WARN ", "watch out",
		"ERROR", "boom",
		"PROG", "reading", "5/10", "file.xlsx",
		"Ended At:",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("日志缺片段 %q\n----\n%s", want, content)
		}
	}
}

// TestTaskLog_DoubleCloseIsSafe：重复 Close 不应报错。
func TestTaskLog_DoubleCloseIsSafe(t *testing.T) {
	tl := &TaskLog{}
	if err := tl.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := tl.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

// TestSanitizeTaskID 校验非法 Windows 文件名字符被替换。
func TestSanitizeTaskID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"t-123-1", "t-123-1"},
		{"t/with\\slash:colon*star?qm\"quote<lt>gt|pipe", "t_with_slash_colon_star_qm_quote_lt_gt_pipe"},
		{"中文-OK", "中文-OK"},
	}
	for _, c := range cases {
		if got := sanitizeTaskID(c.in); got != c.want {
			t.Errorf("sanitizeTaskID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeLevel 校验级别字符串归一化。
func TestNormalizeLevel(t *testing.T) {
	cases := map[string]string{
		"info":  "INFO ",
		"warn":  "WARN ",
		"error": "ERROR",
		"debug": "debug", // 未识别级别原样保留（截断到 5 字符）
		"":      "",
	}
	for in, want := range cases {
		if got := normalizeLevel(in); got != want {
			t.Errorf("normalizeLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCleanupOldTaskLogs_DeletesExpired：旧文件被删，新文件保留。
func TestCleanupOldTaskLogs_DeletesExpired(t *testing.T) {
	dir, err := LogsDir()
	if err != nil {
		t.Skipf("LogsDir 不可用：%v", err)
	}
	// 造两个文件：一个老的（mtime 8 天前），一个新的（mtime 1 天前）
	old := filepath.Join(dir, "task-cleanup-old.log")
	fresh := filepath.Join(dir, "task-cleanup-fresh.log")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(fresh, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("write fresh: %v", err)
	}
	defer os.Remove(fresh) // 清理
	defer os.Remove(old)

	// 把 old 的 mtime 改成 8 天前
	past := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	deleted, _ := CleanupOldTaskLogs()
	if deleted < 1 {
		t.Errorf("应至少删 1 个过期文件，实际删了 %d", deleted)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old 文件未被删除")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh 文件被误删：%v", err)
	}
}
