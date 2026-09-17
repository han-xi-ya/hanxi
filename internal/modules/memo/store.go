package memo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// Store 便签旧整库引擎（tmp+fsync+rename 原子写公共核 internal/jsonstore）。
// F3-b 文件库化后仅剩两职：迁移期读旧库（migrate.go）与迁移未成的回落写；
// 正常态权威是 FileStore（memo/<id>.md）。
type Store struct {
	filePath string
	mu       sync.RWMutex
}

// NewStore 实例化存储，传入目标数据文件绝对路径 (如 <StateDir>/memo.json)
func NewStore(filePath string) (*Store, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建便签存储目录失败: %w", err)
	}

	s := &Store{filePath: filePath}
	// 如果文件不存在，初始化一个空数组文件
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := s.saveAtomic([]MemoItem{}); err != nil {
			return nil, fmt.Errorf("初始化便签存储文件失败: %w", err)
		}
	}

	return s, nil
}

// Load 读取所有便签数据（严格策略：文件损坏/读取失败直接报错，不静默清空创作内容）
func (s *Store) Load() ([]MemoItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var items []MemoItem
	ok, err := jsonstore.Load(s.filePath, &items)
	switch {
	case err == nil && ok:
		return items, nil
	case errors.Is(err, jsonstore.ErrEmpty) || (err == nil && !ok):
		// 文件不存在或为 0 字节：视为空便签列表（与原实现逐分支一致）
		return []MemoItem{}, nil
	case errors.Is(err, jsonstore.ErrCorrupt):
		return nil, fmt.Errorf("解析便签 JSON 失败: %w", err)
	default:
		return nil, fmt.Errorf("读取便签文件失败: %w", err)
	}
}

// Save 原子写入所有便签数据 (临时文件 + Rename 保证防断电损坏)
func (s *Store) Save(items []MemoItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveAtomic(items)
}

func (s *Store) saveAtomic(items []MemoItem) error {
	if err := jsonstore.Save(s.filePath, items); err != nil {
		return fmt.Errorf("保存便签数据失败: %w", err)
	}
	return nil
}
