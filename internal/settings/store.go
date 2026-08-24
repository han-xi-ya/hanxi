package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// WechatAccount 微信多账号配置模型
type WechatAccount struct {
	ID                    string `json:"id"`                    // 唯一标识 (例如 botId 或 时间戳/UUID)
	RemarkName            string `json:"remarkName"`            // 备注名称（如“告警通知号”、“个人测试号”）
	BotToken              string `json:"botToken"`
	IlinkBotID            string `json:"ilinkBotId"`
	IlinkUserID           string `json:"ilinkUserId"`
	ContextToken          string `json:"contextToken"`
	ContextTokenUpdatedAt string `json:"contextTokenUpdatedAt"`
	TargetUserID          string `json:"targetUserId"`
	BaseURL               string `json:"baseUrl"`
	CreatedAt             string `json:"createdAt"`
}

// WechatConfig 微信 ClawBot 单账号遗留兼容配置
type WechatConfig struct {
	BotToken              string `json:"botToken"`
	IlinkBotID            string `json:"ilinkBotId"`
	IlinkUserID           string `json:"ilinkUserId"`
	ContextToken          string `json:"contextToken"`
	ContextTokenUpdatedAt string `json:"contextTokenUpdatedAt"`
	TargetUserID          string `json:"targetUserId"`
	BaseURL               string `json:"baseUrl"`
}

// AppSettings 应用全局配置模型
type AppSettings struct {
	Theme          string            `json:"theme"`          // "light" | "dark" | "system"
	Language       string            `json:"language"`       // "zh-CN" | "en-US"
	AutoStart      bool              `json:"autoStart"`      // 开机自启
	MinimizeToTray bool              `json:"minimizeToTray"` // 关闭时最小化到托盘
	LogRetainDays  int               `json:"logRetainDays"`  // 日志保留天数（默认 7）
	Modules        map[string]bool   `json:"modules"`        // 各模块启用状态 map[moduleId]enabled
	LanRemarks     map[string]string `json:"lanRemarks"`     // 局域网 IP/MAC 备注 map[identifier]remark
	Wechat         WechatConfig      `json:"wechat"`         // 微信机器人遗留配置（向下兼容）
	WechatAccounts []WechatAccount   `json:"wechatAccounts"` // 微信多账号列表
}

func DefaultSettings() AppSettings {
	return AppSettings{
		Theme:          "light",
		Language:       "zh-CN",
		AutoStart:      false,
		MinimizeToTray: true,
		LogRetainDays:  7,
		Modules:        make(map[string]bool),
		LanRemarks:     make(map[string]string),
		Wechat: WechatConfig{
			BaseURL: "https://ilinkai.weixin.qq.com",
		},
		WechatAccounts: make([]WechatAccount, 0),
	}
}

// Store 配置存储管理器
type Store struct {
	filePath string
	mu       sync.RWMutex
	data     AppSettings
}

func NewStore(filePath string) (*Store, error) {
	s := &Store{
		filePath: filePath,
		data:     DefaultSettings(),
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var data AppSettings
	if err := json.Unmarshal(bytes, &data); err != nil {
		return fmt.Errorf("corrupt config json: %w", err)
	}

	if data.Modules == nil {
		data.Modules = make(map[string]bool)
	}
	if data.LanRemarks == nil {
		data.LanRemarks = make(map[string]string)
	}
	if data.WechatAccounts == nil {
		data.WechatAccounts = make([]WechatAccount, 0)
	}

	// 平滑迁移：若旧版单账号存在有效凭据且多账号列表为空，自动迁移为多账号中的第一项
	if data.Wechat.BotToken != "" && len(data.WechatAccounts) == 0 {
		accountID := data.Wechat.IlinkBotID
		if accountID == "" {
			accountID = "wechat-default"
		}
		remark := "默认微信机器人"
		if data.Wechat.IlinkUserID != "" {
			remark = fmt.Sprintf("微信助手 (%s)", data.Wechat.IlinkUserID)
		}
		data.WechatAccounts = append(data.WechatAccounts, WechatAccount{
			ID:                    accountID,
			RemarkName:            remark,
			BotToken:              data.Wechat.BotToken,
			IlinkBotID:            data.Wechat.IlinkBotID,
			IlinkUserID:           data.Wechat.IlinkUserID,
			ContextToken:          data.Wechat.ContextToken,
			ContextTokenUpdatedAt: data.Wechat.ContextTokenUpdatedAt,
			TargetUserID:          data.Wechat.TargetUserID,
			BaseURL:               data.Wechat.BaseURL,
			CreatedAt:             data.Wechat.ContextTokenUpdatedAt,
		})
	}

	s.data = data
	return nil
}

// Get 获取当前配置副本
func (s *Store) Get() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := s.data
	cp.Modules = make(map[string]bool, len(s.data.Modules))
	for k, v := range s.data.Modules {
		cp.Modules[k] = v
	}
	cp.LanRemarks = make(map[string]string, len(s.data.LanRemarks))
	for k, v := range s.data.LanRemarks {
		cp.LanRemarks[k] = v
	}
	return cp
}

