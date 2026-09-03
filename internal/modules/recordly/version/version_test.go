package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应，形态取自实测上游：
// stable、beta（tag 带 -beta.N）、blockmap/dmg/AppImage 干扰项、
// 缺安装器的残缺发布（v1.2.0 场）、缺 digest 的发布。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v1.3.5-beta.2",
    "published_at": "2026-07-11T11:55:12Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "Recordly-windows-x64.exe", "url": "https://api.github.com/x/1", "size": 224391183, "digest": "` + h('a') + `"},
      {"name": "Recordly-windows-x64.exe.blockmap", "url": "https://api.github.com/x/2", "size": 236018, "digest": "` + h('b') + `"},
      {"name": "Recordly-linux-x64.AppImage", "url": "https://api.github.com/x/3", "size": 237500000, "digest": "` + h('c') + `"},
      {"name": "SHA256SUMS.txt", "url": "https://api.github.com/x/4", "size": 285, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v1.3.4-beta.1",
    "published_at": "2026-06-13T12:00:30Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Recordly-x64.zip", "url": "https://api.github.com/x/5", "size": 220300000, "digest": "` + h('e') + `"},
      {"name": "Recordly-windows-x64.exe", "url": "https://api.github.com/x/6", "size": 207900000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v1.3.3",
    "published_at": "2026-05-28T20:15:32Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Recordly-x64.dmg", "url": "https://api.github.com/x/7", "size": 227900000, "digest": "` + h('g') + `"},
      {"name": "Recordly-windows-x64.exe", "url": "https://api.github.com/x/8", "size": 207931183, "digest": "sha256:3a3f1c2d9e8b7a6f5d4c3b2a1908f7e6d5c4b3a291807f6e5d4c3b2a1908f7e6"},
      {"name": "latest.yml", "url": "https://api.github.com/x/9", "size": 400, "digest": "` + h('i') + `"}
    ]
  },
  {
    "tag_name": "v1.2.0",
    "published_at": "2026-04-28T06:11:52Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Recordly-windows-x64.exe.blockmap", "url": "https://api.github.com/x/10", "size": 230000, "digest": "` + h('j') + `"}
    ]
  },
  {
    "tag_name": "nightly-20260401",
    "published_at": "2026-04-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Recordly-windows-x64.exe", "url": "https://api.github.com/x/11", "size": 1, "digest": "` + h('k') + `"}
    ]
  },
  {
    "tag_name": "v1.1.0",
    "published_at": "2026-03-16T11:16:08Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "Recordly-windows-x64.exe", "url": "https://api.github.com/x/12", "size": 1}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 过滤规则：残缺/非规范/缺 digest 全部出局；
// IsPre 双依据（release 标记 ∪ tag 后缀）——v1.3.4-beta.1 的 prerelease=false
// 是上游实测踩坑场，必须靠 tag 后缀兜住。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	byVer := map[string]RecordlyRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 个版本，实际 %d: %+v", len(list), list)
	}
	if v, ok := byVer["v1.3.5-beta.2"]; !ok || !v.IsPre {
		t.Errorf("beta 版本应保留并标记 IsPre: %+v", byVer["v1.3.5-beta.2"])
	}
	if v, ok := byVer["v1.3.4-beta.1"]; !ok || !v.IsPre {
		t.Errorf("prerelease=false 但 tag 带后缀的应标 IsPre: %+v", byVer["v1.3.4-beta.1"])
	}
	if v := byVer["v1.3.3"]; v.IsPre {
		t.Errorf("stable 不应标 IsPre: %+v", v)
	}
	if v := byVer["v1.3.3"]; v.SHA256 != "3a3f1c2d9e8b7a6f5d4c3b2a1908f7e6d5c4b3a291807f6e5d4c3b2a1908f7e6" ||
		v.Size != 207931183 || v.AssetName != "Recordly-windows-x64.exe" {
		t.Errorf("v1.3.3 解析错误: %+v", v)
	}
	for _, gone := range []string{"v1.2.0", "nightly-20260401", "v1.1.0"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if r.AssetName != "Recordly-windows-x64.exe" {
			t.Errorf("混入非 win-x64 资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestListRemoteChannelFilter 通道裁剪：stable 视图不见 beta，includePre 全量。
// 用直接注入缓存绕开网络。
func TestListRemoteChannelFilter(t *testing.T) {
	old := remoteCache.data
	defer func() { remoteCache.data = old }()

	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	remoteCache.data = list
	// fetchedAt 置为当下，避免触发真实网络拉取
	remoteCache.fetchedAt = time.Now()

	stable, err := remoteCache.get(false)
	if err != nil {
		t.Fatalf("get(false): %v", err)
	}
	for _, r := range stable {
		if r.IsPre {
			t.Errorf("stable 通道混入预发布 %s", r.Version)
		}
	}
	all, err := remoteCache.get(true)
	if err != nil {
		t.Fatalf("get(true): %v", err)
	}
	if len(all) != 3 || len(stable) != 1 {
		t.Errorf("通道裁剪数量异常: all=%d stable=%d", len(all), len(stable))
	}
	if _, ok := remoteCache.findRelease("v1.3.5-beta.2"); !ok {
		t.Error("findRelease 应在全量缓存内命中 beta")
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.3.3", "v1.3.3", 0},
		{"v1.3.3", "v1.3.2", 1},
		{"v1.10.0", "v1.9.0", 1},        // 数值分段而非字典序
		{"v1.3.5-beta.2", "v1.3.5", -1}, // 预发布 < 正式版
		{"v1.3.4", "v1.3.5-beta.2", -1}, // 数值核心优先
		{"v1.3.4-beta.1", "v1.3.5-beta.2", -1},
		{"v1.3.5-beta.10", "v1.3.5-beta.2", 1}, // 数字标识符按数值比
		{"v1.3.5-beta", "v1.3.5-beta.1", -1},   // 前缀短者小
		{"v1.3.5-alpha", "v1.3.5-beta", -1},    // 字母段字典序
		{"1.3.3", "v1.3.3", 0},
		{"imported-20260826", "v1.3.3", strings.Compare("imported-20260826", "v1.3.3")}, // 非规范退化字典序
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCompareCore(t *testing.T) {
	if CompareCore("v1.3.5", "v1.3.5-beta.2") != 0 {
		t.Error("数值核心应互认相等")
	}
	if CompareCore("v1.3.5", "v1.3.4") != 1 {
		t.Error("1.3.5 应大于 1.3.4")
	}
}

// TestParseSumsCrossCheck 清单交叉比对（仅解析函数，无网络）：
// 匹配成功 / 不一致失败 / 无匹配条目放行 三态。
func TestParseSumsCrossCheck(t *testing.T) {
	const sha = "7610482a777eee05bac9ad3f59ab56b961a0b975d6d0784a2b983d979d88ff81"
	makeBody := func(line string) string { return line + "\n" }

	if err := checkSumsBody([]byte(makeBody(sha+"  Recordly-windows-x64.exe")), "Recordly-windows-x64.exe", sha); err != nil {
		t.Errorf("一致应通过: %v", err)
	}
	if err := checkSumsBody([]byte(makeBody(sha+" *Recordly-windows-x64.exe")), "Recordly-windows-x64.exe", sha); err != nil {
		t.Errorf("BSD 星号格式应兼容: %v", err)
	}
	// 真实清单含三行（AppImage/安装器/blockmap），比对目标是安装器行
	realList := sha + "  Recordly-linux-x64.AppImage\n" +
		sha + "  Recordly-windows-x64.exe\n" +
		sha + "  Recordly-windows-x64.exe.blockmap\n"
	if err := checkSumsBody([]byte(realList), "Recordly-windows-x64.exe", sha); err != nil {
		t.Errorf("多行清单应命中安装器行: %v", err)
	}
	if err := checkSumsBody([]byte(makeBody(sha+"  Recordly-windows-x64.exe")), "Recordly-windows-x64.exe",
		"0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("不一致必须报错")
	}
	if err := checkSumsBody([]byte(makeBody("deadbeef  Other.bin")), "Recordly-windows-x64.exe", sha); err != nil {
		t.Errorf("无可比对条目应放行: %v", err)
	}
}

// ---------- 托管目录布局与导入 ----------

// mkInstall 在 tempDir 下伪造一个合法的 Electron 安装目录
func mkInstall(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte("fake-exe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, asarRelPath), []byte("fake-asar"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestListInstalledLayoutAndMeta(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	// 未安装 → 空列表
	list, err := m.ListInstalled()
	if err != nil || len(list) != 0 {
		t.Fatalf("未安装时应为空: %d %v", len(list), err)
	}

	// 合法布局 + 远程 meta（tag 优先于 PE 版本——beta 后缀唯一来源）
	inst := m.InstallDir()
	mkInstall(t, inst)
	meta, _ := json.Marshal(map[string]any{
		"installedAt": "2026-09-01 10:00:00",
		"tag":         "v1.3.5-beta.2",
	})
	os.WriteFile(filepath.Join(inst, "hanxi-meta.json"), meta, 0644)

	list, err = m.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("期望 1 条: %d %v", len(list), err)
	}
	if list[0].Version != "v1.3.5-beta.2" || list[0].InstalledAt != "2026-09-01 10:00:00" {
		t.Errorf("版本/元信息解析错误: %+v", list[0])
	}

	// 缺 app.asar 的损坏安装 → 跳过
	os.Remove(filepath.Join(inst, asarRelPath))
	if list, _ := m.ListInstalled(); len(list) != 0 {
		t.Error("缺 Electron 主包应视为损坏跳过")
	}
	mkInstall(t, inst) // 复原

	// ResolveExe：空版本接受任何；同核心互认；异版本拒绝
	if _, err := m.ResolveExe(""); err != nil {
		t.Errorf("ResolveExe(\"\"): %v", err)
	}
	if _, err := m.ResolveExe("v1.3.5"); err != nil {
		t.Errorf("同核心应互认: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("不同版本应拒绝")
	}

	// Remove
	if err := m.Remove("v1.3.5-beta.2"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(inst); !os.IsNotExist(err) {
		t.Error("卸载后目录应消失")
	}
	if err := m.Remove("v1.3.5-beta.2"); err == nil {
		t.Error("重复卸载应报错")
	}
}

func TestImportLocal(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	src := t.TempDir()
	mkInstall(t, src)
	os.WriteFile(filepath.Join(src, "LICENSE.electron.txt"), []byte("license"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if !info.IsImport || info.Source != filepath.Clean(src) {
		t.Errorf("导入标记错误: %+v", info)
	}
	// 整套迁移：布局文件与杂项文件都在
	for _, name := range []string{exeName, asarRelPath, "LICENSE.electron.txt"} {
		if _, err := os.Stat(filepath.Join(m.InstallDir(), name)); err != nil {
			t.Errorf("%s 未拷贝: %v", name, err)
		}
	}
	// 托管目录已存在时二次导入拒绝
	if _, err := m.ImportLocal(src); err == nil {
		t.Error("已有托管版本时导入应拒绝")
	}
	// 非安装目录拒绝
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("无 Recordly.exe 的目录应拒绝")
	}
}

func TestIsUnderDir(t *testing.T) {
	base := filepath.Join("E:", "data", "hanxi", "versions")
	if !isUnderDir(filepath.Join(base, "recordly"), base) {
		t.Error("子目录应判 true")
	}
	if isUnderDir(filepath.Join("E:", "other"), base) {
		t.Error("旁支目录应判 false")
	}
	if isUnderDir(filepath.Join("C:", "Users", "x", "AppData", "Local", "Programs", "Recordly"), base) {
		t.Error("绝对路径逃逸应判 false")
	}
}
