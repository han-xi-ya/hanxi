package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
	"hanxi/packages/go/hostfeed"
)

// ---------------------------------------------------------------------------
// 测试夹具：假 exe 用文件内容承载元数据，PE 读取接缝按内容解读
// （真实实现走 Win32 VerQueryValue，非 Windows 与 CI 环境不可用）。
// ---------------------------------------------------------------------------

// writeFakeExe 落一枚假主程序：MZ 头 + VER=<版本> + DESC=<FileDescription>。
// version/desc 传空串表示"资源缺失"（接缝据此返回错误）。
func writeFakeExe(t *testing.T, dir, name, version, desc string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "MZ\x90\x00\x03\x00"
	if version != "" {
		body += "VER=" + version + "\x00"
	}
	if desc != "" {
		body += "DESC=" + desc + "\x00"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// readTag 从假 exe 内容里取指定标签值。
func readTag(data []byte, tag string) (string, bool) {
	s := string(data)
	i := strings.Index(s, tag+"=")
	if i < 0 {
		return "", false
	}
	rest := s[i+len(tag)+1:]
	if j := strings.IndexByte(rest, 0); j >= 0 {
		rest = rest[:j]
	}
	return rest, true
}

func fakeFileVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if v, ok := readTag(data, "VER"); ok {
		return v, nil
	}
	return "", fmt.Errorf("假 PE：无版本资源")
}

func fakeFileDescription(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if d, ok := readTag(data, "DESC"); ok {
		return d, nil
	}
	return "", fmt.Errorf("假 PE：无描述资源")
}

// newTestManager 装 PE 假接缝。mirrors 用真实实现（纯字符串拼接、不触网）；
// fetch/unpack 刻意留 nil——漏装的用例应当立刻显形，而不是悄悄走真网络。
func newTestManager() *Manager {
	return &Manager{
		fileVersion:     fakeFileVersion,
		fileDescription: fakeFileDescription,
		mirrors:         assetMirrors,
	}
}

func newManagerAt(t *testing.T) (*Manager, string) {
	t.Helper()
	versions := filepath.Join(t.TempDir(), "versions")
	m := newTestManager()
	m.versionsDir = versions
	m.tree = OpenTree(versions)
	return m, versions
}

// seedRemoteCache 把样例响应灌进包级远程缓存（TTL 内命中，测试全程不触网）。
func seedRemoteCache(t *testing.T, body []byte) {
	t.Helper()
	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatalf("样例响应解析失败: %v", err)
	}
	remoteCache.mu.Lock()
	remoteCache.data = list
	remoteCache.fetchedAt = time.Now()
	remoteCache.mu.Unlock()
	t.Cleanup(func() {
		remoteCache.mu.Lock()
		remoteCache.data, remoteCache.fetchedAt = nil, time.Time{}
		remoteCache.mu.Unlock()
	})
}

// realReleaseBody 64.0.0 / 63.x / 62.0.0（无 digest）/ 61.0.0（仅 rar 无 digest）
// / 40.0.0（zip 时代）/ 24.4.1RP（tag 形状非法）的实测响应片段。
const realReleaseBody = `[
 {"tag_name":"64.0.0","html_url":"https://github.com/zbezj/HEU_KMS_Activator/releases/tag/64.0.0","published_at":"2026-07-06T04:01:22Z","draft":false,"prerelease":false,
  "assets":[
    {"name":"HEU_KMS_Activator_v64.0.0.iso","url":"https://iso","size":6045696,"digest":"sha256:bf4b8fb9ac78eac99e453668d85ab9f91c232ce6a16a35077574e2c0b271663d"},
    {"name":"HEU_KMS_Activator_v64.0.0.rar","url":"https://rar","size":5112665,"digest":"sha256:7e4f770fcf41ef71c6c20949b818193e16c76e68c664571a6316b4b8d22832e5"}]},
 {"tag_name":"63.1.0","html_url":"https://x/63.1.0","published_at":"2025-07-07T12:40:02Z","draft":false,"prerelease":false,
  "assets":[{"name":"HEU_KMS_Activator_v63.1.0.rar","url":"https://rar63","size":4892615,"digest":"sha256:e27a66f172c6bfaef523ed429824393773770f058addcd8fa0af3aca34d68726"}]},
 {"tag_name":"62.0.0","html_url":"https://x/62.0.0","published_at":"2025-05-13T13:49:50Z","draft":false,"prerelease":false,
  "assets":[
    {"name":"HEU_KMS_Activator_v62.0.0.iso","url":"https://iso62","size":5812224,"digest":""},
    {"name":"HEU_KMS_Activator_v62.0.0.rar","url":"https://rar62","size":4875352,"digest":""}]},
 {"tag_name":"61.0.0","html_url":"","published_at":"2025-03-31T16:01:41Z","draft":false,"prerelease":false,
  "assets":[{"name":"HEU_KMS_Activator_v61.0.0.rar","url":"https://rar61","size":4936840,"digest":""}]},
 {"tag_name":"40.0.0","html_url":"https://x/40.0.0","published_at":"2023-10-08T00:00:00Z","draft":false,"prerelease":false,
  "assets":[{"name":"HEU_KMS_Activator_v40.0.0.zip","url":"https://zip40","size":4000000,"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},
 {"tag_name":"24.4.1RP","html_url":"https://x/rp","published_at":"2022-01-01T00:00:00Z","draft":false,
  "assets":[{"name":"HEU_KMS_Activator.zip","size":1,"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},
 {"tag_name":"65.0.0","html_url":"https://x/65","published_at":"2026-08-01T00:00:00Z","draft":true,
  "assets":[{"name":"HEU_KMS_Activator_v65.0.0.rar","size":1,"digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}]},
 {"tag_name":"66.0.0","html_url":"https://x/66","published_at":"2026-09-01T00:00:00Z","draft":false,"prerelease":false,
  "assets":[{"name":"HEU_KMS_Activator_v66.0.0.iso","size":1,"digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]}
]`

