package frpc

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hanxi/internal/domain"
	"hanxi/internal/jsonstore"
	"hanxi/internal/platform/windows"
)

const dpapiPrefix = "dpapi:"

// frpcStore 项目存储：以单 JSON 文件原子读写持久化全部 frpc 项目
// （原子写公共核 internal/jsonstore；本项目刻意取"损坏即报错、禁止以空库覆盖落盘文件"的严格策略）。
// 位置：<stateDir>/projects.json（历史版本平铺在数据根，启动时自动迁移，见 internal/settings/state_migrate.go）。
type frpcStore struct {
	filePath string
	mu       sync.RWMutex
	projects map[string]domain.Project // key: project.ID
	loadErr  error                     // 加载失败时禁止以空集合覆盖原文件
}

func newFrpcStore(dir string) *frpcStore {
	s := &frpcStore{
		filePath: filepath.Join(dir, "projects.json"),
		projects: make(map[string]domain.Project),
	}
	s.loadErr = s.load()
	return s
}

func (s *frpcStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var data struct {
		Projects []domain.Project `json:"projects"`
	}
	ok, err := jsonstore.Load(s.filePath, &data)
	if errors.Is(err, jsonstore.ErrCorrupt) || errors.Is(err, jsonstore.ErrEmpty) {
		// 损坏/空文件：报错驻留 loadErr，禁止后续保存把项目库当空库覆盖
		return fmt.Errorf("corrupt frpc projects.json: %w", err)
	}
	if err != nil || !ok {
		return err // 读失败原样上抛；不存在（ok=false 且 err=nil）按空库继续
	}
	// 兼容与解密：自动解密 DPAPI 加密的 Token，无 ID 的项目自动补 ID
	for _, p := range data.Projects {
		if p.ID == "" {
			p.ID = newProjectID()
		}
		// 若 Token 使用 DPAPI 加密，则在内存中解密为明文。
		// 解密失败（如文件拷自另一 Windows 用户/机器）时保留 dpapi: 前缀形态原样驻留：
		// saveLocked 对带前缀的值直通不再二次加密，避免"密文当明文再包一层"把原 Token
		// 永久锁死；此时连接认证会失败，属预期，日志已点名根因。
		if strings.HasPrefix(p.Server.Token, dpapiPrefix) {
			cipher := strings.TrimPrefix(p.Server.Token, dpapiPrefix)
			plain, err := windows.DPAPIDecrypt(cipher)
			if err != nil {
				slog.Error("frpc: DPAPI 解密失败，Token 维持密文形态（可能属另一 Windows 用户/机器），请重新录入",
					"project", p.ID, "name", p.Name, "err", err)
			} else {
				p.Server.Token = string(plain)
			}
		}
		s.projects[p.ID] = p
	}
	return nil
}

func (s *frpcStore) saveLocked(projects map[string]domain.Project) error {
	list := make([]domain.Project, 0, len(projects))
	for _, p := range projects {
		// 落盘保护：克隆对象并将敏感 Token 加密为 DPAPI 密文。
		// 已带 dpapi: 前缀的值（load 解密失败的密文驻留态）直通写回，绝不再包一层；
		// 加密失败拒绝以明文落盘并让整次保存报错——明文泄露比保存失败严重得多，
		// 且 DPAPI 异常通常持续存在，静默降级只会把旧明文重新引回磁盘。
		item := p
		if item.Server.Token != "" && !strings.HasPrefix(item.Server.Token, dpapiPrefix) {
			cipher, err := windows.DPAPIEncrypt([]byte(item.Server.Token))
			if err != nil {
				return fmt.Errorf("项目 %s 的 Token DPAPI 加密失败，拒绝明文落盘: %w", item.ID, err)
			}
			item.Server.Token = dpapiPrefix + cipher
		}
		list = append(list, item)
	}
	data := struct {
		Projects  []domain.Project `json:"projects"`
		UpdatedAt string           `json:"updatedAt"`
	}{list, time.Now().Format("2006-01-02 15:04:05")}

	return jsonstore.Save(s.filePath, data)
}

func newProjectID() string {
	return fmt.Sprintf("p%d", time.Now().UnixNano())
}

// List 返回全部项目（副本）
func (s *frpcStore) List() ([]domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.loadErr != nil {
		return nil, s.loadErr
	}
	list := make([]domain.Project, 0, len(s.projects))
	for _, p := range s.projects {
		list = append(list, cloneProject(p))
	}
	return list, nil
}

// LoadError 返回初始化加载错误；损坏或不可读的项目文件禁止被当成空库覆盖。
func (s *frpcStore) LoadError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// Get 按 ID 查询项目
func (s *frpcStore) Get(id string) (domain.Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[id]
	return cloneProject(p), ok
}

// Save 新建或更新项目（自动维护 CreatedAt/UpdatedAt；空 ID 自动生成）
func (s *frpcStore) Save(p *domain.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loadErr != nil {
		return s.loadErr
	}
	if p == nil {
		return fmt.Errorf("项目不能为空")
	}
	item := cloneProject(*p)
	now := time.Now().Format("2006-01-02 15:04:05")
	if item.ID == "" {
		item.ID = newProjectID()
		item.CreatedAt = now
	} else if _, exists := s.projects[item.ID]; !exists {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	next := s.cloneProjectsLocked()
	next[item.ID] = item
	if err := s.saveLocked(next); err != nil {
		return err
	}
	s.projects = next
	*p = cloneProject(item)
	return nil
}

// Delete 删除项目
func (s *frpcStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loadErr != nil {
		return s.loadErr
	}
	if _, ok := s.projects[id]; !ok {
		return fmt.Errorf("项目 %s 不存在", id)
	}
	next := s.cloneProjectsLocked()
	delete(next, id)
	if err := s.saveLocked(next); err != nil {
		return err
	}
	s.projects = next
	return nil
}

func cloneProject(p domain.Project) domain.Project {
	if p.Proxies != nil {
		p.Proxies = append([]domain.ProxyRule{}, p.Proxies...)
		for i := range p.Proxies {
			if p.Proxies[i].CustomDomains != nil {
				p.Proxies[i].CustomDomains = append([]string{}, p.Proxies[i].CustomDomains...)
			}
		}
	}
	return p
}

func (s *frpcStore) cloneProjectsLocked() map[string]domain.Project {
	out := make(map[string]domain.Project, len(s.projects))
	for id, p := range s.projects {
		out[id] = cloneProject(p)
	}
	return out
}
