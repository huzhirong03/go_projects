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
//   - 否则探测 exe 同目录是否可写 →
//     a) 先尝试从老 fallback (%APPDATA%) 迁移老 config 过来（升级路径）
//     b) 然后用 exe 同目录（首次运行落地为绿色）
//   - 都不行 → fallback 到 UserConfigDir
func configFilePath() (string, error) {
	if portable := portableConfigPath(); portable != "" {
		// 已存在的绿色 config 优先：避免一次 fallback 后再也回不来
		if _, err := os.Stat(portable); err == nil {
			return portable, nil
		}
		// 还没绿色 config，但 exe 同目录可写 → 升级逻辑：先迁移老配置，再落地
		if dirIsWritable(filepath.Dir(portable)) {
			if fb, err := fallbackConfigPath(); err == nil {
				migrateConfigFromFallbackOnce(portable, fb)
			}
			return portable, nil
		}
	}
	// 不可写（Program Files / 只读盘 / dev 模式 wails dev 拿不到稳定 exe 路径）
	return fallbackConfigPath()
}

// migrateConfigFromFallbackOnce 一次性把老 fallback (%APPDATA%) 里的 config.json
// 迁移到 exe 同目录的 portable 路径。仅在以下条件全部满足时执行：
//  1. portable 路径**不存在**（避免覆盖已经绿色化的用户的最新配置）
//  2. fallback 路径**存在**（这是用户从老版本升级上来）
//  3. fallback 内容是合法 JSON（损坏的就别污染新位置）
//
// 静默失败：迁移本身是"锦上添花"，任何错误都不影响主流程，最坏后果是用户感觉
// 配置丢了一次（跟没加迁移逻辑的体验一致）。
//
// 参数化两个路径是为了**可测**：测试可注入临时目录路径，避免污染真实 %APPDATA%。
func migrateConfigFromFallbackOnce(portablePath, fallbackPath string) {
	// 1) portable 已有 → 不迁移
	if _, err := os.Stat(portablePath); err == nil {
		return
	}
	// 2) 读老路径
	data, err := os.ReadFile(fallbackPath)
	if err != nil {
		return // 老的也没有，正常的全新用户
	}
	// 3) 校验 JSON 合法
	var any interface{}
	if err := json.Unmarshal(data, &any); err != nil {
		return
	}
	// 4) 写入 portable（mkdir 通常不需要，exe 同目录已存在）
	_ = os.MkdirAll(filepath.Dir(portablePath), 0o755)
	_ = os.WriteFile(portablePath, data, 0o644)
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
