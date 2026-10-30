package aichat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// store 是会话**账本**（id/title/时间戳），落装配根的 StateDir。
// 对话正文不在这里（见 SessionRow 注释）——本文件管的是"有哪些会话"，
// 不是"会话里说了什么"；两者存储语义不同，混存会造出第二套 transcript。

type store struct {
	mu   sync.Mutex
	path string
	rows map[string]*SessionRow
}

type storeFile struct {
	Sessions []*SessionRow `json:"sessions"`
}

func newStore(stateDir string) (*store, error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 aichat 账本目录失败: %w", err)
	}
	s := &store{path: filepath.Join(stateDir, "aichat-sessions.json"), rows: map[string]*SessionRow{}}
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读会话账本失败: %w", err)
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		// 账本坏了不静默清空：那是用户的全部会话名录，清空 = 二次伤害。
		// 报错上抛，让用户自己决定留档还是重开。
		return nil, fmt.Errorf("会话账本 JSON 解析失败（不自动清空，请人工处置 %s）: %w", s.path, err)
	}
	for _, r := range f.Sessions {
		if r != nil && r.ID != "" {
			s.rows[r.ID] = r
		}
	}
	return s, nil
}

func (s *store) list() []*SessionRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*SessionRow, 0, len(s.rows))
	for _, r := range s.rows {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func (s *store) get(id string) (*SessionRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

func (s *store) put(r *SessionRow) error {
	s.mu.Lock()
	cp := *r
	s.rows[cp.ID] = &cp
	err := s.persistLocked()
	s.mu.Unlock()
	return err
}

func (s *store) remove(id string) (bool, error) {
	s.mu.Lock()
	_, ok := s.rows[id]
	if ok {
		delete(s.rows, id)
	}
	err := s.persistLocked()
	s.mu.Unlock()
	return ok, err
}

// persistLocked 原子落盘：tmp + rename。仓内同名原子写已散在三处
// （mcpwizard/sshkeys/settings，#131 同款"孤例并立"信号），抽取归公共件
// 是重构线的活，本轮不新开第四种**语义**——姿势与既有三份一致。
func (s *store) persistLocked() error {
	f := storeFile{Sessions: make([]*SessionRow, 0, len(s.rows))}
	for _, r := range s.rows {
		cp := *r
		f.Sessions = append(f.Sessions, &cp)
	}
	sort.Slice(f.Sessions, func(i, j int) bool { return f.Sessions[i].CreatedAt < f.Sessions[j].CreatedAt })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