// ---------------------------------------------------------------------------
// remote.go：入选四闸（非 draft / tag 形状 / 有 rar / 有官方摘要）
// ---------------------------------------------------------------------------

func TestParseReleasesBodyHostableGate(t *testing.T) {
	list, err := parseReleasesBody([]byte(realReleaseBody))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	// 只有 64.0.0 与 63.1.0 通过四闸：62/61 无 digest、40 是 zip、24.4.1RP 形状
	// 非法、65 是 draft、66 只有 iso。
	if got := versionsOf(list); len(got) != 2 || got[0] != "64.0.0" || got[1] != "63.1.0" {
		t.Fatalf("可安装表应为 [64.0.0 63.1.0]，got %v", got)
	}

	top := list[0]
	// 托管资产恒为 rar（iso 的 UDF 侧读不到，zip 时代太老不纳入）
	if !strings.HasSuffix(top.AssetName, ".rar") {
		t.Fatalf("托管资产应取 rar，got %q", top.AssetName)
	}
	if top.SHA256 != "7e4f770fcf41ef71c6c20949b818193e16c76e68c664571a6316b4b8d22832e5" {
		t.Fatalf("官方 digest 剥离错误: %q", top.SHA256)
	}
	if top.Size != 5112665 {
		t.Fatalf("资产大小应取 rar 的声明值，got %d", top.Size)
	}
	if top.PageURL == "" || !strings.Contains(top.PageURL, "64.0.0") {
		t.Fatalf("PageURL 应回填发布页，got %q", top.PageURL)
	}
	if top.Form != hostfeed.FormPortable {
		t.Fatalf("托管形态事实应为 portable（解包直启），got %q", top.Form)
	}
	// 资产矩阵仍含 iso（展示上游全发布物），且恰一枚标为托管
	var managed, iso int
	for _, a := range top.Assets {
		if a.Managed {
			managed++
		}
		if strings.HasSuffix(a.Label, ".iso") {
			iso++
		}
	}
	if managed != 1 || iso != 1 {
		t.Fatalf("矩阵应两枚齐备且仅 rar 标托管，managed=%d iso=%d", managed, iso)
	}
	// 缺 html_url 时按 tag 拼页
	if list[1].PageURL == "" {
		t.Fatal("PageURL 不应为空")
	}
}

func TestFindHostedAsset(t *testing.T) {
	mk := func(names ...string) []asset {
		out := make([]asset, 0, len(names))
		for _, n := range names {
			out = append(out, asset{Name: n})
		}
		return out
	}
	if a, ok := findHostedAsset(mk("x.iso", "y.rar")); !ok || !strings.HasSuffix(a.Name, ".rar") {
		t.Fatalf("iso+rar 双发应取 rar，got %+v ok=%v", a, ok)
	}
	if _, ok := findHostedAsset(mk("y.RAR")); !ok {
		t.Fatal("扩展名大小写不敏感")
	}
	if _, ok := findHostedAsset(mk("x.iso")); ok {
		t.Fatal("仅 iso 不可解包，应判 false")
	}
	if _, ok := findHostedAsset(mk("x.zip")); ok {
		t.Fatal("zip 时代老版本不纳入下载通道，应判 false")
	}
	if _, ok := findHostedAsset(nil); ok {
		t.Fatal("空资产列表应判 false")
	}
}

func TestExtOf(t *testing.T) {
	if extOf("a.b.rar") != ".rar" || extOf("noext") != "" {
		t.Fatal("扩展名提取不符")
	}
}

func versionsOf(list []HeuRelease) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.Version)
	}
	return out
}

