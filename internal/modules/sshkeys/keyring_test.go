package sshkeys

// 台账归组测试。**为什么按"一把钥匙"而不是"一个文件"列，是用机主 2026-10-23 的
// 一次误删换来的**：`id_x` 与 `id_x.pub` 同指纹各占一行，读起来像两把钥匙，
// 于是"删哪一把"变成猜。这里的断言就是那条结构规则的锁。

import (
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"hanxi/internal/extapi"
)

// writeKeyPair 真生成一把 ed25519 钥匙（passphrase 非空即加口令），
// 返回私钥 PEM 原文供"内容不得外泄"的断言使用。
func writeKeyPair(t *testing.T, dir, base, passphrase string, withPub bool) string {
	t.Helper()
	// 返回顺序是 (PublicKey, PrivateKey, error)——先公后私；接反会报
	// unsupported key type ed25519.PublicKey（本仓踩过）。
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}

	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, base+"-comment")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, base+"-comment", []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("导出私钥失败: %v", err)
	}
	pemBytes := pem.EncodeToMemory(block)

	pPath := filepath.Join(dir, base)
	if err := os.WriteFile(pPath, pemBytes, 0o600); err != nil {
		t.Fatalf("写私钥失败: %v", err)
	}
	if withPub {
		sshPub, kErr := ssh.NewPublicKey(pub)
		if kErr != nil {
			t.Fatalf("转 ssh 公钥失败: %v", kErr)
		}
		line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " " + base + "@hanxi-test\n"
		if wErr := os.WriteFile(pPath+".pub", []byte(line), 0o644); wErr != nil {
			t.Fatalf("写公钥失败: %v", wErr)
		}
	}
	return string(pemBytes)
}

func find(entries []KeyEntry, name string) (KeyEntry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return KeyEntry{}, false
}

func TestScanKeyDirGroupsByKeyNotByFile(t *testing.T) {
	dir := t.TempDir()
	plainPEM := writeKeyPair(t, dir, "id_pair", "", true) // 私钥 + 配套公钥
	writeKeyPair(t, dir, "id_guard", "s3cret-pp", false)  // 有口令、无 .pub
	writeKeyPair(t, dir, "id_orphan", "", true)           // 先成对写…
	if err := os.Remove(filepath.Join(dir, "id_orphan")); err != nil {
		t.Fatalf("制造孤儿公钥失败: %v", err)
	} // …再删私钥，留孤公

	for _, noise := range []string{"config", "known_hosts", "known_hosts.old", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, noise), []byte("Host example\n"), 0o644); err != nil {
			t.Fatalf("写干扰文件失败: %v", err)
		}
	}

	got, err := scanKeyDir(dir)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	// **三把钥匙，不是四行。**
	if len(got) != 3 {
		t.Fatalf("应按钥匙归组为 3 条，实得 %d 条：%v", len(got), keyNames(got))
	}
	for _, e := range got {
		if strings.HasSuffix(e.Name, ".pub") {
			t.Errorf("出现了以 .pub 为名的独立条目 %q（应并入基名）", e.Name)
		}
	}

	pair, ok := find(got, "id_pair")
	if !ok {
		t.Fatalf("缺 id_pair 条目：%v", keyNames(got))
	}
	if !pair.HasPrivate || !pair.HasPublic {
		t.Errorf("私钥+公钥应合成一条，实得 %+v", pair)
	}
	if pair.Fingerprint == "" || pair.Type != "ssh-ed25519" {
		t.Errorf("应给出指纹与算法，实得 %+v", pair)
	}
	if pair.Comment != "id_pair@hanxi-test" {
		t.Errorf("注释应取自 .pub 尾字段，实得 %q", pair.Comment)
	}
	if pair.Guarded || !pair.GuardKnown {
		t.Errorf("未加密应 Guarded=false/Known=true，实得 %+v", pair)
	}
	if pair.Bits != 256 {
		t.Errorf("ed25519 位数应为 256，实得 %d", pair.Bits)
	}

	guard, ok := find(got, "id_guard")
	if !ok || !guard.HasPrivate || guard.HasPublic {
		t.Fatalf("缺配套公钥的私钥应成一条 HasPublic=false：%+v", guard)
	}
	if !guard.Guarded || !guard.GuardKnown {
		t.Errorf("有口令应 Guarded=true/Known=true，实得 %+v", guard)
	}
	// 诚实性：没有 .pub 且私钥加密 → 指纹必须为空，**绝不为"界面好看"去解私钥**。
	if guard.Fingerprint != "" {
		t.Errorf("加密私钥不该被解开取指纹，实得 %q", guard.Fingerprint)
	}

	orphan, ok := find(got, "id_orphan")
	if !ok || orphan.HasPrivate || !orphan.HasPublic {
		t.Fatalf("孤儿公钥应成一条 HasPrivate=false：%+v", orphan)
	}
	if orphan.Fingerprint == "" {
		t.Error("孤儿公钥本就能给出指纹，不该空白")
	}

	for _, noise := range []string{"config", "known_hosts", "known_hosts.old", "README.md"} {
		if _, j := find(got, noise); j {
			t.Errorf("非密钥文件 %q 混进了台账", noise)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Name > got[i].Name {
			t.Fatalf("未按名升序（前端列表会抖动）：%v", keyNames(got))
		}
	}

	// 硬边界：私钥正文（去掉 PEM 包装后的 base64 体）一个字节都不许出现在返回值里。
	blob := mustJSON(t, got)
	if body := pemBody(plainPEM); body != "" && strings.Contains(blob, body) {
		t.Fatal("私钥正文出现在台账返回值里（泄露）")
	}
	if strings.Contains(blob, "PRIVATE KEY") {
		t.Fatal("返回值里出现了私钥 PEM 标记")
	}
}

