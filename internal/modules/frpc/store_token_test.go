package frpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/domain"
)

// Token 落盘形态的用例（ADR-0004 去 DPAPI）。核心断言只有一条主线：
// **数据根整目录搬到另一台机器后仍然可用**——这既是明文落盘的全部理由，
// 也是历史密文必须作废而不是"留着再看"的理由。

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

// TestStorePersistsTokenAsPlaintext 明文原样落盘——换机/换 Windows 账户后可直接续用。
// 这条断言就是"便携优先"的实现锁定：谁哪天想加回一层本机绑定加密，这里先红。
func TestStorePersistsTokenAsPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	s := newFrpcStore(dir)

	const token = "s3cr3t-with-@-and:" // 顺带覆盖特殊字符不被转义改写
	p := &domain.Project{Name: "proj", Server: domain.ServerConfig{ServerAddr: "a", Token: token}}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := readPersistedToken(t, path, p.ID); got != token {
		t.Errorf("Token 应逐字明文落盘，实得 %q", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), legacyDPAPIPrefix) {
		t.Errorf("新写入的数据不该再带历史密文前缀：%s", raw)
	}
}

// TestStoreVoidLegacyDPAPIToken 锁两件事：
// ① 历史 `dpapi:` 密文在 load 时判失效并清空——仓内已无解密能力，留着它只会让一个
//
//	非空无意义的字符串去连服务器，用户看到的失败是"认证不通过"而不是"这里没填"；
//
// ② 清空后再次保存，文件里不再出现该前缀（不会被"驻留—直通写回"养在库里）。
func TestStoreVoidLegacyDPAPIToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	const legacy = legacyDPAPIPrefix + "QnlWS0NPV0xITFlTT05HSFpFU0xFS0hXSkZMSFdLRVpMSFdLRUtaTEhXRUtaTEhX"
	raw := `{"projects":[{"id":"p1","name":"旧项目","server":{"addr":"a","token":"` + legacy + `"}}],"updatedAt":"x"}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newFrpcStore(dir)
	if err := s.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	p, ok := s.projects["p1"]
	if !ok {
		t.Fatal("项目应被读出")
	}
	if p.Server.Token != "" {
		t.Errorf("历史密文应清空待重填，实得 %q", p.Server.Token)
	}
	if strings.HasPrefix(p.Server.Token, legacyDPAPIPrefix) {
		t.Error("密文形态不得驻留在 Token 位上")
	}

	// 再存一次：作废结果落成明文空值，前缀彻底离开文件
	if err := s.Save(&p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), legacyDPAPIPrefix) {
		t.Errorf("保存后文件里不该再有历史密文前缀：%s", after)
	}
}