// ---------------------------------------------------------------------------
// unpack_rar.go：条目审阅与安全闸（纯函数层——本仓无纯 Go 的 RAR 写入库，
// CI 造不出 rar 夹具，故判据抽成纯逻辑逐条锁死；真解包由机主联调验证）
// ---------------------------------------------------------------------------

func TestSanitizeRarName(t *testing.T) {
	ok := []string{"HEU_KMS_Activator_v64.0.0.exe", "更新日志.txt", "当前目录添加到WD白名单.cmd", "a b.exe"}
	for _, n := range ok {
		if got, err := sanitizeRarName(n); err != nil || got != n {
			t.Errorf("平铺条目名应原样通过: %q got %q err %v", n, got, err)
		}
	}
	bad := []string{
		"sub/dir/x.exe",    // 非平铺包（本模块只接受平铺）
		`..\evil.exe`,      // 反斜杠当分隔符同样拒
		"a/../../b.exe",    // 穿越
		"C:/Windows/x.exe", // 盘符
		"..",               // 纯穿越
		"",                 // 空名
		"   ",              // 全空白
		"x.",               // 以点结尾（Windows 目录名限制）
		"con.exe",          // 保留设备名
		"LPT9",             // 保留设备名
		"NUL.txt",          // 保留设备名（大小写不敏感）
		"x/y",              // 目录层级
	}
	for _, n := range bad {
		if got, err := sanitizeRarName(n); err == nil {
			t.Errorf("可疑条目名必须拒收: %q → got %q", n, got)
		}
	}
}

func TestIsReservedDeviceName(t *testing.T) {
	for _, n := range []string{"con", "CON", "prn.txt", "aux.dat", "nul", "com1", "COM9.log", "lpt3"} {
		if !isReservedDeviceName(n) {
			t.Errorf("应为保留设备名: %s", n)
		}
	}
	for _, n := range []string{"com0", "com10", "lpt0", "config.sys", "conx", "console.exe", "nula.txt"} {
		if isReservedDeviceName(n) {
			t.Errorf("不应误判为保留设备名: %s", n)
		}
	}
}

func TestPlanRarUnpack(t *testing.T) {
	t.Run("平铺包唯一 exe", func(t *testing.T) {
		target, err := planRarUnpack([]rarEntryInfo{
			{Name: "HEU_KMS_Activator_v64.0.0.exe", Size: 4707840},
			{Name: "更新日志.txt", Size: 42827},
			{Name: "更新地址：微信公众号.jpg", Size: 643884},
			{Name: "当前目录添加到WD白名单.cmd", Size: 416},
		})
		if err != nil || target != "HEU_KMS_Activator_v64.0.0.exe" {
			t.Fatalf("应选中唯一 exe，got %q err %v", target, err)
		}
	})
	t.Run("目录条目不计入 exe 统计", func(t *testing.T) {
		target, err := planRarUnpack([]rarEntryInfo{
			{Name: "tool", IsDir: true},
			{Name: "HEU_KMS_Activator.exe", Size: 100},
		})
		if err != nil || target != "HEU_KMS_Activator.exe" {
			t.Fatalf("got %q err %v", target, err)
		}
	})
	t.Run("无 exe 拒收", func(t *testing.T) {
		if _, err := planRarUnpack([]rarEntryInfo{{Name: "readme.txt", Size: 10}}); err == nil {
			t.Fatal("包内没有 exe 必须拒收")
		}
	})
	t.Run("两枚 exe 歧义拒收", func(t *testing.T) {
		_, err := planRarUnpack([]rarEntryInfo{
			{Name: "HEU_KMS_Activator_v64.0.0.exe", Size: 100},
			{Name: "Unins.exe", Size: 50},
		})
		if err == nil || !strings.Contains(err.Error(), "无法判定主程序") {
			t.Fatalf("多 exe 必须拒收而非挑一个，got %v", err)
		}
	})
	t.Run("空包拒收", func(t *testing.T) {
		if _, err := planRarUnpack(nil); err == nil {
			t.Fatal("空条目列表必须拒收")
		}
	})
	t.Run("单条目超限拒收", func(t *testing.T) {
		_, err := planRarUnpack([]rarEntryInfo{{Name: "a.exe", Size: rarMaxEntryBytes + 1}})
		if err == nil || !strings.Contains(err.Error(), "压缩炸弹") {
			t.Fatalf("单条目超限应拒收，got %v", err)
		}
	})
	t.Run("总量超限拒收", func(t *testing.T) {
		// 每枚都在单条目上限内（48MB < 64MB），但三枚合计 144MB 越过总量上限
		per := rarMaxEntryBytes * 3 / 4
		_, err := planRarUnpack([]rarEntryInfo{
			{Name: "a.exe", Size: per}, {Name: "b.bin", Size: per}, {Name: "c.bin", Size: per},
		})
		if err == nil || !strings.Contains(err.Error(), "总量") {
			t.Fatalf("总量超限应拒收，got %v", err)
		}
	})
	t.Run("带目录的 exe 也被名字闸拒收", func(t *testing.T) {
		if _, err := planRarUnpack([]rarEntryInfo{{Name: "bin/HEU.exe", Size: 100}}); err == nil {
			t.Fatal("非平铺条目应在名字闸就被拒")
		}
	})
}

