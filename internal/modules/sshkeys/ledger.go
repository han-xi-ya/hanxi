package sshkeys

// 主机账本（清单）：本模块的唯一权威落盘处。
//
// 一条贯穿性的口径：**账本权威在这里，`~/.ssh/hanxi.conf` 只是渲染产物**——
// 改主机只改这里，产物由 export.go 幂等重生成。这与 scripts/cataloggen 的关系
// 同构（源码是权威、catalog.go 是产物），目的都是"不让同一件事有两份可写的真相"。
//
// 校验单源：字段合法性一律走 packages/go/sshconf 的 Validate/ValidateAlias，
// **本文件不重复写一份校验规则**。两处各写一套，早晚一套严一套松，
// 界面上就能存进一条渲染时会炸的记录。

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"hanxi/internal/jsonstore"
	"hanxi/packages/go/sshconf"
)

const ledgerFileName = "ssh-keys.json"

// ErrAliasExists 新增时别名已被占用。
var ErrAliasExists = errors.New("该别名已存在于清单中")

// ErrAliasNotFound 按别名改删时目标不存在。
var ErrAliasNotFound = errors.New("清单里没有该别名")

type ledgerFile struct {
	Version int         `json:"version"`
	Hosts   []HostEntry `json:"hosts"`
}

// ledger 主机账本存储。严格型（对齐 frpc store 的口径）：
// 文件损坏时**报错驻留**，禁止后续保存把坏文件当空账本覆盖掉——
// 用户看到的后果应该是"改不动、且告诉你为什么"，而不是"清单突然空了"。
type ledger struct {
	filePath string
	mu       sync.RWMutex
	hosts    map[string]HostEntry
	loadErr  error
	loaded   bool
}

func newLedger(stateDir string) *ledger {
	return &ledger{
		filePath: filepath.Join(stateDir, ledgerFileName),
		hosts:    map[string]HostEntry{},
	}
}

// ensureLoad 首次访问时读盘；损坏驻留后每次访问都继续报同一个错。
func (l *ledger) ensureLoad() error {
	if l.loaded {
		return l.loadErr
	}
	l.loaded = true
	var data ledgerFile
	ok, err := jsonstore.Load(l.filePath, &data)
	if errors.Is(err, jsonstore.ErrCorrupt) || errors.Is(err, jsonstore.ErrEmpty) {
		// jsonstore 的口径：损坏/空时目标结构体可能被**部分填充**，所以只在
		// ok==true 时才采信 data——这里 ok 为 false，data 直接弃用。
		l.loadErr = fmt.Errorf("清单文件已损坏，拒绝以空账本覆盖 %s——请先手工改名该文件再重开", l.filePath)
		return l.loadErr
	}
	if err != nil {
		l.loadErr = err
		return err
	}
	if !ok {
		return nil // 不存在 = 空账本，正常起始态
	}
	for _, h := range data.Hosts {
		// 盘上数据同样要过一遍校验：文件可能被手改过，脏数据不该被"默认信任"再写回去。
		// 但也不因一条坏记录就拒绝整个账本——跳过并在首次保存时按当前内存态重写。
		if err := validateEntry(h); err != nil {
			continue
		}
		l.hosts[h.Alias] = h
	}
	return nil
}

// List 返回按别名升序的账本副本（深拷贝：调用方改不动内部状态）。
func (l *ledger) list() ([]HostEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.ensureLoad(); err != nil {
		return nil, err
	}
	out := make([]HostEntry, 0, len(l.hosts))
	for _, h := range l.hosts {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out, nil
}

// Add 新增。别名重复直接拒——**这条不是洁癖**：sshconf 的最后一道自证断言
// "产物里的 Host 段头数量 == 本次渲染项数"，重复别名会让两条记录渲染出同名段，
// 让那道防线要么误报要么形同虚设。
func (l *ledger) add(h HostEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ensureLoad(); err != nil {
		return err
	}
	if err := validateEntry(h); err != nil {
		return err
	}
	if _, exists := l.hosts[h.Alias]; exists {
		return fmt.Errorf("%w：%s", ErrAliasExists, h.Alias)
	}
	next := cloneHosts(l.hosts)
	next[h.Alias] = h
	if err := l.saveLocked(next); err != nil {
		return err
	}
	l.hosts = next
	return nil
}

// Update 按别名整条覆盖（别名本身不可改——改了就是另一把钥匙的账，
// 让用户"改别名 + 重排"两步走，比静默搬掉一条记录更容易看清）。
func (l *ledger) update(h HostEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ensureLoad(); err != nil {
		return err
	}
	if err := validateEntry(h); err != nil {
		return err
	}
	if _, exists := l.hosts[h.Alias]; !exists {
		return fmt.Errorf("%w：%s", ErrAliasNotFound, h.Alias)
	}
	next := cloneHosts(l.hosts)
	next[h.Alias] = h
	if err := l.saveLocked(next); err != nil {
		return err
	}
	l.hosts = next
	return nil
}

// Delete 移除一条。**只动账本，不动 ~/.ssh 下的产物与磁盘文件**——
// 产物由 export.go 重生成时自然少了这一段，盘上的私钥更不该被清单删除顺带清掉。
func (l *ledger) remove(alias string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ensureLoad(); err != nil {
		return err
	}
	if _, exists := l.hosts[alias]; !exists {
		return fmt.Errorf("%w：%s", ErrAliasNotFound, alias)
	}
	next := cloneHosts(l.hosts)
	delete(next, alias)
	if err := l.saveLocked(next); err != nil {
		return err
	}
	l.hosts = next
	return nil
}

// saveLocked 先落盘、成功才换内存（事务式）：落盘失败时内存保持原样，
// 不会出现"界面显示已保存、盘上还是旧的"这种半截状态。
func (l *ledger) saveLocked(hosts map[string]HostEntry) error {
	out := make([]HostEntry, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return jsonstore.Save(l.filePath, ledgerFile{Version: 1, Hosts: out})
}

// validateEntry 把字段校验完全委托给 sshconf——单一规则源，见文件头。
func validateEntry(h HostEntry) error {
	if err := sshconf.ValidateAlias(h.Alias); err != nil {
		return err
	}
	return sshconf.Validate(sshconf.Host{
		Alias:        h.Alias,
		HostName:     strings.TrimSpace(h.Host),
		Port:         h.Port,
		User:         strings.TrimSpace(h.User),
		IdentityFile: strings.TrimSpace(h.KeyPath),
		Note:         strings.TrimSpace(h.Note),
		Enabled:      h.Enabled,
	})
}

func cloneHosts(src map[string]HostEntry) map[string]HostEntry {
	dst := make(map[string]HostEntry, len(src)+1)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// toConfHosts 账本 → 渲染输入。转换只在这里做一次，export.go 不再各自映射。
func toConfHosts(items []HostEntry) []sshconf.Host {
	out := make([]sshconf.Host, 0, len(items))
	for _, h := range items {
		out = append(out, sshconf.Host{
			Alias:        h.Alias,
			HostName:     h.Host,
			Port:         h.Port,
			User:         h.User,
			IdentityFile: h.KeyPath,
			Note:         h.Note,
			Enabled:      h.Enabled,
		})
	}
	return out
}
