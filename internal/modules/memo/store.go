package memo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store 便签本地原子化持久化存储引擎
type Store struct {
	filePath string
	mu       sync.RWMutex
}

// NewStore 实例化存储，传入目标数据文件绝对路径 (如 <DataDir>/memo.json)
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

// Load 读取所有便签数据
func (s *Store) Load() ([]MemoItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []MemoItem{}, nil
		}
		return nil, fmt.Errorf("读取便签文件失败: %w", err)
	}

	if len(data) == 0 {
		return []MemoItem{}, nil
	}

	var items []MemoItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("解析便签 JSON 失败: %w", err)
	}

	return items, nil
}

// Save 原子写入所有便签数据 (临时文件 + Rename 保证防断电损坏)
func (s *Store) Save(items []MemoItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveAtomic(items)
}

func (s *Store) saveAtomic(items []MemoItem) error {
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化便签数据失败: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", s.filePath, os.Getpid())
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("写入临时便签文件失败: %w", err)
	}

	// Windows 下直接 rename 到已存在目标可能会失败，因此先尝试 rename，若失败先移除再 rename
	if err := os.Rename(tmpFile, s.filePath); err != nil {
		_ = os.Remove(s.filePath)
		if err := os.Rename(tmpFile, s.filePath); err != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("原子替换便签文件失败: %w", err)
		}
	}

	return nil
}