func TestUnpackRarRejectsBadPackage(t *testing.T) {
	// 没有 rar 夹具可造，但**打不开/无条目**的路径必须如实报错而非 panic。
	dir := t.TempDir()
	notRar := filepath.Join(dir, "fake.rar")
	if err := os.WriteFile(notRar, []byte("PK\x03\x04not a rar at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := unpackRar(context.Background(), notRar, filepath.Join(dir, "out")); err == nil {
		t.Fatal("非 rar 文件必须报错")
	}
	if _, err := unpackRar(context.Background(), filepath.Join(dir, "missing.rar"), dir); err == nil {
		t.Fatal("文件缺失必须报错")
	}
	// 已取消的上下文不得启动解包
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listRarEntries(ctx, notRar); err == nil {
		t.Fatal("取消的 ctx 应即刻失败")
	}
}

// ---------------------------------------------------------------------------
// DownloadContext：下载→解包→三重闸→落位（fetch/unpack 接缝注入）
// ---------------------------------------------------------------------------

// fakeFetch 模拟内核 Fetch：写一枚占位包并核过摘要。
func fakeFetch(payload string) func(context.Context, artifact.Source, string, func(artifact.Progress), time.Duration) error {
	return func(_ context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), _ time.Duration) error {
		if src.SHA256 == "" {
			return errors.New("必须提供官方 SHA-256 摘要（托管下载不允许无校验安装）")
		}
		if err := os.WriteFile(destPath, []byte(payload), 0o644); err != nil {
			return err
		}
		if prog != nil {
			prog(artifact.Progress{Stage: artifact.StageDownload, Done: src.MaxBytes, Total: src.MaxBytes})
			prog(artifact.Progress{Stage: artifact.StageVerify})
		}
		return nil
	}
}

// fakeUnpack 模拟 rardecode：在 dstDir 写一枚假主程序（接缝签名不带 *testing.T，
// 故此处自己 MkdirAll + WriteFile，失败以 error 回传由被测代码收口）。
func fakeUnpack(name, ver, desc string) func(context.Context, string, string) (string, error) {
	return func(_ context.Context, _, dstDir string) (string, error) {
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return "", err
		}
		body := "MZ\x90\x00\x03\x00"
		if ver != "" {
			body += "VER=" + ver + "\x00"
		}
		if desc != "" {
			body += "DESC=" + desc + "\x00"
		}
		p := filepath.Join(dstDir, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			return "", err
		}
		return p, nil
	}
}

// collectingEmit 收集进度票据。
type collectingEmit struct {
	stages []string
	errs   []string
}

func (c *collectingEmit) fn(p DownloadProgress) {
	c.stages = append(c.stages, p.Stage)
	if p.Message != "" {
		c.errs = append(c.errs, p.Message)
	}
}

