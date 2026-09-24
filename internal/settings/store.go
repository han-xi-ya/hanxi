// Package settings 提供应用全局配置（config.json）的模型、加载与持久化，
// 以及数据目录布局解析（F6 起同级默认 + 显式绑定，见 paths.go）。
// 本包不依赖任何业务模块代码，仅被 app 与各业务模块反向引用；
// 唯一的契约级依赖是 extapi（receipts.go 实现其 ReceiptStorage 接口，无环）。
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
	ID                    string `json:"id"`         // 唯一标识 (例如 botId 或 时间戳/UUID)
	RemarkName            string `json:"remarkName"` // 备注名称（如“告警通知号”、“个人测试号”）
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

// WebAppEntry 网页应用窗口的网址条目配置模型（webapp 模块）。
// 尺寸/坐标为"记忆位"：0 表示未记忆，建窗时回退默认 1120x820 居中；
// 窗口收起/销毁时由 webapp 服务回写。
type WebAppEntry struct {
	ID        string `json:"id"`        // 唯一标识（webapp_<UnixNano>；出厂预置用固定 ID）
	Name      string `json:"name"`      // 显示名，兼作子窗口标题
	URL       string `json:"url"`       // 外部地址（仅 http/https，webapp 服务层闸门校验）
	Icon      string `json:"icon"`      // emoji 等自由文本图标，可空，仅前端行内展示
	Width     int    `json:"width"`     // 记忆窗口宽度（0=默认）
	Height    int    `json:"height"`    // 记忆窗口高度（0=默认）
	X         int    `json:"x"`         // 记忆窗口坐标 X（X/Y 同为非 0 才生效；负值=副屏合法）
	Y         int    `json:"y"`         // 记忆窗口坐标 Y
	CreatedAt string `json:"createdAt"` // 创建时刻（预置条目为空）
}

// 托盘菜单项类型常量。
const (
	TrayItemCommand = "command" // 托管模块启动命令，Ref = "moduleId/commandId"
	TrayItemRoute   = "route"   // 打开主窗口模块页面，Ref = 前端路由
	TrayItemExe     = "exe"     // 启动任意外部程序，Path/Args 自描述
	TrayItemGroup   = "group"   // 分组：轮盘二级扇区/托盘子菜单容器，本身无动作，Children 仅允许叶子条目
)

// TrayMenuItem 托盘右键菜单自定义条目（配置切片顺序即菜单显示顺序）。
type TrayMenuItem struct {
	Type     string         `json:"type"`               // "command" | "route" | "exe" | "group"
	Ref      string         `json:"ref"`                // command: "moduleId/commandId"；route: 前端路由；其余留空
	Path     string         `json:"path"`               // exe: 可执行文件绝对路径；其余留空
	Args     string         `json:"args"`               // exe: 启动参数（空格分隔，支持一对引号包裹含空格参数）；其余留空
	Label    string         `json:"label"`              // 菜单显示名；留空则回退默认名（命令/页面/程序名）；group 必填
	Enabled  bool           `json:"enabled"`            // 是否显示在托盘右键菜单
	Children []TrayMenuItem `json:"children,omitempty"` // group: 二级条目（仅 command/route/exe，不允许再嵌套）；其余留空
}

// SnapshotConfig 数据历史版本（自动版本快照）偏好。字段语义见 internal/snapshot；
// 缺字段解码进 DefaultSettings 副本即自动回落出厂值（与 QuickMenuTwoTier 同机制）。
type SnapshotConfig struct {
	Enabled         bool `json:"enabled"`         // 总开关（默认开）
	IdleSeconds     int  `json:"idleSeconds"`     // mtime 空闲兜底阈值：白名单文件静默该秒数后视为可拍（默认 300）
	IntervalMinutes int  `json:"intervalMinutes"` // 两次提交的最小间隔，防编辑期连环保存灌碎历史（默认 5）
}

