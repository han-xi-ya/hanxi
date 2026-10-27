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
)

const legacyDPAPIPrefix = "dpapi:" // 历史密文标记，仅用于识别并作废（ADR-0004）

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
	// 兼容：无 ID 的项目自动补 ID；历史 DPAPI 密文一律判为失效凭据。
	for _, p := range data.Projects {
		if p.ID == "" {
			p.ID = newProjectID()
		}
		// ADR-0004：数据根要能整目录搬去另一台机器继续用，而 DPAPI 密文绑定"当前
		// Windows 用户 + 当前机器"，搬过去必然解不开，故仓内已无解密能力。带历史前缀
		// 的值**清空而非原样驻留**——没有解密能力之后还把它留在 Token 位上，等于让一个
		// 非空却无意义的字符串去连服务器，用户看到的是"认证失败"，而不是"这里没填"；
		// 清空后该格为空，前端引导重填，根因也在日志里点名。
		if strings.HasPrefix(p.Server.Token, legacyDPAPIPrefix) {
			slog.Warn("frpc: Token 为历史 DPAPI 密文，已按失效处理并清空，请重新录入",
				"project", p.ID, "name", p.Name)
			p.Server.Token = ""
		}
		s.projects[p.ID] = p
	}
	return nil
}

func (s *frpcStore) saveLocked(projects map[string]domain.Project) error {
	// 明文落盘（ADR-0004）：便携性优先于本机绑定加密。风险（数据根若被 NAS/网盘同步，
	// Token 即明文可读）由该 ADR 承担并如实记录，与 clipboard v1.6、Memo 库同谱口径。
	list := make([]domain.Project, 0, len(projects))
	for _, p := range projects {
		list = append(list, p)
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