func TestDownloadContextHappyPath(t *testing.T) {
	seedRemoteCache(t, []byte(realReleaseBody))
	m, versions := newManagerAt(t)
	m.fetch = fakeFetch("rar-bytes")
	m.unpack = fakeUnpack("HEU_KMS_Activator_v64.0.0.exe", "64.0.0.0", productHint+"™")
	m.mirrors = assetMirrors

	emit := &collectingEmit{}
	if err := m.DownloadContext(context.Background(), "txn-happy", "64.0.0", emit.fn); err != nil {
		t.Fatalf("下载应成功: %v（%v）", err, emit.errs)
	}
	// 阶段词表：downloading → extract → done（verify 由内核折进 download，不造幻影步）
	joined := strings.Join(emit.stages, ",")
	if !strings.Contains(joined, "downloading") || !strings.Contains(joined, "extract") || !strings.Contains(joined, "done") {
		t.Fatalf("阶段序列不符: %s", joined)
	}

	dir := filepath.Join(versions, "heukms_64.0.0")
	exe := filepath.Join(dir, "HEU_KMS_Activator_v64.0.0.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("主程序未落位: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("账本未落盘: %v", err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("账本解析失败: %v", err)
	}
	if meta.Entry != "HEU_KMS_Activator_v64.0.0.exe" || meta.Source != artifact.SourceRemote || meta.Version != "64.0.0" {
		t.Fatalf("账本内容不符: %+v", meta)
	}
	// ZipSHA256 存的是**整包**摘要（内核幂等与防漂移比对的锚），AssetSHA256 是 exe 摘要
	if meta.ZipSHA256 != "7e4f770fcf41ef71c6c20949b818193e16c76e68c664571a6316b4b8d22832e5" {
		t.Fatalf("包摘要应记 GitHub 官方 digest，got %q", meta.ZipSHA256)
	}
	if len(meta.AssetSHA256) != 64 {
		t.Fatalf("exe 摘要未入账: %+v", meta)
	}
	// 独占中转目录必须已被清理
	left, _ := filepath.Glob(filepath.Join(versions, ".tmp-*"))
	if len(left) != 0 {
		t.Fatalf("残留中转目录: %v", left)
	}
	// 已装列表可回读（四段核账全过：exe 定位 / FileVersion==目录令牌 / 身份闸）
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 || list[0].Version != "64.0.0" || list[0].IsImport {
		t.Fatalf("已装列表不符: %+v err=%v", list, err)
	}
	if list[0].Source != artifact.SourceRemote {
		t.Fatalf("来源应记 remote，got %q", list[0].Source)
	}
}

func TestDownloadRejectsNonHostableVersion(t *testing.T) {
	seedRemoteCache(t, []byte(realReleaseBody))
	m, _ := newManagerAt(t)
	m.fetch = fakeFetch("x")
	m.unpack = fakeUnpack("HEU_KMS_Activator_v62.0.0.exe", "62.0.0", productHint)

	emit := &collectingEmit{}
	err := m.DownloadContext(context.Background(), "txn-62", "62.0.0", emit.fn)
	if err == nil {
		t.Fatal("62.0.0 无官方摘要，不在可安装表，必须拒绝")
	}
	// 错误话术要给出路（不是干巴巴的"版本不存在"）
	if !strings.Contains(err.Error(), "导入本地") {
		t.Fatalf("拒绝理由应指路导入通道，got %v", err)
	}
	if len(emit.stages) == 0 || emit.stages[len(emit.stages)-1] != "error" {
		t.Fatalf("失败必须以 error 票据收口，got %v", emit.stages)
	}
}

func TestDownloadQuadrupleGates(t *testing.T) {
	cases := []struct {
		name    string
		ver     string
		desc    string
		wantErr string
	}{
		{"版本与目录不符拒收", "99.0.0", productHint, "文件版本不匹配"},
		{"身份不符拒收", "64.0.0", "假冒程序", "FileDescription"},
		{"版本资源缺失拒收", "", productHint, "读取主程序版本失败"},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seedRemoteCache(t, []byte(realReleaseBody))
			m, _ := newManagerAt(t)
			m.fetch = fakeFetch("x")
			m.unpack = fakeUnpack("HEU_KMS_Activator_v64.0.0.exe", c.ver, c.desc)

			emit := &collectingEmit{}
			// txnID 必须是 ASCII 令牌（内核 ValidateVersionToken 白名单拒中文，
			// 用例最初就是被这道闸拦下才暴露的——闸门本身是对的，故编号取序数）
			err := m.DownloadContext(context.Background(), fmt.Sprintf("txn-gate-%d", i), "64.0.0", emit.fn)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("应因 %q 被拒，got %v", c.wantErr, err)
			}
			// 被拒的现场不得留半件版本目录
			if _, statErr := os.Stat(filepath.Join(m.versionsDir, "heukms_64.0.0")); statErr == nil {
				t.Fatal("三重闸失败后不应落位版本目录")
			}
		})
	}
}

func TestDownloadPropagatesFetchAndUnpackFailures(t *testing.T) {
	seedRemoteCache(t, []byte(realReleaseBody))

	t.Run("下载中断", func(t *testing.T) {
		m, _ := newManagerAt(t)
		m.fetch = func(context.Context, artifact.Source, string, func(artifact.Progress), time.Duration) error {
			return fmt.Errorf("curl exit 56: DNS 间歇失败")
		}
		m.unpack = fakeUnpack("a.exe", "64.0.0", productHint)
		emit := &collectingEmit{}
		err := m.DownloadContext(context.Background(), "txn-net", "64.0.0", emit.fn)
		if err == nil || !strings.Contains(err.Error(), "DNS") {
			t.Fatalf("下载失败应如实上抛，got %v", err)
		}
		if !strings.Contains(emit.errs[len(emit.errs)-1], "下载失败") {
			t.Fatalf("error 票据应含人话前缀，got %v", emit.errs)
		}
	})

	t.Run("解包失败不留半件", func(t *testing.T) {
		m, versions := newManagerAt(t)
		m.fetch = fakeFetch("x")
		m.unpack = func(context.Context, string, string) (string, error) {
			return "", errors.New("rardecode: bad file checksum")
		}
		emit := &collectingEmit{}
		if err := m.DownloadContext(context.Background(), "txn-crc", "64.0.0", emit.fn); err == nil {
			t.Fatal("CRC 校验失败必须拒装")
		}
		left, _ := filepath.Glob(filepath.Join(versions, ".tmp-*"))
		if len(left) != 0 {
			t.Fatalf("解包失败后中转目录未清理: %v", left)
		}
		if !strings.Contains(emit.errs[len(emit.errs)-1], "解包失败") {
			t.Fatalf("error 票据应说明解包失败，got %v", emit.errs)
		}
	})

	t.Run("取消上下文即刻失败", func(t *testing.T) {
		m, _ := newManagerAt(t)
		m.fetch = fakeFetch("x")
		m.unpack = fakeUnpack("HEU_KMS_Activator_v64.0.0.exe", "64.0.0", productHint)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := m.DownloadContext(ctx, "txn-cancel", "64.0.0", nil); err == nil {
			t.Fatal("已取消的 ctx 不得继续下载")
		}
	})
}