// AppSettings 应用全局配置模型
type AppSettings struct {
	Theme            string            `json:"theme"`            // 明暗轴 "light" | "dark" | "system"
	Accent           string            `json:"accent"`           // 色板轴 "teal" | "sky" | "iris" | "jade" | "onyx"
	Language         string            `json:"language"`         // "zh-CN" | "en-US"
	AutoStart        bool              `json:"autoStart"`        // 开机自启
	MinimizeToTray   bool              `json:"minimizeToTray"`   // 关闭时最小化到托盘
	LogRetainDays    int               `json:"logRetainDays"`    // 日志保留天数（默认 7）
	Modules          map[string]bool   `json:"modules"`          // 各模块启用状态 map[moduleId]enabled
	LanRemarks       map[string]string `json:"lanRemarks"`       // 局域网 IP/MAC 备注 map[identifier]remark
	TrayMenu         []TrayMenuItem    `json:"trayMenu"`         // 托盘右键菜单自定义条目（有序）
	QuickMenuTwoTier bool              `json:"quickMenuTwoTier"` // 快捷菜单轮盘是否启用二级展开（默认开；关=分组子条目拍平进主盘）
	// N5-C2 触发参数外化：0 = 出厂默认（450ms / 16px）。有效值钳制在消费方
	// quickmenu 服务执行（盘上值按不可信输入对待，坏值不武装鼠标钩子）。
	QuickMenuHoldMs    int             `json:"quickMenuHoldMs"`
	QuickMenuMovePx    int             `json:"quickMenuMovePx"`
	HistoryOcrFullText bool            `json:"historyOcrFullText"` // 历史记录是否收录 OCR 识别全文（默认开；关=只记图片路径与摘要，不存识别文本）
	Wechat             WechatConfig    `json:"wechat"`             // 微信机器人遗留配置（向下兼容）
	WechatAccounts     []WechatAccount `json:"wechatAccounts"`     // 微信多账号列表
	WebAppEntries      []WebAppEntry   `json:"webAppEntries"`      // 网页应用窗口网址条目（有序，配置顺序即列表显示顺序）
	Snapshot           SnapshotConfig  `json:"snapshot"`           // 数据历史版本偏好（internal/snapshot）
}

// DefaultSettings 返回出厂默认配置：浅色主题、青壳色板、中文、关闭时最小化到托盘、日志保留 7 天。
func DefaultSettings() AppSettings {
	return AppSettings{
		Theme:              "light",
		Accent:             "teal",
		Language:           "zh-CN",
		AutoStart:          false,
		MinimizeToTray:     true,
		LogRetainDays:      7,
		Modules:            make(map[string]bool),
		LanRemarks:         make(map[string]string),
		TrayMenu:           make([]TrayMenuItem, 0),
		QuickMenuTwoTier:   true, // 二级轮盘默认开启：load 解码进默认副本，旧配置文件缺字段自动落 true
		HistoryOcrFullText: true, // OCR 全文入历史默认开启（同 QuickMenuTwoTier 缺字段回落机制）
		// wechat 业务默认端点不属设置存储职责：出厂留空，
		// 缺省回退由 wechat 模块读取侧兜底（defaultBaseURL）。
		Wechat:         WechatConfig{},
		WechatAccounts: make([]WechatAccount, 0),
		// 出厂预置微信文件传输助手网页版：旧配置文件缺 webAppEntries 键时，
		// load() 解码进本默认副本即自动带出；用户显式存 [] 后不再复活。
		WebAppEntries: []WebAppEntry{{
			ID:   "webapp_filehelper",
			Name: "微信文件传输助手",
			URL:  "https://filehelper.weixin.qq.com/",
			Icon: "💬",
		}},
		// 历史版本快照默认开启（零用户负担红线）；空闲 300s / 最小间隔 5min
		// 为 PLAN_SNAPSHOT Q4 拍板默认值。
		Snapshot: SnapshotConfig{Enabled: true, IdleSeconds: 300, IntervalMinutes: 5},
	}
}

