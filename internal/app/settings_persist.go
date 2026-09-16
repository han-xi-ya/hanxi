package app

import (
	"fmt"

	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// GeneralSettings 前端通用设置模型
type GeneralSettings struct {
	AutoStart      bool `json:"autoStart"`
	MinimizeToTray bool `json:"minimizeToTray"`
	LogRetainDays  int  `json:"logRetainDays"`
}

// GetGeneralSettings 获取常规配置（开机自启、最小化托盘等）
func (s *AppService) GetGeneralSettings() GeneralSettings {
	if s.store == nil {
		return GeneralSettings{
			AutoStart:      false,
			MinimizeToTray: true,
			LogRetainDays:  7,
		}
	}
	cfg := s.store.Get()
	// 开机自启以注册表实际状态为准（配置值仅作意图记录，外部改动实时反映）
	cfg.AutoStart = windows.IsAutoStart()
	return GeneralSettings{
		AutoStart:      cfg.AutoStart,
		MinimizeToTray: cfg.MinimizeToTray,
		LogRetainDays:  cfg.LogRetainDays,
	}
}

// SetGeneralSettings 保存常规配置
func (s *AppService) SetGeneralSettings(gen GeneralSettings) error {
	if err := windows.SetAutoStart(gen.AutoStart); err != nil {
		return fmt.Errorf("设置开机自启动失败: %w", err)
	}

	if s.store != nil {
		return s.store.Update(func(cfg *settings.AppSettings) {
			cfg.AutoStart = gen.AutoStart
			cfg.MinimizeToTray = gen.MinimizeToTray
			if gen.LogRetainDays > 0 {
				cfg.LogRetainDays = gen.LogRetainDays
			}
		})
	}
	return nil
}

// SetTrayRebuilder 注入托盘菜单热重建回调，仅由装配根（app.New）在托盘创建后调用。
func (s *AppService) SetTrayRebuilder(fn func()) { s.trayRebuild = fn }

// SetWindowDarkApplier 注入主窗口标题栏深色应用回调，仅由装配根（app.New）在窗口创建后调用。
func (s *AppService) SetWindowDarkApplier(fn func(dark bool) error) { s.windowDark = fn }

// GetTheme 返回持久化的主题模式："light" | "dark" | "system"（异常/未设置回退浅色）。
func (s *AppService) GetTheme() string {
	if s.store == nil {
		return "light"
	}
	switch t := s.store.Get().Theme; t {
	case "light", "dark", "system":
		return t
	default:
		return "light"
	}
}

// SetTheme 持久化主题模式。DOM 与标题栏的实际应用由前端 useTheme 完成
// （其解析 system→亮/暗后回调 SetWindowDarkMode），后端不感知解析细节。
func (s *AppService) SetTheme(mode string) error {
	switch mode {
	case "light", "dark", "system":
	default:
		return fmt.Errorf("非法主题模式: %s", mode)
	}
	if s.store == nil {
		return nil
	}
	return s.store.Update(func(cfg *settings.AppSettings) {
		cfg.Theme = mode
	})
}