func TestAssetMirrorsOrderAndShape(t *testing.T) {
	urls := assetMirrors("64.0.0", "HEU_KMS_Activator_v64.0.0.rar")
	if len(urls) != 4 {
		t.Fatalf("应给出主址 + 3 镜像，got %d", len(urls))
	}
	if urls[0] != "https://github.com/zbezj/HEU_KMS_Activator/releases/download/64.0.0/HEU_KMS_Activator_v64.0.0.rar" {
		t.Fatalf("主址必须是 GitHub 直连，got %q", urls[0])
	}
	for _, u := range urls[1:] {
		if !strings.Contains(u, "https://github.com/zbezj/HEU_KMS_Activator/releases/download/") {
			t.Fatalf("镜像应是主址的前缀包裹（同摘要备用传输源），got %q", u)
		}
	}
}

// ---------------------------------------------------------------------------
// 版本串与形状判定
// ---------------------------------------------------------------------------

func TestNormalizeFileVersion(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"64.0.0", "64.0.0"},
		{" 64.0.0 ", "64.0.0"},
		{"64.0.0.0", "64.0.0"},
		{"1.2.3.4", "1.2.3.4"}, // 第四段非 0 不动（防误削真实修订号）
		{"", ""},
	} {
		if got := normalizeFileVersion(tc.in); got != tc.want {
			t.Errorf("normalizeFileVersion(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 主 exe 定位（文件名漂移 + 多 exe 歧义）
// ---------------------------------------------------------------------------

func TestFindHostedExe(t *testing.T) {
	t.Run("唯一 exe", func(t *testing.T) {
		dir := t.TempDir()
		want := writeFakeExe(t, dir, "HEU_KMS_Activator_v64.0.0.exe", "64.0.0", productHint)
		got, err := findHostedExe(dir)
		if err != nil || got != want {
			t.Fatalf("got %q err %v", got, err)
		}
	})
	t.Run("改名后仍可用", func(t *testing.T) {
		dir := t.TempDir()
		want := writeFakeExe(t, dir, "heu.exe", "64.0.0", productHint)
		got, err := findHostedExe(dir)
		if err != nil || got != want {
			t.Fatalf("单枚 exe 不苛求前缀，got %q err %v", got, err)
		}
	})
	t.Run("多枚按前缀筛", func(t *testing.T) {
		dir := t.TempDir()
		want := writeFakeExe(t, dir, "HEU_KMS_Activator_v63.4.0.exe", "63.4.0", productHint)
		writeFakeExe(t, dir, "Uninst.exe", "", "")
		got, err := findHostedExe(dir)
		if err != nil || got != want {
			t.Fatalf("应命中上游前缀那枚，got %q err %v", got, err)
		}
	})
	t.Run("两枚同前缀歧义拒收", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeExe(t, dir, "HEU_KMS_Activator_v64.0.0.exe", "64.0.0", productHint)
		writeFakeExe(t, dir, "HEU_KMS_Activator_v63.0.0.exe", "63.0.0", productHint)
		if _, err := findHostedExe(dir); err == nil {
			t.Fatal("歧义目录必须拒收，不得猜主程序")
		}
	})
	t.Run("空字节 exe 判不可用", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeExe(t, dir, "broken.exe", "1.0.0", productHint)
		if err := os.Truncate(filepath.Join(dir, "broken.exe"), 0); err != nil {
			t.Fatal(err)
		}
		if _, err := findHostedExe(dir); err == nil {
			t.Fatal("0 字节 exe 应判不可用")
		}
	})
	t.Run("无 exe 与不可访问目录", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := findHostedExe(dir); err == nil {
			t.Fatal("无 exe 应报错")
		}
		if _, err := findHostedExe(filepath.Join(dir, "not-exist")); err == nil {
			t.Fatal("目录不可访问应报错")
		}
	})
}

