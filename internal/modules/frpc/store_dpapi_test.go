package frpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/domain"
	"hanxi/internal/platform/windows"
)

// readPersistedToken 从落盘 JSON 里取出指定项目的 token 原文。
func readPersistedToken(t *testing.T, path, id string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Projects []domain.Project `json:"projects"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	for _, p := range data.Projects {
		if p.ID == id {
			return p.Server.Token
		}
	}
	t.Fatalf("落盘数据中找不到项目 %s", id)
	return ""
}

// TestStoreKeepsUndecryptableToken 锁 BUG-031 回归：DPAPI 解密失败（如 projects.json
// 拷自另一 Windows 用户/机器）时，内存值保留 dpapi: 前缀形态，保存直通——
// 旧实现会把密文串当明文再加密一层，原 Token 永久锁死。
func TestStoreKeepsUndecryptableToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	const fake = "dpapi:AGkAbgB2AGEAbABpAGQALQBiAGwAbwBiAA==" // 前缀合法、密文非本机 DPAPI 产物
	raw, err := json.Marshal(map[string]any{
		"projects": []map[string]any{
			{"id": "p1", "name": "x", "server": map[string]any{"token": fake}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	s := newFrpcStore(dir)
	if err := s.LoadError(); err != nil {
		t.Fatalf("load 应容忍解密失败而非报错: %v", err)
	}
	p, ok := s.Get("p1")
	if !ok {
		t.Fatal("项目应存在")
	}
	if p.Server.Token != fake {
		t.Fatalf("解密失败的 Token 应原样驻留，得到 %q", p.Server.Token)
	}

	if err := s.Save(&p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := readPersistedToken(t, path, "p1")
	if got != fake {
		t.Errorf("带前缀的密文应直通落盘，不得二次包裹或降级明文，得到 %q", got)
	}
	if strings.HasPrefix(strings.TrimPrefix(got, dpapiPrefix), dpapiPrefix) {
		t.Error("出现双重 dpapi: 包裹（回归到二次加密密文的锁死路径）")
	}
}

// TestStoreEncryptsPlaintextToken 守正常路径：明文 Token 保存必经 DPAPI 加密落盘，
// 且解密可还原（同时验证保存报错路径不误伤正常环境）。
func TestStoreEncryptsPlaintextToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	s := newFrpcStore(dir)

	p := &domain.Project{Name: "proj", Server: domain.ServerConfig{ServerAddr: "a", Token: "s3cr3t"}}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := readPersistedToken(t, path, p.ID)
	if !strings.HasPrefix(got, dpapiPrefix) {
		t.Fatalf("明文 Token 必须加密落盘（当前环境 DPAPI 应可用），得到 %q", got)
	}
	plain, err := windows.DPAPIDecrypt(strings.TrimPrefix(got, dpapiPrefix))
	if err != nil || string(plain) != "s3cr3t" {
		t.Fatalf("密文应可还原: %v", err)
	}
}