// Update 更新配置并原子落盘
func (s *Store) Update(fn func(cfg *AppSettings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fn(&s.data)
	return s.saveLocked()
}

// IsModuleEnabled 查询特定模块是否启用
func (s *Store) IsModuleEnabled(moduleId string, defaultEnabled bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if val, ok := s.data.Modules[moduleId]; ok {
		return val
	}
	return defaultEnabled
}

// SetModuleEnabled 切换模块启用状态并持久化
func (s *Store) SetModuleEnabled(moduleId string, enabled bool) error {
	return s.Update(func(cfg *AppSettings) {
		if cfg.Modules == nil {
			cfg.Modules = make(map[string]bool)
		}
		cfg.Modules[moduleId] = enabled
	})
}

// GetLanRemarks 获取所有 IP/MAC 备注
func (s *Store) GetLanRemarks() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make(map[string]string, len(s.data.LanRemarks))
	for k, v := range s.data.LanRemarks {
		res[k] = v
	}
	return res
}

// SetLanRemark 设置单个 IP 或 MAC 的备注并落盘
func (s *Store) SetLanRemark(key, remark string) error {
	return s.Update(func(cfg *AppSettings) {
		if cfg.LanRemarks == nil {
			cfg.LanRemarks = make(map[string]string)
		}
		if remark == "" {
			delete(cfg.LanRemarks, key)
		} else {
			cfg.LanRemarks[key] = remark
		}
	})
}

// GetWechatConfig 获取微信机器人配置 (遗留单账号)
func (s *Store) GetWechatConfig() WechatConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Wechat
}

// SetWechatConfig 更新微信机器人配置并原子落盘 (遗留单账号)
func (s *Store) SetWechatConfig(cfg WechatConfig) error {
	return s.Update(func(c *AppSettings) {
		c.Wechat = cfg
	})
}

// GetWechatAccounts 获取所有已配置的微信机器人账号
func (s *Store) GetWechatAccounts() []WechatAccount {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]WechatAccount, len(s.data.WechatAccounts))
	copy(res, s.data.WechatAccounts)
	return res
}

// GetWechatAccountByID 按 ID 获取单个微信账号
func (s *Store) GetWechatAccountByID(id string) (WechatAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, acc := range s.data.WechatAccounts {
		if acc.ID == id {
			return acc, true
		}
	}
	return WechatAccount{}, false
}

// UpsertWechatAccount 添加或更新微信账号并持久化
func (s *Store) UpsertWechatAccount(acc WechatAccount) error {
	return s.Update(func(c *AppSettings) {
		if c.WechatAccounts == nil {
			c.WechatAccounts = make([]WechatAccount, 0)
		}
		found := false
		for i, existing := range c.WechatAccounts {
			if existing.ID == acc.ID {
				c.WechatAccounts[i] = acc
				found = true
				break
			}
		}
		if !found {
			c.WechatAccounts = append(c.WechatAccounts, acc)
		}
		// 同时同步到遗留 Wechat 字段以保持向下兼容
		if len(c.WechatAccounts) > 0 {
			first := c.WechatAccounts[0]
			c.Wechat = WechatConfig{
				BotToken:              first.BotToken,
				IlinkBotID:            first.IlinkBotID,
				IlinkUserID:           first.IlinkUserID,
				ContextToken:          first.ContextToken,
				ContextTokenUpdatedAt: first.ContextTokenUpdatedAt,
				TargetUserID:          first.TargetUserID,
				BaseURL:               first.BaseURL,
			}
		}
	})
}

// DeleteWechatAccount 删除指定微信账号
func (s *Store) DeleteWechatAccount(id string) error {
	return s.Update(func(c *AppSettings) {
		filtered := make([]WechatAccount, 0, len(c.WechatAccounts))
		for _, acc := range c.WechatAccounts {
			if acc.ID != id {
				filtered = append(filtered, acc)
			}
		}
		c.WechatAccounts = filtered

		// 同步更新兼容字段
		if len(c.WechatAccounts) > 0 {
			first := c.WechatAccounts[0]
			c.Wechat = WechatConfig{
				BotToken:              first.BotToken,
				IlinkBotID:            first.IlinkBotID,
				IlinkUserID:           first.IlinkUserID,
				ContextToken:          first.ContextToken,
				ContextTokenUpdatedAt: first.ContextTokenUpdatedAt,
				TargetUserID:          first.TargetUserID,
				BaseURL:               first.BaseURL,
			}
		} else {
			c.Wechat = WechatConfig{
				BaseURL: "https://ilinkai.weixin.qq.com",
			}
		}
	})
}

// saveLocked 原子写文件：写入临时文件后 Rename，避免崩溃导致配置损坏
func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	bytes, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings failed: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", s.filePath, os.Getpid())
	if err := os.WriteFile(tmpFile, bytes, 0644); err != nil {
		return fmt.Errorf("write temp settings failed: %w", err)
	}

	if err := os.Rename(tmpFile, s.filePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("rename temp settings failed: %w", err)
	}

	return nil
}