func TestResolveImportExe(t *testing.T) {
	t.Run("包装目录一层", func(t *testing.T) {
		root := t.TempDir()
		want := writeFakeExe(t, filepath.Join(root, "HEU_KMS_Activator_v64.0.0"), "HEU_KMS_Activator_v64.0.0.exe", "64.0.0", productHint)
		got, err := resolveImportExe(root)
		if err != nil || got != want {
			t.Fatalf("应下探一层包装目录，got %q err %v", got, err)
		}
	})
	t.Run("两个子目录都有 exe 拒收", func(t *testing.T) {
		root := t.TempDir()
		writeFakeExe(t, filepath.Join(root, "a"), "HEU_KMS_Activator_v64.0.0.exe", "64.0.0", productHint)
		writeFakeExe(t, filepath.Join(root, "b"), "HEU_KMS_Activator_v63.0.0.exe", "63.0.0", productHint)
		if _, err := resolveImportExe(root); err == nil {
			t.Fatal("歧义源目录必须拒收")
		}
	})
	t.Run("空源目录", func(t *testing.T) {
		if _, err := resolveImportExe(t.TempDir()); err == nil {
			t.Fatal("无 exe 应报错")
		}
	})
}

func TestRequireMZ(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.exe")
	if err := os.WriteFile(good, []byte("MZ\x90\x00rest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireMZ(good); err != nil {
		t.Fatalf("MZ 头应通过: %v", err)
	}
	bad := filepath.Join(dir, "bad.exe")
	if err := os.WriteFile(bad, []byte("PK\x03\x04zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireMZ(bad); err == nil {
		t.Fatal("zip 头冒充 exe 应拒收")
	}
	if err := requireMZ(filepath.Join(dir, "missing.exe")); err == nil {
		t.Fatal("文件缺失应报错")
	}
}

// ---------------------------------------------------------------------------
// ImportLocal（兜底通道）：落位、身份闸、重复导入、版本兜底
// ---------------------------------------------------------------------------

func TestImportLocalHappyPath(t *testing.T) {
	m, versions := newManagerAt(t)
	src := t.TempDir()
	srcExe := writeFakeExe(t, src, "HEU_KMS_Activator_v64.0.0.exe", "64.0.0.0", productHint+"™")

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if info.Version != "64.0.0" {
		t.Fatalf("版本应归一为 64.0.0，got %q", info.Version)
	}
	if info.ExeName != "HEU_KMS_Activator_v64.0.0.exe" {
		t.Fatalf("应保留上游原文件名（进程探测依赖），got %q", info.ExeName)
	}
	if filepath.Dir(info.ExePath) != filepath.Join(versions, "heukms_64.0.0") {
		t.Fatalf("落位目录形状不符: %s", info.ExePath)
	}
	if !info.IsImport || len(info.SHA256) != 64 {
		t.Fatalf("导入标记与摘要缺失: %+v", info)
	}
	if info.Size <= 0 {
		t.Fatalf("大小应为正值: %+v", info)
	}

	data, err := os.ReadFile(filepath.Join(filepath.Dir(info.ExePath), "meta.json"))
	if err != nil {
		t.Fatalf("账本未落盘: %v", err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("账本解析失败: %v", err)
	}
	if meta.Entry != "HEU_KMS_Activator_v64.0.0.exe" || meta.Source != artifact.SourceImported || meta.Version != "64.0.0" {
		t.Fatalf("账本内容不符: %+v", meta)
	}
	if meta.ZipSHA256 != "" {
		t.Fatalf("导入没有包概念，ZipSHA256 应为空: %+v", meta)
	}
	// 导入是拷贝不是移动（挂载光驱上也移不动）
	if _, err := os.Stat(srcExe); err != nil {
		t.Fatalf("源文件应原样保留: %v", err)
	}
	// 重复导入同版本 → 可执行的下一步，而不是内核那句"内容漂移"恐吓
	if _, err := m.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Fatalf("重复导入应报已安装，got %v", err)
	}
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 || list[0].Version != "64.0.0" || list[0].SHA256 != meta.AssetSHA256 {
		t.Fatalf("已装列表不符: %+v err=%v", list, err)
	}
	if got, err := m.ResolveExe("64.0.0"); err != nil || got != info.ExePath {
		t.Fatalf("ResolveExe 不符: %q err=%v", got, err)
	}
	if _, err := m.ResolveExe("v64.0.0"); err != nil {
		t.Fatalf("应容忍前端回传的 v 前缀: %v", err)
	}
}

func TestImportLocalRejectsNonHEU(t *testing.T) {
	m, _ := newManagerAt(t)
	src := t.TempDir()
	writeFakeExe(t, src, "SomeApp.exe", "1.0.0", "某无关程序")
	if _, err := m.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "FileDescription") {
		t.Fatalf("无关 exe 应被身份闸拒收，got %v", err)
	}
}

func TestImportLocalRejectsMissingPEIdentity(t *testing.T) {
	m, _ := newManagerAt(t)
	src := t.TempDir()
	writeFakeExe(t, src, "HEU_KMS_Activator_v64.0.0.exe", "", "")
	if _, err := m.ImportLocal(src); err == nil {
		t.Fatal("读不到 PE 身份资源不得算可信安装，应拒收")
	}
}

func TestImportLocalFallbackTokenWhenVersionUnreadable(t *testing.T) {
	m, versions := newManagerAt(t)
	src := t.TempDir()
	writeFakeExe(t, src, "HEU_KMS_Activator.exe", "", productHint)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("版本探测失败应走时间戳兜底而非拒绝导入: %v", err)
	}
	if !strings.HasPrefix(info.Version, "imported-") {
		t.Fatalf("兜底令牌形状不符: %q", info.Version)
	}
	if _, err := os.Stat(filepath.Join(versions, "heukms_"+info.Version)); err != nil {
		t.Fatalf("兜底目录未落位: %v", err)
	}
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 || list[0].Version != info.Version {
		t.Fatalf("兜底目录应出现在已装列表: %+v err=%v", list, err)
	}
}

