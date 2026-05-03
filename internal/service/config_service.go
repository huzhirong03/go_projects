package service

import (
	"encoding/json"
	"os"
	"path/filepath"

	"excel-master/internal/core"
)

// 配置文件位置（绿色优先 + fallback）：
//
//  1. 优先 exe 同目录的 config.json —— 真·绿色：U 盘/网盘搬走整个文件夹配置不丢，
//     卸载/迁移时也不会留垃圾在 C 盘。
//  2. 如果 exe 同目录不可写（比如 exe 放在 Program Files、只读盘），
//     fallback 到 <UserConfigDir>/excel-master/config.json，保证功能不挂。
//
// Windows fallback: %APPDATA%\excel-master\config.json
// macOS   fallback: ~/Library/Application Support/excel-master/config.json
// Linux   fallback: ~/.config/excel-master/config.json
//
// 内容是前端任意 JSON。后端只做"合法性检查 + 落盘 + 读取"，
// 不解析具体字段（保持灵活，前端字段改动无需后端配合）。
const configFileName = "config.json"

// portableConfigPath 返回 exe 同目录的 config.json 路径；
// 拿不到 exe 路径时返回空串。
func portableConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// EvalSymlinks 处理 Windows 快捷方式 / Unix 软链
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Join(filepath.Dir(exe), configFileName)
}

// fallbackConfigPath 返回 UserConfigDir 下的配置路径。
func fallbackConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", core.Wrap("CONFIG_DIR_FAILED", "无法定位用户配置目录", err)
	}
	return filepath.Join(dir, "excel-master", configFileName), nil
}

// dirIsWritable 探测目录是否可写：尝试创建一个小探针文件然后立刻删除。
// 这是 Windows 上判断"目录是否可写"最可靠的方法（os.Stat + Mode 不靠谱）。
func dirIsWritable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".excel-master-probe-*")
	if err != nil {
		return false
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	return true
}

// configFilePath 决定本次读写用哪个路径：
//   - 如果 exe 同目录的 config.json 已存在 → 用它（用户已绿色化）
//   - 否则探测 exe 同目录是否可写 → 可写就用它（首次运行落地为绿色）
//   - 都不行 → fallback 到 UserConfigDir
func configFilePath() (string, error) {
	if portable := portableConfigPath(); portable != "" {
		// 已存在的绿色 config 优先：避免一次 fallback 后再也回不来
		if _, err := os.Stat(portable); err == nil {
			return portable, nil
		}
		// 还没绿色 config，但 exe 同目录可写 → 落地成绿色
		if dirIsWritable(filepath.Dir(portable)) {
			return portable, nil
		}
	}
	// 不可写（Program Files / 只读盘 / dev 模式 wails dev 拿不到稳定 exe 路径）
	return fallbackConfigPath()
}

// LoadConfig 读取持久化的前端配置 JSON。
// 文件不存在或损坏时返回 "{}"（不报错，让前端走默认值）。
func (s *Service) LoadConfig() (string, error) {
	p, err := configFilePath()
	if err != nil {
		return "{}", nil // 取不到目录也不致命，给个空对象
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "{}", nil
	}
	if err != nil {
		return "{}", nil // 读不到（权限等）静默回退，避免阻塞 UI 启动
	}
	// 校验是合法 JSON，否则丢弃
	var anything any
	if err := json.Unmarshal(data, &anything); err != nil {
		return "{}", nil
	}
	return string(data), nil
}

// SaveConfig 把前端 JSON 配置写入持久化文件。
// raw 必须是合法 JSON 字符串，否则报错让前端知晓。
func (s *Service) SaveConfig(raw string) error {
	var anything any
	if err := json.Unmarshal([]byte(raw), &anything); err != nil {
		return core.Wrap("CONFIG_INVALID_JSON", "配置不是合法 JSON", err)
	}
	p, err := configFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return core.Wrap("CONFIG_MKDIR_FAILED", "创建配置目录失败", err)
	}
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		return core.Wrap("CONFIG_WRITE_FAILED", "写入配置文件失败: "+p, err)
	}
	return nil
}

// ConfigPath 返回配置文件的绝对路径，方便前端在"诊断"时打开它。
func (s *Service) ConfigPath() (string, error) {
	return configFilePath()
}
