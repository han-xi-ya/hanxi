package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseReleasesBody(t *testing.T) {
	body, _ := json.Marshal([]map[string]any{
		{
			"tag_name": "v1.0.7", "published_at": "2026-08-26T00:00:00Z", "draft": false, "prerelease": false,
			"assets": []map[string]any{
				{"name": "MangoDisk-1.0.7-windows.exe", "size": 1, "digest": "sha256:" + strings.Repeat("1", 64)},
				{"name": "MangoDisk-1.0.7-windows-cli.exe", "size": 2, "digest": "sha256:" + strings.Repeat("2", 64)},
				{"name": "MangoDisk-1.0.7-windows-portable.exe", "size": 23, "digest": "sha256:" + strings.Repeat("a", 64), "url": "asset"},
			},
		},
		{"tag_name": "nightly", "draft": false, "assets": []map[string]any{}},
	})
	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].AssetName != "MangoDisk-1.0.7-windows-portable.exe" || list[0].Size != 23 {
		t.Fatalf("asset = %+v", list[0])
	}
}

func TestParseReleasesRejectsMissingDigestAndSize(t *testing.T) {
	body := []byte(`[
		{"tag_name":"v1.0.6","draft":false,"assets":[{"name":"MangoDisk-1.0.6-windows-portable.exe","size":0,"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},
		{"tag_name":"v1.0.5","draft":false,"assets":[{"name":"MangoDisk-1.0.5-windows-portable.exe","size":1,"digest":""}]}
	]`)
	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("应全部过滤: %+v", list)
	}
}

