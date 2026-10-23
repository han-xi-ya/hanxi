package sshkeys

// 两个补位 RPC 的接线测试。逻辑本身在 keyops 里已测透，这里只验**父层加的三件事**：
// 走调用门、定位到正确的密钥目录、把子包的错误哨兵翻成枚举码。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/sshkeys/keyops"
)

func newTestService(t *testing.T) (*SshKeysService, string) {
	t.Helper()
	fakeHome := t.TempDir()
	keyDir := filepath.Join(fakeHome, ".ssh")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatalf("造假目录失败: %v", err)
	}
	prev := userHomeDir
	userHomeDir = func() (string, error) { return fakeHome, nil }
	t.Cleanup(func() { userHomeDir = prev })
	return &SshKeysService{
		prober: newProber(),
		holder: extapi.NewLeaseHolder(ID),
		ledger: newLedger(t.TempDir()),
		pins:   newPinsStore(t.TempDir()),
	}, keyDir
}

func writeTestKey(t *testing.T, dir, base, passphrase string) string {
	t.Helper()
	res, err := keyops.Generate(dir, keyops.GenerateParams{
		Name: base, Type: "ed25519", Comment: base + "@test", Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("生成测试钥匙失败: %v", err)
	}
	// Generate 总是成对写；本文件的用例都要"缺公钥半边"的起点，所以一律摘掉 .pub
	// （带口令那把同时也就成了"加密 + 无公钥"，正是注释取不回、又最需要补公钥的形态）。
	_ = os.Remove(res.PublicPath)
	return res.PrivatePath
}

func TestExportPublicKeyRPC(t *testing.T) {
	svc, dir := newTestService(t)
	priv := writeTestKey(t, dir, "id_nopub", "") // 已把 .pub 摘掉，模拟"从服务器下回来的私钥"

	res, err := svc.ExportPublicKey(ExportPubRequest{Path: priv, Comment: "laptop"})
	if err != nil {
		t.Fatalf("补公钥失败: %v", err)
	}
	if res.Code != codeOK || res.Fingerprint == "" {
		t.Fatalf("回执不合形: %+v", res)
	}
	if !strings.HasSuffix(res.Path, "id_nopub") {
		t.Errorf("Path 应回私钥真身路径，实得 %q", res.Path)
	}
	if _, err := os.Stat(priv + ".pub"); err != nil {
		t.Fatalf("公钥未落盘: %v", err)
	}
	// 新建出来的公钥必须是**结构化字段**能拿到，不能只活在 Message 散文里——
	// 否则界面上没有可核对、可复制的目标文件。
	if !strings.HasSuffix(res.PublicPath, "id_nopub.pub") {
		t.Errorf("PublicPath 应指向新建的公钥，实得 %q", res.PublicPath)
	}

	// 再点一次：已存在必须拒，且不许改动刚写出的那份。
	if _, err := svc.ExportPublicKey(ExportPubRequest{Path: priv, Comment: "other"}); err == nil {
		t.Error("已有公钥时第二次必须被拒")
	}
}

// TestExportPublicKeyOnEncryptedKey 走一遍**带口令**的私钥：这是这个功能存在的
// 主要场景（口令私钥拿不到注释，但公钥段是明文），也是最容易只测明文的一支。
func TestExportPublicKeyOnEncryptedKey(t *testing.T) {
	svc, dir := newTestService(t)
	priv := writeTestKey(t, dir, "id_guarded", "s3cret-pass")

	res, err := svc.ExportPublicKey(ExportPubRequest{Path: priv})
	if err != nil {
		t.Fatalf("带口令私钥也应能补出公钥: %v", err)
	}
	if res.Fingerprint == "" {
		t.Fatal("该给出指纹")
	}
	before, _ := os.ReadFile(priv)
	if len(before) == 0 || !strings.Contains(string(before), "OPENSSH PRIVATE KEY") {
		t.Error("私钥文件被破坏")
	}
	// 口令不得从任何出口流出（回执文案与结构体都算出口）。
	blob := mustJSON(t, res) + res.Message
	if strings.Contains(blob, "s3cret-pass") {
		t.Fatal("口令出现在返回值里")
	}
}

func TestDeletePublicKeyRPC(t *testing.T) {
	svc, dir := newTestService(t)
	// 造一把完整的，再删掉私钥半边 → 真孤儿公钥
	res, err := keyops.Generate(dir, keyops.GenerateParams{Name: "id_pair", Type: "ed25519"})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if err := os.Remove(res.PrivatePath); err != nil {
		t.Fatalf("删私钥失败: %v", err)
	}

	if _, err := svc.DeletePublicKey(DeletePubRequest{Path: res.PublicPath, Confirm: "id_pai"}); err == nil {
		t.Error("回显不符必须拒")
	}
	if _, err := os.Stat(res.PublicPath); err != nil {
		t.Fatalf("被拒时不该动文件: %v", err)
	}
	out, err := svc.DeletePublicKey(DeletePubRequest{Path: res.PublicPath, Confirm: "id_pair.pub"})
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if !strings.HasSuffix(out.PublicPath, "id_pair.pub") {
		t.Errorf("删除公钥也应回结构化路径，实得 %q", out.PublicPath)
	}
	if out.Code != codeOK || !strings.Contains(out.Message, "不会被回收") {
		t.Errorf("回执应如实说明远端授权不受影响，实得 %+v", out)
	}
	if _, err := os.Stat(res.PublicPath); !os.IsNotExist(err) {
		t.Error("应已删除")
	}
}
