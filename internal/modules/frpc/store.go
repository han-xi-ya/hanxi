package frpc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hubkit/internal/domain"
	"hubkit/internal/platform/windows"
)

const dpapiPrefix = "dpapi:"

// frpcStore 项目存储：以单 JSON 文件原子读写持久化全部 frpc 项目。
// 位置：<dataDir>/frpc/projects.json（与版本隔离目录同根，方便整体拷贝搬迁）。
type frpcStore struct {
	filePath string
	mu       sync.RWMutex
	projects map[string]domain.Project // key: project.ID
}

func newFrpcStore(dir string) *frpcStore {
	s := &frpcStore{
		filePath: filepath.Join(dir, "projects.json"),
		projects: make(map[string]domain.Project),
	}
	_ = s.load()
	return s
}

func (s *frpcStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var data struct {
		Projects []domain.Project `json:"projects"`
	}
	if err := json.Unmarshal(bytes, &data); err != nil {
		return fmt.Errorf("corrupt frpc projects.json: %w", err)
	}
	// 兼容与解密：自动解密 DPAPI 加密的 Token，无 ID 的项目自动补 ID
	for _, p := range data.Projects {
		if p.ID == "" {
			p.ID = newProjectID()
		}
		// 若 Token 使用 DPAPI 加密，则在内存中解密为明文
		if strings.HasPrefix(p.Server.Token, dpapiPrefix) {
			cipher := strings.TrimPrefix(p.Server.Token, dpapiPrefix)
			plain, err := windows.DPAPIDecrypt(cipher)
			if err == nil {
				p.Server.Token = string(plain)
			}
		}
		s.projects[p.ID] = p
	}
	return nil
}

func (s *frpcStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	list := make([]domain.Project, 0, len(s.projects))
	for _, p := range s.projects {
		// 落盘保护：克隆对象并将敏感 Token 加密为 DPAPI 密文
		item := p
		if item.Server.Token != "" {
			cipher, err := windows.DPAPIEncrypt([]byte(item.Server.Token))
			if err == nil {
				item.Server.Token = dpapiPrefix + cipher
			}
		}
		list = append(list, item)
	}
	data := struct {
		Projects  []domain.Project `json:"projects"`
		UpdatedAt string           `json:"updatedAt"`
	}{list, time.Now().Format("2006-01-02 15:04:05")}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", s.filePath, os.Getpid())
	if err := os.WriteFile(tmp, bytes, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func newProjectID() string {
	return fmt.Sprintf("p%d", time.Now().UnixNano())
}

// List 返回全部项目（副本）
func (s *frpcStore) List() ([]domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]domain.Project, 0, len(s.projects))
	for _, p := range s.projects {
		list = append(list, p)
	}
	return list, nil
}

// Get 按 ID 查询项目
func (s *frpcStore) Get(id string) (domain.Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[id]
	return p, ok
}

// Save 新建或更新项目（自动维护 CreatedAt/UpdatedAt；空 ID 自动生成）
func (s *frpcStore) Save(p *domain.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format("2006-01-02 15:04:05")
	if p.ID == "" {
		p.ID = newProjectID()
		p.CreatedAt = now
	} else if _, exists := s.projects[p.ID]; !exists {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	s.projects[p.ID] = *p

	return s.saveLocked()
}

// Delete 删除项目
func (s *frpcStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.projects[id]; !ok {
		return fmt.Errorf("项目 %s 不存在", id)
	}
	delete(s.projects, id)
	return s.saveLocked()
}