// pemBody 取 PEM 的 base64 正文（去掉首尾标记行与所有空白）。
func pemBody(s string) string {
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.HasPrefix(l, "-----") {
			continue
		}
		kept = append(kept, strings.TrimSpace(l))
	}
	return strings.Join(kept, "")
}

func keyNames(got []KeyEntry) []string {
	out := make([]string, 0, len(got))
	for _, e := range got {
		out = append(out, e.Name)
	}
	return out
}

// TestListKeysAnnotatesUsedBy 钉的是"被谁在用"那一列——它是防误删的唯一现场信息。
// KeyPath 故意用 ~ 形态：不展开就会漏报，而漏报正好放过一次误删。
func TestListKeysAnnotatesUsedBy(t *testing.T) {
	// 替换的是**家目录接缝本身**，而不是 sshHomeDir：清单里的 KeyPath 用 `~` 形态，
	// 展开与扫描必须同源。上一版测试只换 sshHomeDir，于是 "~" 展开到真家目录、
	// 钥匙在临时目录，"被谁在用"直接漏报——正是这个字段最危险的失效形态。
	fakeHome := t.TempDir()
	keyDir := filepath.Join(fakeHome, ".ssh")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatalf("建假密钥目录失败: %v", err)
	}
	prev := userHomeDir
	userHomeDir = func() (string, error) { return fakeHome, nil }
	t.Cleanup(func() { userHomeDir = prev })

	writeKeyPair(t, keyDir, "id_used", "", true)
	writeKeyPair(t, keyDir, "id_free", "", true)

	l := newLedger(t.TempDir())
	if err := l.add(HostEntry{Alias: "nas01", Host: "10.0.0.8", Port: 22, User: "ops",
		KeyPath: "~/.ssh/id_used", Enabled: true}); err != nil {
		t.Fatalf("入册失败: %v", err)
	}

	svc := &SshKeysService{prober: newProber(), holder: extapi.NewLeaseHolder(ID), ledger: l}
	got, err := svc.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys 失败: %v", err)
	}
	used, _ := find(got, "id_used")
	free, _ := find(got, "id_free")
	if len(used.UsedBy) != 1 || used.UsedBy[0] != "nas01" {
		t.Errorf("id_used 应被 nas01 引用，实得 %+v", used.UsedBy)
	}
	if len(free.UsedBy) != 0 {
		t.Errorf("id_free 不该有引用，实得 %+v", free.UsedBy)
	}
}

