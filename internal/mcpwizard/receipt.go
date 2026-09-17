package mcpwizard

import (
	"time"

	"hanxi/internal/jsonstore"
)

// receipt 所有权回执（PLAN §2.5）：<DataDir>/mcp/install.json。
// 只登记"hanxi 亲手写过什么"，四态判定与拒绝覆盖都以它为准；丢失时退化为
// "无回执"保守态（外部同名条目按冲突拒动），不影响客户端文件本身。
type receipt struct {
	Version  int                     `json:"version"`
	Installs map[string]receiptEntry `json:"installs"`
}

type receiptEntry struct {
	ConfigPath  string `json:"configPath"`  // 写入时的目标文件绝对路径
	Fingerprint string `json:"fingerprint"` // 上次写入条目/区块的规范化指纹
	InstalledAt string `json:"installedAt"` // RFC3339
}

func newReceipt() *receipt {
	return &receipt{Version: 1, Installs: map[string]receiptEntry{}}
}

// loadReceipt 容忍缺失（返回空回执）；损坏按空回执继续由调用方覆写修复
// （回执是 hanxi 私有文件，不像用户配置那样 fail-closed，但也不会静默采信半截内容）。
func loadReceipt(path string) *receipt {
	var r receipt
	ok, err := jsonstore.Load(path, &r)
	if !ok || err != nil || r.Installs == nil {
		return newReceipt()
	}
	r.Version = 1
	return &r
}

func saveReceipt(path string, r *receipt) error { return jsonstore.Save(path, r) }

func (r *receipt) set(clientID, configPath, fingerprint string, now time.Time) {
	if r.Installs == nil {
		r.Installs = map[string]receiptEntry{}
	}
	r.Installs[clientID] = receiptEntry{
		ConfigPath:  configPath,
		Fingerprint: fingerprint,
		InstalledAt: now.Format(time.RFC3339),
	}
}

func (r *receipt) remove(clientID string) { delete(r.Installs, clientID) }

func (r *receipt) get(clientID string) (receiptEntry, bool) {
	e, ok := r.Installs[clientID]
	return e, ok
}