func TestImportLocalRejectsBadSource(t *testing.T) {
	m, _ := newManagerAt(t)
	if _, err := m.ImportLocal(filepath.Join(t.TempDir(), "not-exist")); err == nil {
		t.Fatal("不存在的源目录应报错")
	}
	if _, err := m.ImportLocal("   "); err == nil {
		t.Fatal("空源目录应报错")
	}
}

// ---------------------------------------------------------------------------
// ListInstalled 健康闸（下载与导入两条链共用同一套账）
// ---------------------------------------------------------------------------

func TestListInstalledGates(t *testing.T) {
	m, versions := newManagerAt(t)

	writeFakeExe(t, filepath.Join(versions, "heukms_63.4.0"), "HEU_KMS_Activator_v63.4.0.exe", "63.4.0", productHint)
	writeFakeExe(t, filepath.Join(versions, "heukms_62.0.0"), "HEU_KMS_Activator_v62.0.0.exe", "99.99.99", productHint) // 名实不符
	writeFakeExe(t, filepath.Join(versions, "heukms_61.0.0"), "HEU_KMS_Activator_v61.0.0.exe", "61.0.0", "假冒程序")        // 身份不符
	if err := os.MkdirAll(filepath.Join(versions, "heukms_60.0.0"), 0o755); err != nil {
		t.Fatal(err) // 无 exe
	}
	writeFakeExe(t, filepath.Join(versions, "heukms_v1.2.3"), "HEU_KMS_Activator_v1.2.3.exe", "1.2.3", productHint) // 形状外目录

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 1 || list[0].Version != "63.4.0" {
		t.Fatalf("应只保留通过三重闸的安装，got %+v", list)
	}
}

func TestListInstalledEmptyTree(t *testing.T) {
	m, _ := newManagerAt(t)
	list, err := m.ListInstalled()
	if err != nil || len(list) != 0 {
		t.Fatalf("全新安装（版本树未建）应返回空列表而非错误，got %+v err=%v", list, err)
	}
}

func TestListRemoteCopiesCacheRow(t *testing.T) {
	seedRemoteCache(t, []byte(realReleaseBody))
	m := newTestManager()
	list, err := m.ListRemote()
	if err != nil || len(list) != 2 {
		t.Fatalf("ListRemote 应命中缓存并给两条，got %+v err=%v", list, err)
	}
	if list[0].Form != hostfeed.FormPortable {
		t.Fatalf("形态标注应在解析期一次回填，got %q", list[0].Form)
	}
}

// ---------------------------------------------------------------------------
// resolveVersionDir / Remove
// ---------------------------------------------------------------------------

func TestResolveVersionDirRejectsTraversal(t *testing.T) {
	m, _ := newManagerAt(t)
	for _, bad := range []string{"", "..", "../evil", "64.0.0/..", `64.0.0\..\x`, "a b", "-x", "x.", strings.Repeat("9", 70)} {
		if _, _, err := m.resolveVersionDir(bad); err == nil {
			t.Errorf("非法令牌应被拒（先于任何磁盘访问）: %q", bad)
		}
	}
	if _, _, err := m.resolveVersionDir("64.0.0"); err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("未安装应给人话指引，got %v", err)
	}
}

func TestRemoveVersion(t *testing.T) {
	m, _ := newManagerAt(t)
	src := t.TempDir()
	writeFakeExe(t, src, "HEU_KMS_Activator_v64.0.0.exe", "64.0.0", productHint)
	if _, err := m.ImportLocal(src); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if err := m.Remove("64.0.0"); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	if list, err := m.ListInstalled(); err != nil || len(list) != 0 {
		t.Fatalf("卸载后列表应为空，got %+v err=%v", list, err)
	}
	if err := m.Remove("64.0.0"); err == nil {
		t.Fatal("卸载不存在的版本应报错")
	}
}