// Store 配置存储管理器
type Store struct {
	filePath string
	mu       sync.RWMutex
	data     AppSettings
}

// NewStore 创建配置存储并立即加载 filePath 处的 JSON 配置。
// 文件不存在不算错误（视为首次运行，保留默认值）；JSON 损坏则返回错误。
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

// load 读取并反序列化配置文件，补齐 nil 集合字段，并把旧版单微信账号配置迁移进多账号列表。
// 调用方（NewStore）尚未对外暴露 Store，无需额外加锁之外的时序约束；此处仍持写锁保证一致。
func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	// 解码进出厂默认的副本而非零值结构体：旧配置缺失/未包含的字段自动回落默认值
	// （零值解码会把 MinimizeToTray 变 false、LogRetainDays 变 0、Theme/Accent 变空串）。
	// JSON 显式写出的字段仍按文件值覆盖；map/slice 显式为 null 时由下方兜底重建。
	data := DefaultSettings()
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
	if data.WebAppEntries == nil {
		data.WebAppEntries = make([]WebAppEntry, 0)
	}
	if data.TrayMenu == nil {
		data.TrayMenu = make([]TrayMenuItem, 0)
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
	return cloneAppSettings(s.data)
}

// cloneAppSettings 深拷贝全部集合字段，保证调用方按下标/键写入不会污染 Store 内存态。
// WechatAccounts 曾因漏拷共享底层数组，调用方改 cfg.WechatAccounts[i] 直接篡改 Store——新增字段必须同步进本函数。
// TrayMenu 自二级分组起元素内含可变 Children 切片，一层拷贝不完备，走 cloneTrayItems。
func cloneAppSettings(src AppSettings) AppSettings {
	cp := src
	cp.Modules = make(map[string]bool, len(src.Modules))
	for k, v := range src.Modules {
		cp.Modules[k] = v
	}
	cp.LanRemarks = make(map[string]string, len(src.LanRemarks))
	for k, v := range src.LanRemarks {
		cp.LanRemarks[k] = v
	}
	cp.TrayMenu = cloneTrayItems(src.TrayMenu)
	cp.WechatAccounts = append(make([]WechatAccount, 0, len(src.WechatAccounts)), src.WechatAccounts...)
	cp.WebAppEntries = append(make([]WebAppEntry, 0, len(src.WebAppEntries)), src.WebAppEntries...)
	return cp
}

// cloneTrayItems 深拷贝托盘条目切片（含 group 的 Children 嵌套层）。
func cloneTrayItems(src []TrayMenuItem) []TrayMenuItem {
	out := make([]TrayMenuItem, 0, len(src))
	for _, it := range src {
		if it.Children != nil {
			it.Children = cloneTrayItems(it.Children)
		}
		out = append(out, it)
	}
	return out
}

// Update 更新配置并原子落盘。
// 候选提交语义：fn 只改副本，落盘成功后内存才整体换装；落盘失败回滚原值——
// 保证内存与磁盘不分叉（否则运行期读到新值、重启后读回旧值，状态"随机回弹"）。
func (s *Store) Update(fn func(cfg *AppSettings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := cloneAppSettings(s.data)
	fn(&candidate)
	prev := s.data
	s.data = candidate
	if err := s.saveLocked(); err != nil {
		s.data = prev
		return err
	}
	return nil
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
			// 账号清空后遗留字段整体归零；默认端点回退由 wechat 模块读取侧负责
			c.Wechat = WechatConfig{}
		}
	})
}

// GetWebAppEntries 获取全部网页应用网址条目副本（保持配置顺序）
func (s *Store) GetWebAppEntries() []WebAppEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]WebAppEntry, len(s.data.WebAppEntries))
	copy(res, s.data.WebAppEntries)
	return res
}