func TestNormalizeFileVersion(t *testing.T) {
	cases := map[string]string{
		"1.0.7":   "1.0.7",
		"1.0.7.0": "1.0.7",
		"v1.0.7":  "1.0.7",
		"1.0.7,0": "1.0.7",
	}
	for input, want := range cases {
		if got := normalizeFileVersion(input); got != want {
			t.Errorf("normalizeFileVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExpectedAssetName(t *testing.T) {
	if got := expectedAssetName("v1.0.7"); got != "MangoDisk-1.0.7-windows-portable.exe" {
		t.Fatal(got)
	}
}

// ---------- 装机扫描 / 完整性巡检 / 导入（Tree 委托后的领域口径） ----------

// mkLegacyDir 造一个迁移前形状的历史安装目录：mangodisk_<ver>/<含版本资产名> +
// 模块旧账本 meta.json（installMeta 形状），验证双轨回读。
func mkLegacyDir(t *testing.T, versionsDir, ver, content string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "mangodisk_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, expectedAssetName("v"+ver)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := installMeta{
		SchemaVersion: 1, Version: "v" + ver, InstalledAt: "2026-08-28 16:58:23",
		Source: expectedAssetName("v" + ver), AssetName: expectedAssetName("v" + ver),
		ExpectedSize: int64(len(content)), InstalledSize: int64(len(content)),
		ExpectedSHA256: shaHex([]byte(content)), InstalledSHA256: shaHex([]byte(content)),
		FileVersion: ver, ProductName: "MangoDisk", VerifiedOfficial: true,
	}
	raw, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestListInstalledLegacyAndNoise(t *testing.T) {
	versionsDir := t.TempDir()
	mkLegacyDir(t, versionsDir, "1.0.7", "MZ1.0.7")
	// 迁移前的历史安装（meta.json 里哈希基线已对不上假体）：如实列 drifted 不隐藏
	os.WriteFile(filepath.Join(versionsDir, "mangodisk_1.0.7", "meta.json"), func() []byte {
		meta := installMeta{
			SchemaVersion: 1, Version: "v1.0.7", AssetName: expectedAssetName("v1.0.7"),
			InstalledSHA256: strings.Repeat("f", 64), InstalledSize: 1024,
			ExpectedSHA256: strings.Repeat("e", 64), ExpectedSize: 2048,
			FileVersion: "1.0.7", VerifiedOfficial: true,
		}
		raw, _ := json.Marshal(meta)
		return raw
	}(), 0o644)
	mkLegacyDir(t, versionsDir, "1.0.6", "MZ1.0.6")

	m := NewManager(versionsDir)
	m.verifyExe = fakeVerifyExe

	// 噪声目录：.removing- 残骸 / 外来目录 / 缺 exe 目录
	os.MkdirAll(filepath.Join(versionsDir, "mangodisk_1.0.5.removing-123"), 0o755)
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0o755)
	os.MkdirAll(filepath.Join(versionsDir, "mangodisk_9.9.9"), 0o755)

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	byVer := map[string]MangoDiskVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if len(list) != 3 {
		t.Fatalf("应列出 1.0.7/1.0.6/9.9.9（含损坏安装如实呈现），got %+v", list)
	}
	if v, ok := byVer["v9.9.9"]; !ok || v.Integrity != IntegrityInvalid || v.IntegrityNote != "程序文件缺失或为空" {
		t.Errorf("缺 exe 目录应如实列 invalid: %+v", v)
	}
	for _, junk := range []string{"v1.0.5.removing-123", "ccswitch_3.20.0"} {
		if _, ok := byVer[junk]; ok {
			t.Errorf("噪声目录混入: %s", junk)
		}
	}
	// 排序：语义版本降序（versioncmp 数值分段：9.9.9 > 1.0.7 > 1.0.6）
	if list[0].Version != "v9.9.9" || list[1].Version != "v1.0.7" || list[2].Version != "v1.0.6" {
		t.Errorf("列表应按数值降序, got %s, %s, %s", list[0].Version, list[1].Version, list[2].Version)
	}
	// 历史安装（meta 基线不符假体哈希）→ drifted 话术逐字
	v7 := byVer["v1.0.7"]
	if v7.Integrity != IntegrityDrifted || v7.IntegrityNote != "程序文件与安装时基线不一致，可能已被上游更新器替换" {
		t.Errorf("drifted 判定/话术漂移: %+v", v7)
	}
	if v6 := byVer["v1.0.6"]; v6.Integrity != IntegrityVerified || v6.Source != expectedAssetName("v1.0.6") {
		t.Errorf("历史账本双轨回读异常: %+v", v6)
	}
}

// TestInspectAndVerifyBeforeLaunchGates 启动前闸门：verified 放行；drifted/
// invalid 拒绝且话术逐字（迁移前后用户可见文案零漂移）。
func TestInspectAndVerifyBeforeLaunchGates(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.0.7")

	if info, err := m.VerifyBeforeLaunch("v1.0.7"); err != nil || info.Integrity != IntegrityVerified {
		t.Fatalf("verified 应放行: %+v %v", info, err)
	}

	// 模拟 MangoDisk 内置更新器替换 exe（哈希漂移）
	exe := filepath.Join(m.versionsDir, "mangodisk_1.0.7", exeName)
	tampered := exeBody("1.0.8") // 版本也变了：drifted 判定链上任一条件命中即可
	if err := os.WriteFile(exe, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := m.VerifyBeforeLaunch("v1.0.7")
	if err == nil || !strings.Contains(err.Error(), "MangoDisk 内置更新器替换") {
		t.Fatalf("drifted 应拒绝启动并给指引: %+v %v", info, err)
	}
	if info.Integrity != IntegrityDrifted {
		t.Errorf("integrity = %s, want drifted", info.Integrity)
	}

	// 未安装版本：resolveVersionDir 引导话术逐字
	if _, err := m.Inspect("v8.8.8"); err == nil || !strings.Contains(err.Error(), "版本 v8.8.8 未安装，请先下载或导入") {
		t.Errorf("未安装话术漂移: %v", err)
	}
	if _, err := m.Inspect("../escape"); err == nil || !strings.Contains(err.Error(), "非法版本号") {
		t.Errorf("形状外令牌必须先拒: %v", err)
	}
}

// TestImportLocal 导入：PE FileVersion 定版、定名归一落位、来源账为导入路径、
// 完整性走 LocalBaseline；同版本重复导入拒绝（既有话术）。
func TestImportLocal(t *testing.T) {
	m := newTestManager(t)
	srcDir := t.TempDir()
	srcExe := filepath.Join(srcDir, "用户随意改名的文件.exe")
	body := exeBody("1.0.7")
	if err := os.WriteFile(srcExe, body, 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := m.ImportLocal(srcExe)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if info.Version != "v1.0.7" || !info.IsImport || info.Source != srcExe {
		t.Errorf("导入账目异常: %+v", info)
	}
	if info.ExePath != filepath.Join(info.Dir, exeName) {
		t.Errorf("导入应定名归一到 %s: %+v", exeName, info)
	}
	if info.Integrity != IntegrityLocalBaseline || info.IntegrityNote != "本地导入文件与导入时哈希基线一致" {
		t.Errorf("导入完整性口径漂移: %+v", info)
	}
	if info.ExpectedSHA256 != "" {
		t.Errorf("导入不应有官方摘要账: %+v", info)
	}

	// 同版本号重复导入 → 既有话术拒绝
	if _, err := m.ImportLocal(srcExe); err == nil || !strings.Contains(err.Error(), "请先卸载再导入") {
		t.Fatalf("重复导入应拒绝, got %v", err)
	}

	// 无效源与不可识别版本
	if _, err := m.ImportLocal(filepath.Join(srcDir, "不存在.exe")); err == nil ||
		!strings.Contains(err.Error(), "未找到可用的 MangoDisk EXE") {
		t.Errorf("坏源话术漂移: %v", err)
	}
	odd := filepath.Join(srcDir, "odd.exe")
	os.WriteFile(odd, []byte("MZnot-a-semver"), 0o644)
	if _, err := m.ImportLocal(odd); err == nil ||
		!strings.Contains(err.Error(), "FileVersion 不是可识别的语义版本") {
		t.Errorf("非语义版本导入应拒绝: %v", err)
	}

	// 导入链同样不留 .tmp-* 半件（discard 收口）
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestRemoveUntrustedLegacyRefused 迁移前的历史目录（meta.json 无内核 schema）：
// Remove 仍按版本令牌定位删除；ListInstalled/ResolveExe 双轨可用（升级不砸旧装机）。
func TestRemoveUntrustedLegacyRefused(t *testing.T) {
	versionsDir := t.TempDir()
	mkLegacyDir(t, versionsDir, "1.0.7", "MZ1.0.7")
	m := NewManager(versionsDir)
	m.verifyExe = fakeVerifyExe

	if exe, err := m.ResolveExe("v1.0.7"); err != nil || filepath.Base(exe) != expectedAssetName("v1.0.7") {
		t.Fatalf("历史目录寻径应走旧账本 assetName: %v %v", exe, err)
	}
	if err := m.Remove("v1.0.7"); err != nil {
		t.Fatalf("Remove(legacy): %v", err)
	}
	if list, err := m.ListInstalled(); err != nil || len(list) != 0 {
		t.Fatalf("卸载后应清空: %+v %v", list, err)
	}
	assertNoTransactionLeftovers(t, versionsDir)
}
