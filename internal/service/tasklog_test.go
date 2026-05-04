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

// TestTaskLog_SizeLimit：单文件硬上限 10MB 触发后停止写新内容，文件末尾留 truncated 警告。
func TestTaskLog_SizeLimit(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	tl, err := OpenTaskLog("t-size-test", "extract")
	if err != nil {
		t.Logf("OpenTaskLog 警告: %v", err)
	}
	if tl.Path() == "" {
		t.Skip("没拿到日志路径，跳过（dev 环境可能无任何可写位置）")
	}

	// 写一行 ~10KB 的内容，重复 1100 次 = ~11MB，足以触发 10MB 上限
	bigMsg := strings.Repeat("x", 10*1024)
	for i := 0; i < 1100; i++ {
		tl.Write(core.LogInfo, bigMsg)
	}
	// 末尾再尝试写几条，应被 silence 丢弃
	tl.Write(core.LogInfo, "post-silence-1")
	tl.Write(core.LogInfo, "post-silence-2")
	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info, err := os.Stat(tl.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// 允许穿过阈值一行（checkSizeLimit 是写完才检查，最后一行可能 10KB）+
	// truncated 警告 + Close 末尾元信息，20KB 容差足够。
	maxAllowed := int64(maxBytesPerTaskLog) + 20*1024
	if info.Size() > maxAllowed {
		t.Errorf("日志超出硬上限：实际 %d 字节，预期 ≤ %d", info.Size(), maxAllowed)
	}

	// 验证文件含 truncated 警告，且不含 silence 后写的内容
	data, err := os.ReadFile(tl.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "truncated") {
		t.Errorf("日志末尾应有 truncated 警告，实际未找到")
	}
	if strings.Contains(content, "post-silence-1") || strings.Contains(content, "post-silence-2") {
		t.Errorf("silence 后的内容不应被写入")
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

// cleanupTestSetup 清空日志目录里残留的 task-*.log，确保测试隔离。
// 返回日志目录路径供后续造文件用。
func cleanupTestSetup(t *testing.T) string {
	t.Helper()
	dir, err := LogsDir()
	if err != nil {
		t.Skipf("LogsDir 不可用：%v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "task-") && strings.HasSuffix(name, ".log") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return dir
}

// writeTaskLogFile 在 dir 下造一个 task-*.log，指定大小和 mtime。
func writeTaskLogFile(t *testing.T, dir, name string, sizeBytes int64, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(dir, name)
	content := make([]byte, sizeBytes)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", name, err)
	}
	return p
}

// countTaskLogs 返回 dir 里 task-*.log 的当前数量。
func countTaskLogs(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "task-") && strings.HasSuffix(e.Name(), ".log") {
			n++
		}
	}
	return n
}

// TestCleanupOldTaskLogs_KeepsRecentN：规则①"最新 20 个保底"
// 造 25 个全 30 天前的老文件。按规则②理应全删，但规则①保底前 20 个最新的，
// 所以只删最老的 5 个。验证错误日志跨多天仍能查回。
func TestCleanupOldTaskLogs_KeepsRecentN(t *testing.T) {
	dir := cleanupTestSetup(t)
	defer cleanupTestSetup(t) // 测试结束再清一次，避免污染下个测试

	// 造 25 个 mtime 递减的文件，每个 1KB（远小于配额）
	now := time.Now()
	for i := 0; i < 25; i++ {
		mtime := now.Add(-time.Duration(30+i) * 24 * time.Hour) // 30、31、32...54 天前
		writeTaskLogFile(t, dir, formatLogName(i), 1024, mtime)
	}

	if got := countTaskLogs(t, dir); got != 25 {
		t.Fatalf("setup: 应有 25 个文件，实际 %d", got)
	}

	deleted, _ := CleanupOldTaskLogs()
	if deleted != 5 {
		t.Errorf("应删 5 个（25 个 - 20 个保底），实际 %d", deleted)
	}
	if got := countTaskLogs(t, dir); got != 20 {
		t.Errorf("清理后应留 20 个，实际 %d", got)
	}
}

// TestCleanupOldTaskLogs_RemoveOldNonProtected：规则②"保底外 3 天清"
// 造 22 个文件：20 个 1 小时前（保底）+ 1 个 2 天前（保底外但未过期）+ 1 个 4 天前（过期）。
// 只有最老的 1 个过期文件被删。
func TestCleanupOldTaskLogs_RemoveOldNonProtected(t *testing.T) {
	dir := cleanupTestSetup(t)
	defer cleanupTestSetup(t)

	now := time.Now()
	// 20 个最新文件（mtime 1 小时前）= 保底
	for i := 0; i < 20; i++ {
		writeTaskLogFile(t, dir, formatLogName(i), 1024, now.Add(-1*time.Hour))
	}
	// 1 个 2 天前（保底外但未过期，< 3 天）
	freshBorderline := writeTaskLogFile(t, dir, "task-borderline.log", 1024,
		now.Add(-2*24*time.Hour))
	// 1 个 4 天前（保底外且过期，> 3 天）→ 应被删
	veryOld := writeTaskLogFile(t, dir, "task-veryold.log", 1024,
		now.Add(-4*24*time.Hour))

	deleted, _ := CleanupOldTaskLogs()
	if deleted != 1 {
		t.Errorf("应恰好删 1 个过期文件，实际 %d", deleted)
	}
	if _, err := os.Stat(veryOld); !os.IsNotExist(err) {
		t.Errorf("4 天前的文件应被删，但仍存在")
	}
	if _, err := os.Stat(freshBorderline); err != nil {
		t.Errorf("2 天前的文件应保留（< 3 天），但被删了：%v", err)
	}
}

// TestCleanupOldTaskLogs_QuotaKicksIn：规则③"总配额 100MB 触发"
// 造 25 个 10MB 的全新文件（mtime 1 小时前，都不过期）。
// 规则①保底 20 个（200MB，虽然超配额也不动），规则③只能删 survivors 5 个。
// 验证规则①的优先级高于规则③，不会因配额误删保底日志。
func TestCleanupOldTaskLogs_QuotaKicksIn(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过配额测试（涉及 250MB 磁盘 I/O）")
	}
	dir := cleanupTestSetup(t)
	defer cleanupTestSetup(t)

	now := time.Now()
	const fileSize = 10 << 20 // 10MB/文件
	// 25 个都是 1 小时前（全不过期），前 20 个更新，后 5 个稍老
	for i := 0; i < 25; i++ {
		mtime := now.Add(-time.Duration(i+1) * time.Minute)
		writeTaskLogFile(t, dir, formatLogName(i), fileSize, mtime)
	}

	deleted, freed := CleanupOldTaskLogs()
	if deleted != 5 {
		t.Errorf("配额触发应删 5 个 survivors（保底 20 个不动），实际 %d", deleted)
	}
	expectedFreed := int64(5 * fileSize)
	if freed != expectedFreed {
		t.Errorf("freed 应 = %d，实际 %d", expectedFreed, freed)
	}
	if got := countTaskLogs(t, dir); got != 20 {
		t.Errorf("清理后应留 20 个保底，实际 %d", got)
	}
}

// TestCleanupOldTaskLogs_Idempotent：连续调 2 次不会出错，不会误删文件。
func TestCleanupOldTaskLogs_Idempotent(t *testing.T) {
	dir := cleanupTestSetup(t)
	defer cleanupTestSetup(t)

	now := time.Now()
	for i := 0; i < 5; i++ {
		writeTaskLogFile(t, dir, formatLogName(i), 1024, now.Add(-1*time.Hour))
	}
	CleanupOldTaskLogs()
	before := countTaskLogs(t, dir)
	deleted, _ := CleanupOldTaskLogs()
	after := countTaskLogs(t, dir)
	if deleted != 0 {
		t.Errorf("第二次调用应 0 删除，实际 %d", deleted)
	}
	if before != after {
		t.Errorf("幂等性被破坏：before=%d after=%d", before, after)
	}
}

// formatLogName 造规则化的文件名。命名保持 task-*.log 前缀+后缀便于被 cleanup 识别。
func formatLogName(i int) string {
	return "task-test-" + time.Now().Format("20060102") + "-" +
		"fix" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + ".log"
}
