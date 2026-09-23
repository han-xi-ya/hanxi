package version

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

// ---------- 通道与形状口径 ----------

func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "termora-2.0.0-beta.16-windows-aarch64.exe"},
		{Name: "termora-2.0.0-beta.16-windows-aarch64.zip"},
		{Name: "termora-2.0.0-beta.16-windows-x86-64.exe"},
		{Name: "termora-2.0.0-beta.16-windows-x86-64.zip"},
		{Name: "termora-2.0.0-beta.16-linux-x86-64.tar.gz"},
	}
	if a, ok := findPortableAsset(assets, "2.0.0-beta.16"); !ok || a.Name != "termora-2.0.0-beta.16-windows-x86-64.zip" {
		t.Fatalf("应精确命中 x86-64 便携 zip（排除 .exe 安装器/aarch64/其它系统）: %#v", a)
	}
	if _, ok := findPortableAsset(assets[:3], "2.0.0-beta.16"); ok {
		t.Fatal("无 windows-x86-64 zip 的 release 必须拒入列")
	}
}

func TestSemverTagFilter(t *testing.T) {
	for tag, want := range map[string]bool{
		"2.0.0-beta.16": true,  // 2.x 主干通道（事实稳定版）
		"1.0.0":         true,  // 旧世代稳定
		"v2.0.0":        false, // 上游 tag 无 v 前缀
		"nightly":       false,
		"2.0":           false,
	} {
		if got := semverTag.MatchString(tag); got != want {
			t.Errorf("semverTag(%q) = %v, want %v", tag, got, want)
		}
	}
}

func TestParseAssetDigest(t *testing.T) {
	if _, _, ok := parseAssetDigest("sha256:" + strings.Repeat("a", 64)); !ok {
		t.Fatal("合法 sha256 摘要必须接受")
	}
	for _, bad := range []string{"", "sha256:short", "md5:" + strings.Repeat("f", 32), "garbage"} {
		if _, _, ok := parseAssetDigest(bad); ok {
			t.Errorf("非法摘要必须拒: %q", bad)
		}
	}
}

func TestNormalizeFileVersionTermora(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"2.0.0.16", "2.0.0.16", true}, // beta 构建号不得截掉（与远程 tag 不假装精确）
		{"2.0.0.0", "2.0.0", true},     // 仅修剪尾部 .0 冗余
		{"2.7.0", "2.7.0", true},
		{"2.0.0-beta", "", false}, // PE 版本形不含预发布段（jpackage 数值化）
		{"1.0", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeFileVersion(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("normalizeFileVersion(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// ---------- 布局与令牌 ----------

func TestFindPayloadExeShapes(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, payloadDir)
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, exeName), []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, ok := findPayloadExe(dir); !ok || got != filepath.Join(nested, exeName) {
		t.Fatalf("jpackage 嵌套根应命中: %q ok=%v", got, ok)
	}
	flatDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(flatDir, exeName), []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := findPayloadExe(flatDir); !ok {
		t.Fatal("平铺导入件必须可识别")
	}
	if _, ok := findPayloadExe(t.TempDir()); ok {
		t.Fatal("空目录不得判在场")
	}
}

func TestImportTokenFallback(t *testing.T) {
	junk := filepath.Join(t.TempDir(), "Termora.exe")
	if err := os.WriteFile(junk, []byte("not-a-pe"), 0644); err != nil {
		t.Fatal(err)
	}
	tok := importToken(junk)
	if !strings.HasPrefix(tok, "imported-") {
		t.Fatalf("PE 版本读不出必须 imported-时间戳 兜底: %q", tok)
	}
	if err := artifact.ValidateVersionToken(tok); err != nil {
		t.Fatalf("兜底令牌必须过白名单: %v", err)
	}
}

// ---------- 账本口径 ----------

func installFixture(t *testing.T, root, token string, mm moduleMeta, meta artifact.Meta) string {
	t.Helper()
	dir := filepath.Join(root, treeEntryName+"_"+token)
	payload := filepath.Join(dir, payloadDir)
	if err := os.MkdirAll(filepath.Join(payload, "app"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, exeName), []byte("MZfake-exe-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	meta.Schema = artifact.DefaultSchema
	meta.Tool = treeEntryName
	meta.Version = token
	if meta.InstalledAt.IsZero() {
		meta.InstalledAt = time.Now()
	}
	raw, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeModuleMeta(dir, mm); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestListInstalledLedgers(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)

	installFixture(t, root, "2.0.0-beta.16", moduleMeta{},
		artifact.Meta{Entry: exeName, ZipSHA256: strings.Repeat("a", 64), Source: artifact.SourceRemote})
	installFixture(t, root, "imported-20260101000000", moduleMeta{Source: `E:\Tools\Termora`, IsImport: true},
		artifact.Meta{Entry: exeName, Source: artifact.SourceImported})
	// 损坏安装：无 payload exe → 跳过
	if err := os.MkdirAll(filepath.Join(root, treeEntryName+"_9.9.9"), 0755); err != nil {
		t.Fatal(err)
	}

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("损坏安装必须跳过: %+v", list)
	}
	byVer := map[string]TermoraVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	remote, ok := byVer["v2.0.0-beta.16"]
	if !ok {
		t.Fatalf("缺 beta 条目: %+v", list)
	}
	if !remote.VerifiedHash {
		t.Fatalf("远程摘要链安装必须记 verifiedHash=true: %+v", remote)
	}
	imp, ok := byVer["imported-20260101000000"]
	if !ok || !imp.IsImport || imp.Source != `E:\Tools\Termora` || imp.VerifiedHash {
		t.Fatalf("导入账目失真（verifiedHash 必须如实 false）: %+v", list)
	}
}

// ---------- 测试夹具 ----------

func writeTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTxnID() string { return fmt.Sprintf("txn-%d", time.Now().UnixNano()) }
