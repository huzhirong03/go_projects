package service

import (
	"os"
	"path/filepath"
	"testing"
)

// helper：在临时目录写一个 config.json，返回路径
func writeJSON(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// TestMigrate_HappyPath：portable 不存在 + fallback 有合法 JSON → 应迁移过去
func TestMigrate_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	portableDir := filepath.Join(tmp, "portable")
	fallbackDir := filepath.Join(tmp, "fallback")
	_ = os.MkdirAll(portableDir, 0o755)

	portablePath := filepath.Join(portableDir, "config.json")
	fallbackPath := writeJSON(t, fallbackDir, "config.json", `{"foo":"bar"}`)

	migrateConfigFromFallbackOnce(portablePath, fallbackPath)

	// portable 应已存在，且内容跟 fallback 一致
	got, err := os.ReadFile(portablePath)
	if err != nil {
		t.Fatalf("portable 应被创建：%v", err)
	}
	if string(got) != `{"foo":"bar"}` {
		t.Errorf("内容不匹配：got %q", string(got))
	}
}

// TestMigrate_PortableExists：portable 已有 → 必须不覆盖（保护新版本配置）
func TestMigrate_PortableExists(t *testing.T) {
	tmp := t.TempDir()
	portablePath := writeJSON(t, tmp, "portable.json", `{"new":"version"}`)
	fallbackPath := writeJSON(t, tmp, "fallback.json", `{"old":"version"}`)

	migrateConfigFromFallbackOnce(portablePath, fallbackPath)

	// portable 内容应不变
	got, _ := os.ReadFile(portablePath)
	if string(got) != `{"new":"version"}` {
		t.Errorf("portable 不应被覆盖：got %q", string(got))
	}
}

// TestMigrate_NoFallback：fallback 不存在 → 不做任何事，portable 不应被创建
func TestMigrate_NoFallback(t *testing.T) {
	tmp := t.TempDir()
	portablePath := filepath.Join(tmp, "config.json")
	fallbackPath := filepath.Join(tmp, "no-such-file.json")

	migrateConfigFromFallbackOnce(portablePath, fallbackPath)

	if _, err := os.Stat(portablePath); !os.IsNotExist(err) {
		t.Errorf("portable 不应被创建（fallback 不存在时）")
	}
}

// TestMigrate_CorruptFallback：fallback 是损坏 JSON → 跳过迁移，避免污染
func TestMigrate_CorruptFallback(t *testing.T) {
	tmp := t.TempDir()
	portablePath := filepath.Join(tmp, "config.json")
	fallbackPath := writeJSON(t, tmp, "fallback.json", `{not valid json`)

	migrateConfigFromFallbackOnce(portablePath, fallbackPath)

	if _, err := os.Stat(portablePath); !os.IsNotExist(err) {
		t.Errorf("portable 不应被创建（fallback 是损坏 JSON 时）")
	}
}
