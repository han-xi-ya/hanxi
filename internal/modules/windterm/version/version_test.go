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

// ---------- 纯函数与形状口径 ----------

func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "WindTerm_2.6.1_Linux_Portable_x86_64.tar.gz"},
		{Name: "WindTerm_2.6.1_Mac_Portable_x86_64.dmg"},
		{Name: "WindTerm_2.6.1_Windows_Portable_x86_32.zip"},
		{Name: "WindTerm_2.6.1_Windows_Portable_x86_64.zip"},
	}
	// tag 2.6.0 挂 2.6.1 资产（上游实测错位）：精确名不中，形状兜底命中
	if a, ok := findPortableAsset(assets, "2.6.0"); !ok || a.Name != "WindTerm_2.6.1_Windows_Portable_x86_64.zip" {
		t.Fatalf("tag/资产错位应由形状兜底: %#v ok=%v", a, ok)
	}
	// 精确名在场优先
	precise := append([]asset{{Name: "WindTerm_2.7.0_Windows_Portable_x86_64.zip"}}, assets...)
	if a, ok := findPortableAsset(precise, "2.7.0"); !ok || !strings.Contains(a.Name, "2.7.0") {
		t.Fatalf("精确匹配应优先: %#v", a)
	}
	// 只有 x86_32/无 zip → 拒收
	if _, ok := findPortableAsset(assets[:3], "2.6.1"); ok {
		t.Fatal("x86_32/Linux/Mac 资产绝不入列")
	}
}

func TestPlainSemverTagFilter(t *testing.T) {
	for tag, want := range map[string]bool{
		"2.7.0":            true,
		"2.6.1":            true,
		"2.7-prerelease-3": false, // 预发布 tag 非纯语义
		"v2.7.0":           false, // 上游 tag 无 v 前缀；未来若改用 v 形需同批改口径
		"v0.8":             false,
		"2.7.0-fix1":       false,
	} {
		if got := plainSemverTag.MatchString(tag); got != want {
			t.Errorf("plainSemverTag(%q) = %v, want %v", tag, got, want)
		}
	}
}

func TestParseAssetDigest(t *testing.T) {
	tests := []struct {
		raw     string
		wantOK  bool
		wantHex string
	}{
		{"sha256:" + strings.Repeat("a", 64), true, strings.Repeat("a", 64)},
		{"", false, ""},
		{"sha256:short", false, ""},
		{"sha512:" + strings.Repeat("f", 128), false, ""},
		{"garbage", false, ""},
	}
	for _, tt := range tests {
		algo, hex64, ok := parseAssetDigest(tt.raw)
		if ok != tt.wantOK || (ok && (algo != "sha256" || hex64 != tt.wantHex)) {
			t.Errorf("parseAssetDigest(%q) = (%q,%q,%v)", tt.raw, algo, hex64, ok)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	for in, want := range map[string]string{
		"2.7.0":               "v2.7.0",
		"v2.7.0":              "v2.7.0",
		" imported-20260101 ": "imported-20260101",
	} {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeFileVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"2.7.0.0", "2.7.0", true},
		{" 3.2.7 ", "3.2.7", true},
		{"2.7.0.0.", "2.7.0", true}, // 尾点修剪
		{"2.7", "", false},
		{"1.2.3.4.5", "", false},
		{"2.7.x", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeFileVersion(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("normalizeFileVersion(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// ---------- 布局不变式 ----------

func TestFindPayloadExeShapes(t *testing.T) {
	dir := t.TempDir()

	// 嵌套根（官方 zip 形态）
	nested := filepath.Join(dir, "WindTerm_2.7.0")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, exeName), []byte("MZfake"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, ok := findPayloadExe(dir); !ok || filepath.Dir(got) != nested {
		t.Fatalf("嵌套根应命中: %q ok=%v", got, ok)
	}

	// 空 exe 不算在场
	flat := filepath.Join(dir, exeName)
	if err := os.WriteFile(flat, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if got, ok := findPayloadExe(dir); !ok || got != filepath.Join(nested, exeName) {
		t.Fatalf("空平铺 exe 不得遮蔽有效嵌套（regular 判定）: %q", got)
	}

	// 平铺可识别
	big := strings.Repeat("x", 1024)
	if err := os.WriteFile(flat, []byte(big), 0644); err != nil {
		t.Fatal(err)
	}
	if got, ok := findPayloadExe(dir); !ok || got != flat {
		t.Fatalf("平铺形态应命中（平铺优先）: %q", got)
	}

	// 全无 → 拒
	if _, ok := findPayloadExe(t.TempDir()); ok {
		t.Fatal("空目录不得判在场")
	}
}

func TestImportTokenFallback(t *testing.T) {
	junk := filepath.Join(t.TempDir(), "WindTerm.exe")
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

// ---------- ListInstalled 账本口径 ----------

// installFixture 手工摆一个"已落位"的版本目录（内核账本 + 模块账本 + 嵌套 payload）。
func installFixture(t *testing.T, root, token string, mm moduleMeta, meta artifact.Meta) string {
	t.Helper()
	dir := filepath.Join(root, treeEntryName+"_"+token)
	payload := filepath.Join(dir, "WindTerm_"+token)
	if err := os.MkdirAll(payload, 0755); err != nil {
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

	installFixture(t, root, "2.7.0",
		moduleMeta{Source: "WindTerm_2.7.0_Windows_Portable_x86_64.zip", VerifiedHash: false, ComputedSHA: strings.Repeat("a", 64)},
		artifact.Meta{Entry: exeName, Source: artifact.SourceRemote})
	installFixture(t, root, "imported-20260101000000",
		moduleMeta{Source: `E:\Tools\WindTerm`, IsImport: true},
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
	byVersion := map[string]int{}
	for i, v := range list {
		byVersion[v.Version] = i
	}
	idx, ok := byVersion["v2.7.0"]
	if !ok {
		t.Fatal("缺 v2.7.0 条目")
	}
	remote := list[idx]
	if remote.VerifiedHash {
		t.Fatalf("远程安装必须如实 unverified: %+v", remote)
	}
	if !strings.HasSuffix(remote.ExePath, filepath.Join("WindTerm_2.7.0", exeName)) {
		t.Fatalf("ExePath 应指向嵌套 payload: %s", remote.ExePath)
	}
	if remote.SHA256 == "" || remote.InstalledAt == "" {
		t.Fatalf("诊断哈希与安装时刻必须入账: %+v", remote)
	}
	if iidx, iok := byVersion["imported-20260101000000"]; !iok || !list[iidx].IsImport || list[iidx].Source != `E:\Tools\WindTerm` {
		t.Fatalf("导入账目失真: %+v", list)
	}
}

// writeTestZip 按条目表构建 zip（键为包内路径，支持嵌套目录名）。
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
