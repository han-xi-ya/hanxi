package msgboard

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"hanxi/internal/jsonstore"
)

// 预设文案模板（MVP 固定内置，卡片裁定"预设几条 + 自定义文字"；自定义正文
// 持久化在 store，预设只是模块页的一键填词候选，不进磁盘配置）。
var Presets = []string{
	"马上回来",
	"会议中，请勿打扰",
	"下班了，有事请留言",
	"请勿动我电脑",
}

// 字段边界：字号给挂牌场景留足"远看大字报"空间；正文限长防呆（全屏字再大也放
// 不下一页纸），越界直接中文报错回前端，不静默截断吞内容。
const (
	minFontSize   = 24
	maxFontSize   = 200
	maxTextRunes  = 400
	defaultHotkey = "Ctrl+Alt+B"
)

// msgBoardStore 持久化留言板偏好，位置 <stateDir>/msgboard.json。
// 原子写（tmp+rename）与损坏容忍（解析失败按默认值继续）收口 internal/jsonstore，
// 与 ocr/memo 等模块 store 同一族规。字段全指针形态落盘：区分"未设置"与
// 显式空串（如 hotkey="" 表示用户主动停用热键，加载时必须保留）。
type msgBoardStore struct {
	filePath string
	mu       sync.RWMutex
	cfg      Config
}

// persistedConfig 磁盘结构（指针字段：未落盘的键保持 nil，加载回落默认值）。
type persistedConfig struct {
	Text     *string `json:"text,omitempty"`
	FontSize *int    `json:"fontSize,omitempty"`
	Screen   *string `json:"screen,omitempty"`
	Hotkey   *string `json:"hotkey,omitempty"`
}

// defaultConfig 出厂偏好：正文空（展示时回落第一条预设）、64 DIP 大字、主屏、
// Ctrl+Alt+B 热键。
func defaultConfig() Config {
	return Config{FontSize: 64, Hotkey: defaultHotkey}
}

func newMsgBoardStore(dir string) *msgBoardStore {
	s := &msgBoardStore{filePath: filepath.Join(dir, "msgboard.json"), cfg: defaultConfig()}
	_ = s.load()
	return s
}

func (s *msgBoardStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var pc persistedConfig
	if ok, err := jsonstore.Load(s.filePath, &pc); err != nil || !ok {
		return err // 未初始化/损坏容忍：字段保持默认值
	}
	if pc.Text != nil {
		s.cfg.Text = strings.TrimSpace(*pc.Text)
	}
	if pc.FontSize != nil {
		s.cfg.FontSize = clampFontSize(*pc.FontSize)
	}
	if pc.Screen != nil {
		s.cfg.Screen = strings.TrimSpace(*pc.Screen)
	}
	if pc.Hotkey != nil {
		s.cfg.Hotkey = strings.TrimSpace(*pc.Hotkey)
	}
	return nil
}

// Get 返回当前偏好快照。
func (s *msgBoardStore) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Set 校验并全量落盘，返回规范化后的实际生效值（服务层据此比对热键/屏幕变更）。
// 规则：字号越界钳位（数值控件防呆兜底，不报错）；正文去首尾空白后限长
// maxTextRunes（超出中文报错）；热键仅做形态轻校验，真实可用性以
// RegisterHotKey 注册结果为准（服务层负责回滚）。
func (s *msgBoardStore) Set(cfg Config) (Config, error) {
	next := Config{
		Text:     strings.TrimSpace(cfg.Text),
		FontSize: clampFontSize(cfg.FontSize),
		Screen:   strings.TrimSpace(cfg.Screen),
		Hotkey:   strings.TrimSpace(cfg.Hotkey),
	}
	if utf8.RuneCountInString(next.Text) > maxTextRunes {
		return next, fmt.Errorf("留言文案过长（%d 字，上限 %d 字）——全屏大字放不下一页纸，请精简",
			utf8.RuneCountInString(next.Text), maxTextRunes)
	}
	if next.Hotkey != "" && !strings.Contains(next.Hotkey, "+") {
		return next, fmt.Errorf("热键 %q 无效：需至少一个修饰键（如 Ctrl+Alt+B），留空表示停用热键", next.Hotkey)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	pc := persistedConfig{Text: &next.Text, FontSize: &next.FontSize, Screen: &next.Screen, Hotkey: &next.Hotkey}
	if err := jsonstore.Save(s.filePath, pc); err != nil {
		return next, fmt.Errorf("保存留言板配置失败：%w", err)
	}
	s.cfg = next
	return next, nil
}

// effectiveText 展示正文：自定义为空时回落第一条预设（"一键挂牌"永不空白）。
func effectiveText(cfg Config) string {
	if cfg.Text != "" {
		return cfg.Text
	}
	return Presets[0]
}

func clampFontSize(v int) int {
	switch {
	case v < minFontSize:
		return minFontSize
	case v > maxFontSize:
		return maxFontSize
	default:
		return v
	}
}
