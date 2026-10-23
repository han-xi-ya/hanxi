package sshkeys

// 主机密钥钉定（TOFU）存储。
//
// 为什么本模块要存这个：`deploy` 子包做的事是**往远端写授权**——第一次连上某台
// 机器时如果不记下它的 host key，之后遇到中间人替换就完全无从察觉，而后果是
// "把你的公钥装到了攻击者的机器上"。探测场景（CheckSSH）刻意不校验、只把指纹给人
// 自己去对，那是因为它不产生任何写影响；写操作不能沿用那个宽松口径。
//
// 策略是 TOFU（第一次见到就认）：首次记录并放行，之后**指纹变了就在认证之前拒绝**
// （连密码都不发）。重装服务器导致指纹合法变化的场景，靠界面上的"清除该主机钉定"
// 解决，而不是自动放行——见 service.go 的 ResetHostKeyPin。
//
// 落点 <stateDir>/ssh-hostkey-pins.json：这是"我的机器认得的指纹"，不含任何
// 私钥/口令，与账本同级同严格度（损坏即报错，不以空表覆盖）。

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"hanxi/internal/jsonstore"
)

const pinsFileName = "ssh-hostkey-pins.json"

type pinsFile struct {
	Version int      `json:"version"`
	Pins    []pinRow `json:"pins"`
}

type pinRow struct {
	Addr        string `json:"addr"` // host:port，与 ssh 拨号地址同形
	Fingerprint string `json:"fingerprint"`
}

type pinsStore struct {
	filePath string
	mu       sync.RWMutex
	pins     map[string]string
	loadErr  error
	loaded   bool
}

func newPinsStore(stateDir string) *pinsStore {
	return &pinsStore{
		filePath: filepath.Join(stateDir, pinsFileName),
		pins:     map[string]string{},
	}
}

func (p *pinsStore) ensureLoad() error {
	if p.loaded {
		return p.loadErr
	}
	p.loaded = true
	var data pinsFile
	ok, err := jsonstore.Load(p.filePath, &data)
	if errors.Is(err, jsonstore.ErrCorrupt) || errors.Is(err, jsonstore.ErrEmpty) {
		p.loadErr = fmt.Errorf("钉定文件已损坏，拒绝以空表覆盖 %s", p.filePath)
		return p.loadErr
	}
	if err != nil {
		p.loadErr = err
		return err
	}
	if !ok {
		return nil
	}
	for _, row := range data.Pins {
		if row.Addr == "" || !strings.HasPrefix(row.Fingerprint, "SHA256:") {
			continue // 脏行跳过，不因此拒绝整表
		}
		p.pins[row.Addr] = row.Fingerprint
	}
	return nil
}

// Lookup 实现 deploy.PinStore：无记录返回空串（首次见到的形态）。
func (p *pinsStore) Lookup(addr string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.ensureLoad(); err != nil {
		return "", err
	}
	return p.pins[addr], nil
}

// Record 实现 deploy.PinStore：记录/更新一个地址的指纹。
func (p *pinsStore) Record(addr, fingerprint string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureLoad(); err != nil {
		return err
	}
	if addr == "" || !strings.HasPrefix(fingerprint, "SHA256:") {
		return fmt.Errorf("钉定入参不合形态：addr=%q fingerprint=%q", addr, fingerprint)
	}
	next := make(map[string]string, len(p.pins)+1)
	for k, v := range p.pins {
		next[k] = v
	}
	next[addr] = fingerprint
	if err := p.saveLocked(next); err != nil {
		return err
	}
	p.pins = next
	return nil
}

// Reset 清掉某个地址的钉定（重装服务器后的合法变更出口）。地址不存在不算错。
func (p *pinsStore) Reset(addr string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureLoad(); err != nil {
		return err
	}
	next := make(map[string]string, len(p.pins))
	for k, v := range p.pins {
		if k != addr {
			next[k] = v
		}
	}
	if err := p.saveLocked(next); err != nil {
		return err
	}
	p.pins = next
	return nil
}

// All 返回按地址排序的钉定列表，供界面展示"我认过哪些机器"。
func (p *pinsStore) All() ([]pinRow, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.ensureLoad(); err != nil {
		return nil, err
	}
	out := make([]pinRow, 0, len(p.pins))
	for addr, fp := range p.pins {
		out = append(out, pinRow{Addr: addr, Fingerprint: fp})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr < out[j].Addr })
	return out, nil
}

func (p *pinsStore) saveLocked(pins map[string]string) error {
	rows := make([]pinRow, 0, len(pins))
	for addr, fp := range pins {
		rows = append(rows, pinRow{Addr: addr, Fingerprint: fp})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Addr < rows[j].Addr })
	return jsonstore.Save(p.filePath, pinsFile{Version: 1, Pins: rows})
}
