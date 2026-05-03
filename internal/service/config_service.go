package service

import (
	"encoding/json"
	"os"
	"path/filepath"

	"excel-master/internal/core"
)

// 配置文件位置：<UserConfigDir>/excel-master/config.json
//
// Windows: %APPDATA%\excel-master\config.json
// macOS:   ~/Library/Application Support/excel-master/config.json
// Linux:   ~/.config/excel-master/config.json
//
// 内容是前端任意 JSON。后端只做"合法性检查 + 落盘 + 读取"，
// 不解析具体字段（保持灵活，前端字段改动无需后端配合）。
const configFileName = "config.json"

func configFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", core.Wrap("CONFIG_DIR_FAILED", "无法定位用户配置目录", err)
	}
	return filepath.Join(dir, "excel-master", configFileName), nil
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
