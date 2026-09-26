package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/packages/go/artifact"
)

// buildImportSource 造一个"用户手里已有的完整 piik 便携目录"：布局不变式
// 条目 + 用户就地产生的杂项（junk.txt / client.json——整套迁移应一并带走）。
func buildImportSource(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		exeName:           "imported-piik-app-bytes",
		"LICENSE":         "MIT License",
		revisionFileName:  "beef1234import",
		"NOTICES":         "notices text",
		captureExeRel:     "cap bytes",
		cloudflaredExeRel: "cfd bytes",
	}
	for k, v := range extra {
		files[k] = v
	}
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(name, ".exe") {
			mode = 0755
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestImportLocalFullTree 整套导入：假 exe 无 PE 版本资源 → imported-时间戳
// 兜底目录；主 exe/runtime 双件/许可文本/用户杂项全部随迁；账本 isImport +
// verifiedHash=false 如实落账；ListInstalled 回读投影零漂移。
func TestImportLocalFullTree(t *testing.T) {
	src := buildImportSource(t, map[string]string{"junk.txt": "user cruft", "client.json": `{"gate":true}`})
	m := NewManager(t.TempDir())

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("非 PE 假件应走 imported-时间戳兜底: %s", info.Version)
	}
	if !info.IsImport || info.Source != src || info.VerifiedHash {
		t.Errorf("导入投影漂移: %+v", info)
	}
	if info.Revision != "beef1234import" {
		t.Errorf("REVISION 未记账: %q", info.Revision)
	}
	token := strings.TrimPrefix(info.Version, "v")
	dest := filepath.Join(m.versionsDir, "piik_"+token)
	if info.Dir != dest {
		t.Errorf("目录漂移: %s vs %s", info.Dir, dest)
	}
	// 整套迁移：兄弟路径双件与用户杂项都在
	for _, rel := range []string{exeName, captureExeRel, cloudflaredExeRel, "junk.txt", "client.json", "LICENSE", "NOTICES", revisionFileName} {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); err != nil {
			t.Errorf("导入缺件 %s: %v", rel, err)
		}
	}
	// 落位目录必须过布局不变式（导入链与下载链同一形状标准）
	if err := checkPiikLayout(dest); err != nil {
		t.Errorf("导入落位布局不完整: %v", err)
	}
	// 双账：内核 artifact.Meta（imported）+ 模块账本
	raw, err := os.ReadFile(filepath.Join(dest, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if json.Unmarshal(raw, &meta) != nil {
		t.Fatalf("内核账本不可读: %s", raw)
	}
	if meta.Source != artifact.SourceImported || meta.Entry != exeName || meta.ZipSHA256 != "" {
		t.Errorf("内核账本来源漂移: %+v", meta)
	}
	mm := readModuleMeta(dest)
	if !mm.IsImport || mm.Source != src || mm.VerifiedHash {
		t.Errorf("模块账本漂移: %+v", mm)
	}

	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListInstalled: %v %d", err, len(list))
	}
	got := list[0]
	if !got.IsImport || got.SHA256 != shaHex([]byte("imported-piik-app-bytes")) ||
		got.Revision != "beef1234import" || got.InstalledAt == "" {
		t.Errorf("导入后已装投影漂移: %+v", got)
	}
	// 导入刚完成 → mtime 无改动迹象 → 复算未触发
	if got.HashDrifted || !strings.Contains(got.DriftNote, "复算未触发") {
		t.Errorf("刚落位应走成本闸: %+v", got)
	}
}

// TestImportLocalRefusesIncompleteSource 门口即拒三态：缺主 exe、缺 runtime
// 双件、主 exe 空件——绝不收进"搬了必炸"的残缺目录。
func TestImportLocalRefusesIncompleteSource(t *testing.T) {
	cases := []struct {
		name   string
		extra  map[string]string
		remove string
		empty  string
		want   string
	}{
		{"缺主 exe", nil, exeName, "", exeName},
		{"缺 capture", nil, captureExeRel, "", "runtime"},
		{"缺 cloudflared", nil, cloudflaredExeRel, "", "runtime"},
		{"主 exe 空件", nil, "", exeName, exeName},
	}
	for _, c := range cases {
		src := buildImportSource(t, c.extra)
		if c.remove != "" {
			if err := os.Remove(filepath.Join(src, filepath.FromSlash(c.remove))); err != nil {
				t.Fatal(err)
			}
		}
		if c.empty != "" {
			if err := os.WriteFile(filepath.Join(src, filepath.FromSlash(c.empty)), nil, 0755); err != nil {
				t.Fatal(err)
			}
		}
		m := NewManager(t.TempDir())
		_, err := m.ImportLocal(src)
		if err == nil || !strings.Contains(err.Error(), filepath.Base(c.want)) {
			t.Errorf("%s: 应拒且点名 %s，得 %v", c.name, c.want, err)
		}
		// 拒绝不得留下版本目录残骸
		entries, _ := os.ReadDir(m.versionsDir)
		for _, e := range entries {
			t.Errorf("%s: 拒绝后版本根不应有落账: %s", c.name, e.Name())
		}
	}
}

// TestImportLocalModuleMetaCopied copied 账目如实描述整套迁移语义。
func TestImportLocalModuleMetaCopied(t *testing.T) {
	src := buildImportSource(t, nil)
	m := NewManager(t.TempDir())
	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(info.Dir, moduleMetaFileName))
	if err != nil {
		t.Fatal(err)
	}
	var mm moduleMeta
	if json.Unmarshal(raw, &mm) != nil {
		t.Fatalf("模块账本不可读: %s", raw)
	}
	if mm.Copied == "" {
		t.Error("copied 账目缺失")
	}
}