// GetWebAppEntryByID 按 ID 获取单个网址条目
func (s *Store) GetWebAppEntryByID(id string) (WebAppEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, e := range s.data.WebAppEntries {
		if e.ID == id {
			return e, true
		}
	}
	return WebAppEntry{}, false
}

// UpsertWebAppEntry 添加或更新网址条目并持久化（按 ID 命中替换，否则追加到末尾）
func (s *Store) UpsertWebAppEntry(entry WebAppEntry) error {
	return s.Update(func(c *AppSettings) {
		if c.WebAppEntries == nil {
			c.WebAppEntries = make([]WebAppEntry, 0)
		}
		for i, existing := range c.WebAppEntries {
			if existing.ID == entry.ID {
				c.WebAppEntries[i] = entry
				return
			}
		}
		c.WebAppEntries = append(c.WebAppEntries, entry)
	})
}

// DeleteWebAppEntry 删除指定网址条目并持久化；同步清理托盘/轮盘配置中
// 指向本条目的命令引用（"webapp/open:<id>"），避免已删条目在托盘菜单里
// 残留成"点击必报错"的死条目。
func (s *Store) DeleteWebAppEntry(id string) error {
	ref := "webapp/open:" + id
	return s.Update(func(c *AppSettings) {
		filtered := make([]WebAppEntry, 0, len(c.WebAppEntries))
		for _, e := range c.WebAppEntries {
			if e.ID != id {
				filtered = append(filtered, e)
			}
		}
		c.WebAppEntries = filtered
		c.TrayMenu = pruneTrayRefs(c.TrayMenu, ref)
	})
}

// pruneTrayRefs 剔除 command 引用等于 ref 的条目（含 group 子层），其余原样保留。
func pruneTrayRefs(items []TrayMenuItem, ref string) []TrayMenuItem {
	if len(items) == 0 {
		return items
	}
	out := make([]TrayMenuItem, 0, len(items))
	for _, it := range items {
		if it.Type == TrayItemCommand && it.Ref == ref {
			continue
		}
		if it.Type == TrayItemGroup {
			it.Children = pruneTrayRefs(it.Children, ref)
		}
		out = append(out, it)
	}
	return out
}

// GetTrayMenu 获取托盘右键菜单条目配置副本（按保存顺序，含 group 子条目的深拷贝）
func (s *Store) GetTrayMenu() []TrayMenuItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneTrayItems(s.data.TrayMenu)
}

// GetQuickMenuTwoTier 快捷菜单轮盘是否启用二级展开（默认开启）。
func (s *Store) GetQuickMenuTwoTier() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.QuickMenuTwoTier
}

// SetQuickMenuTwoTier 保存轮盘二级展开开关并原子落盘。
func (s *Store) SetQuickMenuTwoTier(on bool) error {
	return s.Update(func(cfg *AppSettings) {
		cfg.QuickMenuTwoTier = on
	})
}

// GetQuickMenuTrigger 返回轮盘触发参数**原始盘值**（ms/px；0=未设置走出厂默认）。
// 有效值判定与钳制归消费方（quickmenu 服务），本 getter 不修值——存储层只见事实。
func (s *Store) GetQuickMenuTrigger() (holdMs, movePx int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.QuickMenuHoldMs, s.data.QuickMenuMovePx
}

// SetQuickMenuTrigger 保存轮盘触发参数并原子落盘（0 恒为出厂默认语义，合法域
// 校验/钳制在调用方完成）。
func (s *Store) SetQuickMenuTrigger(holdMs, movePx int) error {
	return s.Update(func(cfg *AppSettings) {
		cfg.QuickMenuHoldMs = holdMs
		cfg.QuickMenuMovePx = movePx
	})
}

// SetTrayMenu 保存托盘右键菜单条目配置并原子落盘（入库前深拷贝，杜绝与调用方共享 Children）
func (s *Store) SetTrayMenu(items []TrayMenuItem) error {
	return s.Update(func(cfg *AppSettings) {
		cfg.TrayMenu = cloneTrayItems(items)
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
