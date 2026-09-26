// msix_test.go 打包形态线的解析过滤与"下载 → 摘要复核 → 原子换装缓存"链测试：
// seam 注入假 msix 远程列表与假资产源（回环 httptest），下载/校验走生产实现
// artifact.Fetch；覆盖无 msix 资产旧版本、无摘要 msix、校验失败不留脏文件、
// 幂等直返不重下载、缓存枚举与删除等收口行为。
package version

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- 测试夹具 ----------

// fakeMsixReleasesJSON 与真实 GitHub API 同构的打包形态样例（资产矩阵按
// 2026-09 实测上游还原）：2026.2 全套形态含带摘要 bundle.msixbundle；
// 2025.1 有 msix 但无 digest（GitHub digest 机制上线前形态）；2024.4 只有
// 便携 zip；2020.2 是散装 .msix + vclibs.msix + setup；draft 与非年份 tag 各一。
func fakeMsixReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "2026.2",
    "published_at": "2026-08-31T21:01:53Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "bundle.msixbundle", "url": "https://api.github.com/x/1", "size": 4173869, "digest": "` + h('a') + `"},
      {"name": "TranslucentTB-portable-x64.zip", "url": "https://api.github.com/x/3", "size": 1769775, "digest": "` + h('b') + `"},
      {"name": "TranslucentTB.appinstaller", "url": "https://api.github.com/x/4", "size": 2563, "digest": "` + h('c') + `"},
      {"name": "TranslucentTB-setup.exe", "url": "https://api.github.com/x/5", "size": 4173872, "digest": "` + h('c') + `"}
    ]
  },
  {
    "tag_name": "2025.1",
    "published_at": "2025-04-24T21:09:38Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "bundle.msixbundle", "url": "https://api.github.com/x/6", "size": 4131399},
      {"name": "TranslucentTB-portable-x64.zip", "url": "https://api.github.com/x/7", "size": 1754408}
    ]
  },
  {
    "tag_name": "2024.4",
    "published_at": "2024-11-03T10:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "TranslucentTB-portable-arm64.zip", "url": "https://api.github.com/x/8", "size": 1700000, "digest": "` + h('d') + `"},
      {"name": "TranslucentTB-portable-x64.zip", "url": "https://api.github.com/x/9", "size": 1700001, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "2020.2",
    "published_at": "2020-08-20T05:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "TranslucentTB-package.msix", "url": "https://api.github.com/x/10", "size": 9000000, "digest": "` + h('e') + `"},
      {"name": "vclibs.msix", "url": "https://api.github.com/x/11", "size": 60000, "digest": "` + h('e') + `"},
      {"name": "TranslucentTB-setup.exe", "url": "https://api.github.com/x/12", "size": 5000000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "2019.1",
    "published_at": "2019-04-12T05:00:00Z",
    "prerelease": false,
    "draft": true,
    "assets": [
      {"name": "bundle.msixbundle", "url": "https://api.github.com/x/13", "size": 8000000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "nightly-build",
    "published_at": "2026-01-01T00:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "bundle.msixbundle", "url": "https://api.github.com/x/14", "size": 8000000, "digest": "` + h('0') + `"}
    ]
  }
]`
	return []byte(body)
}

// seedMsixRemote 注入伪打包形态远程列表（msixCache 为包内全局，同 remoteCache
// 测试纪律：本包测试串行、setup 重灌隔离）。
func seedMsixRemote(t *testing.T, rels ...MsixRelease) {
	t.Helper()
	msixCache.mu.Lock()
	defer msixCache.mu.Unlock()
	msixCache.data = rels
	msixCache.fetchedAt = time.Now() // TTL 内，ListMsixReleases/PreparePackage 不再走网络
}

// newMsixManager 构造指向假源的 Manager（仅镜像 URL 接缝改指回环 http，
// 下载/校验全链仍走生产实现 artifact.Fetch）。
func newMsixManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/" + msixAssetName}
	}
	return m
}

// msixProgress 收集 (percent, stage) 事件序列。
type msixProgress struct {
	mu     sync.Mutex
	events []struct {
		percent float64
		stage   string
	}
}

func (r *msixProgress) cb() func(float64, string) {
	return func(percent float64, stage string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.events = append(r.events, struct {
			percent float64
			stage   string
		}{percent, stage})
	}
}

func (r *msixProgress) stageSeq() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var stages []string
	for _, e := range r.events {
		if len(stages) == 0 || stages[len(stages)-1] != e.stage {
			stages = append(stages, e.stage)
		}
	}
	return strings.Join(stages, ",")
}

func (r *msixProgress) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// ---------- 解析与过滤 ----------

// TestParseMsixReleasesBody 过滤规则：仅 2026.2 入列表；无摘要 msix（2025.1）、
// 无 msix（2024.4）、散装 .msix（2020.2）、draft、非年份 tag 一律剔除。
func TestParseMsixReleasesBody(t *testing.T) {
	list, err := parseMsixReleasesBody(fakeMsixReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseMsixReleasesBody: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 个打包形态版本，实际 %d: %+v", len(list), list)
	}
	r := list[0]
	if r.Version != "2026.2" || r.SHA256 != strings.Repeat("a", 64) || r.Size != 4173869 {
		t.Errorf("2026.2 解析错误: %+v", r)
	}
	want := "https://github.com/TranslucentTB/TranslucentTB/releases/download/2026.2/bundle.msixbundle"
	if r.URL != want {
		t.Errorf("URL = %q，期望 %q", r.URL, want)
	}
}

// TestFindMsixAsset 资产筛选只认 bundle.msixbundle：便携/setup/.appinstaller/
// 散装 .msix 全排除；大小写漂移兼容；无 msix 资产的旧版本判不命中。
func TestFindMsixAsset(t *testing.T) {
	got, ok := findMsixAsset([]asset{
		{Name: "TranslucentTB-portable-x64.zip"},
		{Name: "TranslucentTB-setup.exe"},
		{Name: "TranslucentTB.appinstaller"},
		{Name: "winui-x64.appx"},
		{Name: "Bundle.MSIXBundle", Size: 4173869}, // 大小写漂移仍认
	})
	if !ok || got.Name != "Bundle.MSIXBundle" {
		t.Fatalf("应命中 bundle.msixbundle（大小写不敏感），got %+v ok=%v", got, ok)
	}

	// 2024.4（仅便携）与 2020.2（散装 .msix）形态不得误判
	for _, assets := range [][]asset{
		{{Name: "TranslucentTB-portable-x64.zip"}, {Name: "TranslucentTB-portable-arm64.zip"}},
		{{Name: "TranslucentTB-package.msix"}, {Name: "vclibs.msix"}, {Name: "TranslucentTB-setup.exe"}},
	} {
		if _, ok := findMsixAsset(assets); ok {
			t.Errorf("无 bundle.msixbundle 的资产集误判命中: %v", assets)
		}
	}
}

// TestHasMsixRelease 降级钮判定：列表内在册为 true，缺 msix 的旧版本为 false。
func TestHasMsixRelease(t *testing.T) {
	m := NewManager(t.TempDir())
	seedMsixRemote(t, MsixRelease{Version: "2026.2", SHA256: strings.Repeat("a", 64), Size: 4173869})
	if !m.HasMsixRelease("2026.2") {
		t.Error("2026.2 应判定有打包形态")
	}
	if m.HasMsixRelease("2024.4") {
		t.Error("无 msix 资产的旧版本应判 false")
	}
}

// ---------- PreparePackage 主链与失败注入 ----------

// TestPreparePackageHappyPathAndIdempotent 正常链：下载→verify-sha256→done，
// 落位路径逐字对齐 versions/translucenttb/packages/<ver>/bundle.msixbundle；
// 二次调用幂等直返（终字节摘要对过），一个下载请求都不再发出。
func TestPreparePackageHappyPathAndIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newMsixManager(t, src)
	bundle := []byte("fake-bundle.msixbundle-bytes")
	seedMsixRemote(t, MsixRelease{
		Version: "2026.2",
		URL:     src.srv.URL + "/" + msixAssetName,
		SHA256:  shaHex(bundle),
		Size:    int64(len(bundle)),
	})
	src.set(bundle, false)

	rec := &msixProgress{}
	path, err := m.PreparePackage(context.Background(), "2026.2", rec.cb())
	if err != nil {
		t.Fatalf("PreparePackage: %v", err)
	}
	want := filepath.Join(m.versionsDir, "translucenttb", "packages", "2026.2", "bundle.msixbundle")
	if path != want {
		t.Errorf("落位路径 = %q，期望 %q", path, want)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(bundle) {
		t.Error("缓存文件内容与源字节不符")
	}
	if seq := rec.stageSeq(); seq != "downloading,verify-sha256,done" {
		t.Errorf("进度阶段序列 = %q", seq)
	}
	for _, e := range rec.events {
		if e.percent < 0 || e.percent > 100 {
			t.Fatalf("percent 越界: %+v", e)
		}
	}

	// 幂等直返：不再触源，仅 done 事件
	hitsBefore := src.count()
	rec2 := &msixProgress{}
	path2, err := m.PreparePackage(context.Background(), "2026.2", rec2.cb())
	if err != nil || path2 != want {
		t.Fatalf("幂等直返异常: %q %v", path2, err)
	}
	if src.count() != hitsBefore {
		t.Errorf("缓存命中不得重下载: hits %d → %d", hitsBefore, src.count())
	}
	if seq := rec2.stageSeq(); seq != "done" {
		t.Errorf("幂等路径进度序列 = %q，期望仅 done", seq)
	}
	assertNoPackageTmpLeftovers(t, m.packagesRoot())
}

// TestPreparePackageBadDigestNoDirtyFile 源字节与官方摘要不符 → 报错，
// 终位不出现、目录内零脏文件残留（tmp+rename 纪律）。
func TestPreparePackageBadDigestNoDirtyFile(t *testing.T) {
	src := newZipSource(t)
	m := newMsixManager(t, src)
	seedMsixRemote(t, MsixRelease{
		Version: "2026.2",
		SHA256:  shaHex([]byte("NOT-THE-REAL-DIGEST")),
		Size:    17,
	})
	src.set([]byte("tampered-bytes!!!"), false)

	_, err := m.PreparePackage(context.Background(), "2026.2", nil)
	if err == nil {
		t.Fatal("摘要不符应报错")
	}
	if !strings.Contains(err.Error(), "SHA256") && !strings.Contains(err.Error(), "sha256") {
		t.Errorf("错误应指向摘要校验: %v", err)
	}
	final := filepath.Join(m.packagesRoot(), "2026.2", "bundle.msixbundle")
	if _, statErr := os.Stat(final); !os.IsNotExist(statErr) {
		t.Error("坏包不得出现在终位")
	}
	assertNoPackageTmpLeftovers(t, m.packagesRoot())
}

// TestPreparePackageNoMsixFormRefuses 无打包形态的版本如实报错，
// 且一个下载请求都不发出（先审列表，再碰网络）。
func TestPreparePackageNoMsixFormRefuses(t *testing.T) {
	src := newZipSource(t)
	m := newMsixManager(t, src)
	seedMsixRemote(t, MsixRelease{Version: "2026.2", SHA256: strings.Repeat("a", 64), Size: 10})
	src.set([]byte("x"), false)

	_, err := m.PreparePackage(context.Background(), "2024.4", nil)
	if err == nil || !strings.Contains(err.Error(), "无打包形态") {
		t.Fatalf("应如实报'该版本无打包形态', got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("无打包形态时不得发起下载请求, hits = %d", hits)
	}
}

// TestPreparePackageInvalidVersionVersionWhitelist 版本令牌白名单先于一切
// 磁盘/网络动作：路径穿越、非年份域、imported 兜底名一律拒。
func TestPreparePackageInvalidVersionVersionWhitelist(t *testing.T) {
	src := newZipSource(t)
	m := newMsixManager(t, src)
	seedMsixRemote(t) // 空列表：若白名单失守会走到"无打包形态"分支，报错词不同
	src.set([]byte("x"), false)

	for _, v := range []string{"../../windows", "2026", "imported-20260906-150405", ""} {
		_, err := m.PreparePackage(context.Background(), v, nil)
		if err == nil || !strings.Contains(err.Error(), "非法版本号") {
			t.Errorf("版本令牌 %q 应被白名单拒绝, got %v", v, err)
		}
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("非法版本不得发起下载请求, hits = %d", hits)
	}
}

// TestPreparePackageStaleCacheReplaced 同版本异摘要（上游同 tag 重传）：
// 终位旧包被新下载验明正身的字节原子换装，不留半件。
func TestPreparePackageStaleCacheReplaced(t *testing.T) {
	src := newZipSource(t)
	m := newMsixManager(t, src)
	old := []byte("old-upload-same-tag")
	bundle := []byte("brand-new-upload-same-tag")
	seedMsixRemote(t, MsixRelease{Version: "2026.2", SHA256: shaHex(bundle), Size: int64(len(bundle))})

	// 预置一份与当前官方摘要不符的旧缓存
	final := filepath.Join(m.packagesRoot(), "2026.2", "bundle.msixbundle")
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, old, 0o644); err != nil {
		t.Fatal(err)
	}

	src.set(bundle, false)
	path, err := m.PreparePackage(context.Background(), "2026.2", nil)
	if err != nil {
		t.Fatalf("异摘要旧包应重下换装: %v", err)
	}
	if path != final {
		t.Errorf("路径 = %q", path)
	}
	if got, _ := os.ReadFile(final); string(got) != string(bundle) {
		t.Error("终位未被新字节换装")
	}
	assertNoPackageTmpLeftovers(t, m.packagesRoot())
}

// ---------- 缓存枚举与删除 ----------

// TestPackageCachePathsAndRemove 枚举只收"白名单目录 + 终文件在位非空"，
// 版本降序（2026.10 > 2026.2 语义序）；Remove 走白名单且幂等。
func TestPackageCachePathsAndRemove(t *testing.T) {
	m := NewManager(t.TempDir())
	root := m.packagesRoot()

	put := func(ver, name string, content []byte) {
		dir := filepath.Join(root, ver)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("2026.2", "bundle.msixbundle", []byte("aaa"))
	put("2026.10", "bundle.msixbundle", []byte("bb"))
	put("2026.1", "bundle.msixbundle", nil)                                                    // 空文件：损坏缓存不入列
	put("2025.1", "readme.txt", []byte("x"))                                                   // 缺终文件：不入列
	os.MkdirAll(filepath.Join(root, "imported-20260906-150405"), 0o755)                        // 形状外目录：跳过
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0o644); err != nil { // 文件条目：跳过
		t.Fatal(err)
	}

	list := m.PackageCachePaths()
	if len(list) != 2 {
		t.Fatalf("期望 2 条有效缓存: %+v", list)
	}
	if list[0].Version != "2026.10" || list[1].Version != "2026.2" { // 语义序降序
		t.Errorf("排序错误: %+v", list)
	}
	if list[1].Path != filepath.Join(root, "2026.2", "bundle.msixbundle") || list[1].Size != 3 {
		t.Errorf("条目字段错误: %+v", list[1])
	}

	if err := m.RemovePackageCache("2026.2"); err != nil {
		t.Fatalf("RemovePackageCache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "2026.2")); !os.IsNotExist(err) {
		t.Error("缓存目录未被删除")
	}
	if err := m.RemovePackageCache("2026.2"); err != nil {
		t.Errorf("重复删除应幂等: %v", err)
	}
	for _, bad := range []string{"../../2026.2", "2026", "imported-20260906-150405"} {
		if err := m.RemovePackageCache(bad); err == nil {
			t.Errorf("非法版本令牌 %q 应被拒绝", bad)
		}
	}
}

// assertNoPackageTmpLeftovers 缓存根内不得残留隐藏临时件（.bundle-*.tmp）。
func assertNoPackageTmpLeftovers(t *testing.T, root string) {
	t.Helper()
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 根不存在即无残留
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			t.Errorf("缓存树残留临时件: %s", path)
		}
		return nil
	})
}