func TestScanKeyDirMissingDirIsNotError(t *testing.T) {
	got, err := scanKeyDir(filepath.Join(t.TempDir(), "not-there"))
	if err != nil {
		t.Fatalf("目录不存在应回空态而非错误，实得 err=%v", err)
	}
	if len(got) != 0 {
		t.Fatalf("目录不存在应回空台账，实得 %+v", got)
	}
}

// TestDetectGuardThreeStates 锁"第三态不许塌"：认不出形态时报"判不出来"，
// 既不能报"无口令"（会让人以为不必加固），也不能报"有口令"（会让人以为自动化用不了）。
func TestDetectGuardThreeStates(t *testing.T) {
	cases := []struct {
		name        string
		head        string
		wantGuarded bool
		wantKnown   bool
	}{
		{"垃圾输入非密钥", "hello world", false, false},
		{"老式 Proc-Type 头", "-----BEGIN RSA PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC,00\n", true, true},
		{"只有头没有正文", "-----BEGIN OPENSSH PRIVATE KEY-----\n", false, false},
		{"base64 非 openssh 魔数", "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAAAAAAAAAA\n-----END OPENSSH PRIVATE KEY-----\n", false, false},
	}
	for _, c := range cases {
		g, k := detectGuard([]byte(c.head))
		if g != c.wantGuarded || k != c.wantKnown {
			t.Errorf("%s: 期望 guarded=%v known=%v，实得 %v/%v", c.name, c.wantGuarded, c.wantKnown, g, k)
		}
	}
	dir := t.TempDir()
	plain := writeKeyPair(t, dir, "k1", "", false)
	guarded := writeKeyPair(t, dir, "k2", "pp-pass", false)
	if g, k := detectGuard([]byte(plain)); g || !k {
		t.Errorf("真未加密私钥应 false/true，实得 %v/%v", g, k)
	}
	if g, k := detectGuard([]byte(guarded)); !g || !k {
		t.Errorf("真加密私钥应 true/true，实得 %v/%v", g, k)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return string(b)
}

// TestUsedByKnownSeparatesJudgedFromUnjudged 锁"未判 ≠ 没人用"。
// 这句要单独钉：因为"清单里没有主机用它"是删除决策现场最容易被引用的一句话，
// 把未判渲染成没人用，等于给误删发通行证。
func TestUsedByKnownSeparatesJudgedFromUnjudged(t *testing.T) {
	fakeHome := t.TempDir()
	keyDir := filepath.Join(fakeHome, ".ssh")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatalf("建假目录失败: %v", err)
	}
	prev := userHomeDir
	userHomeDir = func() (string, error) { return fakeHome, nil }
	t.Cleanup(func() { userHomeDir = prev })

	writeKeyPair(t, keyDir, "id_judged", "", true)
	writeKeyPair(t, keyDir, "id_orphanpub", "", true)
	if err := os.Remove(filepath.Join(keyDir, "id_orphanpub")); err != nil {
		t.Fatalf("制造孤儿公钥失败: %v", err)
	}

	svc := &SshKeysService{prober: newProber(), holder: extapi.NewLeaseHolder(ID), ledger: newLedger(t.TempDir())}
	got, err := svc.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys 失败: %v", err)
	}

	judged, ok := find(got, "id_judged")
	if !ok {
		t.Fatalf("缺 id_judged：%v", keyNames(got))
	}
	// 有私钥 + 清单读得动 → **判过了**，空名单是事实"没人用"。
	if !judged.UsedByKnown || len(judged.UsedBy) != 0 {
		t.Errorf("有私钥的行应 UsedByKnown=true 且名单为空，实得 %+v", judged)
	}
	orphan, ok := find(got, "id_orphanpub")
	if !ok {
		t.Fatalf("缺 id_orphanpub：%v", keyNames(got))
	}
	// 仅公钥的行没有可比对的私钥路径 → 未判，前端不得渲染"没有主机用它"。
	if orphan.UsedByKnown {
		t.Errorf("孤儿公钥行应是「未判」（UsedByKnown=false），实得 %+v", orphan)
	}
}
