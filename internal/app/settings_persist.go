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

// SetWindowDarkApplier 注入主窗口标题栏深色/色板应用回调，仅由装配根（app.New）在窗口创建后调用。
func (s *AppService) SetWindowDarkApplier(fn func(dark bool, accent string) error) { s.windowDark = fn }

// GetFont 返回持久化的界面字体档："kai" | "plain" | "mono"（异常/未设置回退默认文楷）。
// 合法域与 DOM data-font 属性、fonts.css N40 档位覆写块三处字面一致（useTheme.spec / fonts.spec 锁）。
func (s *AppService) GetFont() string {
	if s.store == nil {
		return "kai"
	}
	switch f := s.store.Get().Font; f {
	case "kai", "plain", "mono":
		return f
	default:
		return "kai"
	}
}

// SetFont 持久化界面字体档。DOM 的 data-font 实际应用由前端 useTheme 完成，后端不感知。
func (s *AppService) SetFont(font string) error {
	switch font {
	case "kai", "plain", "mono":
	default:
		return fmt.Errorf("非法界面字体档: %s", font)
	}
	if s.store == nil {
		return nil
	}
	return s.store.Update(func(cfg *settings.AppSettings) {
		cfg.Font = font
	})
}

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

// GetAccent 返回持久化的色板轴："teal" | "sky" | "iris" | "jade" | "onyx"（异常/未设置回退默认青壳）。
func (s *AppService) GetAccent() string {
	if s.store == nil {
		return "teal"
	}
	switch a := s.store.Get().Accent; a {
	case "teal", "sky", "iris", "jade", "onyx":
		return a
	default:
		return "teal"
	}
}

// SetAccent 持久化色板轴。DOM 的 data-accent 实际应用由前端 useTheme 完成，后端不感知。
func (s *AppService) SetAccent(accent string) error {
	switch accent {
	case "teal", "sky", "iris", "jade", "onyx":
	default:
		return fmt.Errorf("非法色板: %s", accent)
	}
	if s.store == nil {
		return nil
	}
	return s.store.Update(func(cfg *settings.AppSettings) {
		cfg.Accent = accent
	})
}
