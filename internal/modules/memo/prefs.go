package memo

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"hanxi/internal/jsonstore"
)

// 悬浮速记卡偏好（N16 B 批）：目前唯一一项是全局速记热键，独立落
// <stateDir>/memo-prefs.json——绝不与旧库 memo.json / 文件库 memo/ 混写：
// 前者是"迁移未成"的异常态数据权威，后者是用户便签数据本体（快照白名单
// 口径 memo/ 目录整体归数据），配置掺进去会污染 ClearAll 的"不留残片"收口。
//
// 形态与 msgboard store 同族规（internal/jsonstore 原子写 + 损坏容忍回默认值，
// 指针字段区分"未设置"与显式空串）。热键留空 = 用户主动停用，加载时必须保留。

// defaultQuickHotkey 出厂速记热键：Ctrl+Alt+N（N = Note）。与既有热键
// （ocr/snip Ctrl+Alt+T、msgboard/toggle Ctrl+Alt+B）互不冲突。
const defaultQuickHotkey = "Ctrl+Alt+N"

// quickMemoStore 悬浮速记卡偏好持久化。nil 接收者全部按默认值语义工作
// （测试直装 MemoService 不接 store 的路径天然安全）。
type quickMemoStore struct {
	filePath string
	mu       sync.RWMutex
	hotkey   string
}

// persistedQuickPrefs 磁盘结构（指针字段：未落盘保持 nil，加载回落默认值）。
type persistedQuickPrefs struct {
	QuickHotkey *string `json:"quickHotkey,omitempty"`
}

func newQuickMemoStore(dir string) *quickMemoStore {
	s := &quickMemoStore{filePath: filepath.Join(dir, "memo-prefs.json"), hotkey: defaultQuickHotkey}
	var pc persistedQuickPrefs
	if ok, err := jsonstore.Load(s.filePath, &pc); err == nil && ok && pc.QuickHotkey != nil {
		s.hotkey = strings.TrimSpace(*pc.QuickHotkey)
	}
	return s
}

// Hotkey 当前速记热键（""= 停用）。nil 接收者回落厂默认。
func (s *quickMemoStore) Hotkey() string {
	if s == nil {
		return defaultQuickHotkey
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hotkey
}

// validateHotkey 形态校验纯函数：去空白 + 非空时必须含修饰键（"+" 分隔）。
// 真实可用性以 RegisterHotKey 注册结果为准（服务层持冲突回滚事务）。
func validateHotkey(raw string) (string, error) {
	norm := strings.TrimSpace(raw)
	if norm != "" && !strings.Contains(norm, "+") {
		return norm, fmt.Errorf("热键 %q 无效：需至少一个修饰键（如 Ctrl+Alt+N），留空表示停用热键", norm)
	}
	return norm, nil
}

// SetHotkey 校验并原子落盘，返回规范化后的实际值。
func (s *quickMemoStore) SetHotkey(raw string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("速记偏好存储未就绪")
	}
	norm, err := validateHotkey(raw)
	if err != nil {
		return norm, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	pc := persistedQuickPrefs{QuickHotkey: &norm}
	if err := jsonstore.Save(s.filePath, pc); err != nil {
		return norm, fmt.Errorf("保存速记热键配置失败：%w", err)
	}
	s.hotkey = norm
	return norm, nil
}
