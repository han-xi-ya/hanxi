package ocr

// ---------- F7 托管安装契约核心单测（zip fixture 全部现造） ----------

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zipEntry struct {
	name  string
	body  string
	isDir bool
}

// makeHostedZip 按条目表构建 zip 安装包。
func makeHostedZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		name := e.name
		if e.isDir {
			name += "/"
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if !e.isDir {
			if _, err := io.WriteString(w, e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// makeValidHostedZip 造一个契约合法的包（可覆写 manifest 片段与条目增删）。
func makeValidHostedZip(t *testing.T, dir, engine, version string) string {
	t.Helper()
	path := filepath.Join(dir, "hanxi-ocr-"+engine+"-"+version+".zip")
	makeHostedZip(t, path, hostedZipEntries(engine, version, "1.0"))
	return path
}

func hostedZipEntries(engine, version, extra string) []zipEntry {
	manifest, _ := json.Marshal(hostedManifest{
		Schema: hostedManifestSchema, Engine: engine, Version: version,
		Entry: serviceExeName, MinHanxi: "", Note: "测试包 " + version,
	})
	return []zipEntry{
		{name: manifestName, body: string(manifest)},
		{name: serviceExeName, body: "MZ-exe-" + version},
		{name: "models", isDir: true},
		{name: "models/model.onnx", body: extra},
	}
}

// writeHostedSidecar 计算并旁挂 sha256（withBOM/尾空白用于宽容性测试）。
func writeHostedSidecar(t *testing.T, zipPath string) {
	t.Helper()
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := hex.EncodeToString(func() []byte { h := sha256.Sum256(raw); return h[:] }())
	if err := os.WriteFile(zipPath+".sha256", []byte(sum+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func newTestHostedManager(t *testing.T) *hostedManager {
	t.Helper()
	base := t.TempDir()
	return newHostedManager(filepath.Join(base, "versions", hostedDirName), filepath.Join(base, "installers", hostedDirName))
}

// ---------- manifest 矩阵 ----------

func TestHostedManifestValidation(t *testing.T) {
	ok := hostedManifest{Schema: 1, Engine: "paddle", Version: "4.1.15.9", Entry: serviceExeName}
	if err := ok.validate(); err != nil {
		t.Fatalf("合法 manifest 被拒: %v", err)
	}
	if err := (hostedManifest{Schema: 1, Engine: "wechat", Version: "0.4.0-alpha", Entry: "HANXI-OCR.EXE"}).validate(); err != nil {
		t.Fatalf("entry 大小写应宽容: %v", err)
	}
	cases := []struct {
		name string
		m    hostedManifest
		want string
	}{
		{"schema 不符", hostedManifest{Schema: 2, Engine: "wechat", Version: "1.0", Entry: serviceExeName}, "schema"},
		{"engine 未知", hostedManifest{Schema: 1, Engine: "cuda", Version: "1.0", Entry: serviceExeName}, "engine"},
		{"version 空", hostedManifest{Schema: 1, Engine: "wechat", Version: "", Entry: serviceExeName}, "version"},
		{"version 注入", hostedManifest{Schema: 1, Engine: "wechat", Version: `a/../b`, Entry: serviceExeName}, "非法字符"},
		{"version 尾点", hostedManifest{Schema: 1, Engine: "wechat", Version: "1.0.", Entry: serviceExeName}, "不得以"},
		{"version 尾横线", hostedManifest{Schema: 1, Engine: "wechat", Version: "1.0-", Entry: serviceExeName}, "不得以"},
		{"version 过长", hostedManifest{Schema: 1, Engine: "wechat", Version: strings.Repeat("a", 70), Entry: serviceExeName}, "非法字符"},
		{"entry 非契约名", hostedManifest{Schema: 1, Engine: "wechat", Version: "1.0", Entry: "evil.exe"}, "entry"},
	}
	for _, c := range cases {
		if err := c.m.validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: got %v, want 含 %q", c.name, err, c.want)
		}
	}
}

// ---------- 校验链（inspect + sha256 旁挂） ----------

func TestInspectHostedZipMatrix(t *testing.T) {
	dir := t.TempDir()

	// 合法包
	good := makeValidHostedZip(t, dir, "wechat", "4.1.15.9")
	m, _, err := inspectHostedZip(good)
	if err != nil || m.Engine != EngineWechat || m.Version != "4.1.15.9" {
		t.Fatalf("合法包被拒: %v (%+v)", err, m)
	}

	// 缺 manifest
	p := filepath.Join(dir, "no-manifest.zip")
	makeHostedZip(t, p, []zipEntry{{name: serviceExeName, body: "x"}})
	if _, _, e := inspectHostedZip(p); e == nil || !strings.Contains(e.Error(), manifestName) {
		t.Fatalf("缺 manifest 应报错: %v", e)
	}

	// 嵌套外层文件夹（manifest 不在根）
	p = filepath.Join(dir, "nested.zip")
	makeHostedZip(t, p, []zipEntry{
		{name: "hanxi-ocr-1.0.0/" + manifestName, body: "{}"},
		{name: "hanxi-ocr-1.0.0/" + serviceExeName, body: "x"},
	})
	if _, _, e := inspectHostedZip(p); e == nil || !strings.Contains(e.Error(), "勿嵌套") {
		t.Fatalf("嵌套布局应拒收: %v", e)
	}

	// 缺入口 exe
	manifest, _ := json.Marshal(hostedManifest{Schema: 1, Engine: "paddle", Version: "1.0", Entry: serviceExeName})
	p = filepath.Join(dir, "no-exe.zip")
	makeHostedZip(t, p, []zipEntry{{name: manifestName, body: string(manifest)}})
	if _, _, e := inspectHostedZip(p); e == nil || !strings.Contains(e.Error(), serviceExeName) {
		t.Fatalf("缺入口应报错: %v", e)
	}

	// ZipSlip 条目
	p = filepath.Join(dir, "slip.zip")
	makeHostedZip(t, p, append(hostedZipEntries("paddle", "1.0", "x"), zipEntry{name: "../evil.txt", body: "x"}))
	if _, _, e := inspectHostedZip(p); e == nil || !strings.Contains(e.Error(), "非法路径") {
		t.Fatalf("ZipSlip 应拒收: %v", e)
	}

	// 文件名与 manifest 矛盾（版本、引擎各一例）
	p = filepath.Join(dir, "hanxi-ocr-paddle-9.9.9.zip")
	makeHostedZip(t, p, hostedZipEntries("paddle", "1.0.0", "x"))
	if _, _, e := inspectHostedZip(p); e == nil || !strings.Contains(e.Error(), "矛盾") {
		t.Fatalf("版本矛盾应拒收: %v", e)
	}
	p = filepath.Join(dir, "hanxi-ocr-wechat-1.0.0.zip")
	makeHostedZip(t, p, hostedZipEntries("paddle", "1.0.0", "x"))
	if _, _, e := inspectHostedZip(p); e == nil || !strings.Contains(e.Error(), "矛盾") {
		t.Fatalf("引擎矛盾应拒收: %v", e)
	}

	// 不规范文件名 → 以 manifest 为准（宽容侧）
	p = filepath.Join(dir, "随便起的名字.zip")
	makeHostedZip(t, p, hostedZipEntries("paddle", "2.0", "x"))
	if m, _, e := inspectHostedZip(p); e != nil || m.Version != "2.0" {
		t.Fatalf("非规范名应以 manifest 为准: %v %+v", e, m)
	}

	// 文件不存在
	if _, _, e := inspectHostedZip(filepath.Join(dir, "x.rar")); e == nil || !strings.Contains(e.Error(), "不存在") {
		t.Fatalf("不存在文件应报错: %v", e)
	}
	// 存在但非 .zip 扩展名
	if _, _, e := inspectHostedZip(good + ".sha256"); e == nil || !strings.Contains(e.Error(), ".zip") {
		t.Fatalf("非 zip 扩展名应拒收: %v", e)
	}
}

func TestVerifyHostedZipSHA(t *testing.T) {
	dir := t.TempDir()
	good := makeValidHostedZip(t, dir, "paddle", "1.0")

	// 缺失旁挂件 → 拒收（三件套契约）
	if _, e := verifyHostedZipSHA(good); e == nil || !strings.Contains(e.Error(), "缺少校验文件") {
		t.Fatalf("缺旁挂应拒收: %v", e)
	}

	// 一致通过
	writeHostedSidecar(t, good)
	if _, e := verifyHostedZipSHA(good); e != nil {
		t.Fatalf("一致哈希应通过: %v", e)
	}

	// 篡改 zip → 不符
	if err := os.WriteFile(good, append([]byte{}, mustRead(t, good)...), 0644); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(good, os.O_APPEND|os.O_WRONLY, 0644)
	_, _ = f.WriteString("tamper")
	f.Close()
	if _, e := verifyHostedZipSHA(good); e == nil || !strings.Contains(e.Error(), "SHA256 校验失败") {
		t.Fatalf("篡改应被拦: %v", e)
	}

	// BOM + 尾空白 + 大写 hex 宽容（重新造干净包）
	good = makeValidHostedZip(t, dir, "wechat", "2.0")
	raw := mustRead(t, good)
	sum := strings.ToUpper(hex.EncodeToString(func() []byte { h := sha256.Sum256(raw); return h[:] }()))
	payload := "\uFEFF" + sum + "  hanxi-ocr-wechat-2.0.zip" + "\r\n \n"
	if err := os.WriteFile(good+".sha256", []byte(payload), 0644); err != nil {
		t.Fatal(err)
	}
	if _, e := verifyHostedZipSHA(good); e != nil {
		t.Fatalf("BOM/尾空白/大写应宽容: %v", e)
	}

	// 坏旁挂内容
	if err := os.WriteFile(good+".sha256", []byte("not-a-hash"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, e := verifyHostedZipSHA(good); e == nil {
		t.Fatal("非法摘要文本应报错")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ---------- 炸弹上限 ----------

func TestHostedZipBombCaps(t *testing.T) {
	dir := t.TempDir()

	// 总展开超上限：inspect 阶段按声明值拒收
	origTotal := maxHostedTotalBytes
	maxHostedTotalBytes = 8
	defer func() { maxHostedTotalBytes = origTotal }()
	p := makeValidHostedZip(t, dir, "paddle", "1.0")
	if _, _, e := inspectHostedZip(p); e == nil || !strings.Contains(e.Error(), "超出托管上限") {
		t.Fatalf("总展开越限应拒收: %v", e)
	}

	// 条目数上限
	origEntries := maxHostedEntries
	maxHostedEntries = 2
	defer func() { maxHostedEntries = origEntries }()
	maxHostedTotalBytes = 1 << 40
	p = makeValidHostedZip(t, dir, "paddle", "2.0")
	if _, _, e := inspectHostedZip(p); e == nil || !strings.Contains(e.Error(), "条目数") {
		t.Fatalf("条目数越限应拒收: %v", e)
	}
}

func TestExtractHostedZipCapsAndSlip(t *testing.T) {
	dir := t.TempDir()
	p := makeValidHostedZip(t, dir, "paddle", "1.0")

	// 单文件展开预算（declare 通过 inspect 不代表 extract 放行，双层闸）
	origFile := maxHostedFileBytes
	maxHostedFileBytes = 4
	defer func() { maxHostedFileBytes = origFile }()
	out := filepath.Join(dir, "x")
	if e := extractHostedZip(p, out); e == nil || !strings.Contains(e.Error(), "超") {
		t.Fatalf("单文件越限应中止: %v", e)
	}

	// ZipSlip 在解压层同样拒绝（纵深防御）
	maxHostedFileBytes = origFile
	slip := filepath.Join(dir, "slip.zip")
	makeHostedZip(t, slip, []zipEntry{
		{name: "ok.txt", body: "x"},
		{name: "../escaped.txt", body: "x"},
	})
	if e := extractHostedZip(slip, out); e == nil || !strings.Contains(e.Error(), "非法路径") {
		t.Fatalf("解压层 ZipSlip 应拒绝: %v", e)
	}
}

// ---------- 安装：happy path / 重复覆盖 / 失败无残留 ----------

func TestHostedInstallHappyPath(t *testing.T) {
	hm := newTestHostedManager(t)
	zipPath := makeValidHostedZip(t, t.TempDir(), "paddle", "0.4.0-alpha")
	writeHostedSidecar(t, zipPath)

	hv, err := hm.installZip(zipPath)
	if err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if hv.Engine != "paddle" || hv.Version != "0.4.0-alpha" || hv.State != hostedStateReady {
		t.Fatalf("回执异常: %+v", hv)
	}
	wantDir := filepath.Join(hm.versionsRoot, "paddle-0.4.0-alpha")
	if hv.Dir != wantDir || hv.ExePath != filepath.Join(wantDir, serviceExeName) {
		t.Fatalf("落位路径异常: %+v", hv)
	}
	for _, f := range []string{serviceExeName, manifestName, "meta.json", filepath.Join("models", "model.onnx")} {
		if !isRegularFile(filepath.Join(wantDir, f)) {
			t.Fatalf("版本目录缺少 %s", f)
		}
	}
	if hv.InstalledAt == "" {
		t.Fatal("InstalledAt 应为当前时间格式")
	}
	// zip 与旁挂件移存 installers/，原位消失
	if _, e := os.Stat(zipPath); !os.IsNotExist(e) {
		t.Fatal("zip 原件应移走")
	}
	if _, e := os.Stat(zipPath + ".sha256"); !os.IsNotExist(e) {
		t.Fatal("旁挂件应随包移走")
	}
	if !isRegularFile(filepath.Join(hm.installersRoot, filepath.Base(zipPath))) {
		t.Fatal("zip 应归档进 installers/")
	}
}

func TestHostedInstallOverwriteSameVersion(t *testing.T) {
	hm := newTestHostedManager(t)
	dir := t.TempDir()

	zipPath := makeValidHostedZip(t, dir, "wechat", "1.0.0")
	writeHostedSidecar(t, zipPath)
	if _, err := hm.installZip(zipPath); err != nil {
		t.Fatal(err)
	}

	// 重复安装同版本：覆盖成功、无 tmp/old 残留、只有一份版本目录
	zipPath2 := filepath.Join(t.TempDir(), "hanxi-ocr-wechat-1.0.0.zip")
	if err := os.WriteFile(zipPath2, mustRead(t, filepath.Join(hm.installersRoot, filepath.Base(zipPath))), 0644); err != nil {
		t.Fatal(err)
	}
	writeHostedSidecar(t, zipPath2)
	if _, err := hm.installZip(zipPath2); err != nil {
		t.Fatalf("重复覆盖安装应成功: %v", err)
	}
	entries, _ := os.ReadDir(hm.versionsRoot)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") || strings.Contains(e.Name(), ".old-") {
			t.Fatalf("残留半成品: %s", e.Name())
		}
		if e.Name() == "wechat-1.0.0" && !e.IsDir() {
			t.Fatal("版本目录形态异常")
		}
	}
	if n := len(hm.list()); n != 1 {
		t.Fatalf("覆盖后应仍只有一份版本, got %d", n)
	}
}

func TestHostedInstallFailureLeavesTargetIntact(t *testing.T) {
	hm := newTestHostedManager(t)
	target := filepath.Join(hm.versionsRoot, "paddle-1.0.0")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "KEEP.txt")
	if err := os.WriteFile(marker, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	zipPath := makeValidHostedZip(t, t.TempDir(), "paddle", "1.0.0")
	writeHostedSidecar(t, zipPath)

	// 解压中途越限 → 报错，旧版本目录原封不动，无 tmp 残留
	origFile := maxHostedFileBytes
	maxHostedFileBytes = 4
	defer func() { maxHostedFileBytes = origFile }()
	if _, err := hm.installZip(zipPath); err == nil || !strings.Contains(err.Error(), "超") {
		t.Fatalf("应解压失败: %v", err)
	}
	if !isRegularFile(marker) {
		t.Fatal("失败安装不得动旧版本")
	}
	if _, e := os.Stat(filepath.Join(target, serviceExeName)); !os.IsNotExist(e) {
		t.Fatal("旧目录不应混入新文件")
	}
	entries, _ := os.ReadDir(hm.versionsRoot)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), hostedTmpPrefix) {
			t.Fatalf("tmp 残留未清理: %s", e.Name())
		}
	}
	// 校验失败的 zip 原位保留（未被移存）
	if !isRegularFile(zipPath) {
		t.Fatal("拒收包应留在原位供用户排查")
	}
}

// ---------- 列表 / 解析 / 卸载 ----------

func TestHostedList(t *testing.T) {
	hm := newTestHostedManager(t)
	mk := func(name string, withExe, withManifest bool) {
		d := filepath.Join(hm.versionsRoot, name)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if withExe {
			if err := os.WriteFile(filepath.Join(d, serviceExeName), []byte("MZ"), 0755); err != nil {
				t.Fatal(err)
			}
		}
		if withManifest {
			m, _ := json.Marshal(hostedManifest{Schema: 1, Note: "说明-" + name})
			if err := os.WriteFile(filepath.Join(d, manifestName), m, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("wechat-4.1.15.9", true, true)
	mk("wechat-9.9.9", false, true) // broken：入口缺失
	mk("paddle-0.4.0-alpha", true, true)
	mk("paddle-0.4.0-beta", true, true)
	mk("not-a-version", true, false)
	if err := os.MkdirAll(filepath.Join(hm.versionsRoot, hostedTmpPrefix+"junk"), 0755); err != nil {
		t.Fatal(err)
	}

	list := hm.list()
	if len(list) != 4 {
		t.Fatalf("列表应 4 项（乱名与 tmp 忽略）: %+v", list)
	}
	// engineOrder：wechat 在前；engine 内版本降序
	want := []struct{ engine, version string }{
		{"wechat", "9.9.9"}, {"wechat", "4.1.15.9"},
		{"paddle", "0.4.0-beta"}, {"paddle", "0.4.0-alpha"},
	}
	for i, w := range want {
		if list[i].Engine != w.engine || list[i].Version != w.version {
			t.Fatalf("第 %d 项 = %s/%s, want %s/%s", i, list[i].Engine, list[i].Version, w.engine, w.version)
		}
	}
	if list[0].State != hostedStateBroken || list[0].Error == "" {
		t.Fatalf("缺入口应列 broken 态: %+v", list[0])
	}
	if list[1].State != hostedStateReady || !strings.HasPrefix(list[1].Note, "说明-") {
		t.Fatalf("正常态应带 note: %+v", list[1])
	}
	if list[1].InstalledAt == "" {
		t.Fatal("无 meta.json 应回退目录 mtime 时间")
	}
}

func TestHostedResolveLatest(t *testing.T) {
	root := filepath.Join(t.TempDir(), hostedDirName)
	mkVer(t, root, "paddle", "1.2", false)
	mkVer(t, root, "paddle", "1.10", false)
	mkVer(t, root, "paddle", "2.0", true) // 缺 manifest：不可信，跳过
	mkVer(t, root, "wechat", "4.1.15.9", false)

	exe, ver, ok := hostedResolveLatest(root, EnginePaddle)
	if !ok || ver != "1.10" || exe != filepath.Join(root, "paddle-1.10", serviceExeName) {
		t.Fatalf("paddle 最新解析 = %s/%s/%v, want 1.10（数值段比较）", exe, ver, ok)
	}
	if _, ver, ok := hostedResolveLatest(root, EngineWechat); !ok || ver != "4.1.15.9" {
		t.Fatalf("wechat 解析失败: %s %v", ver, ok)
	}
	if _, _, ok := hostedResolveLatest(root, "cuda"); ok {
		t.Fatal("未知引擎应 ok=false")
	}
	if _, _, ok := hostedResolveLatest(filepath.Join(root, "gone"), EnginePaddle); ok {
		t.Fatal("不存在根目录应 ok=false")
	}
	if _, _, ok := hostedResolveLatest("", EnginePaddle); ok {
		t.Fatal("空根目录（未接线）应 ok=false")
	}
}

func mkVer(t *testing.T, root, engine, version string, withoutManifest bool) string {
	t.Helper()
	d := filepath.Join(root, hostedVersionDirName(engine, version))
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, serviceExeName), []byte("MZ"), 0755); err != nil {
		t.Fatal(err)
	}
	if !withoutManifest {
		m, _ := json.Marshal(hostedManifest{Schema: 1, Engine: engine, Version: version, Entry: serviceExeName})
		if err := os.WriteFile(filepath.Join(d, manifestName), m, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestCompareHostedVersion(t *testing.T) {
	cases := [][3]string{
		{"1.2", "1.10", "-1"},
		{"4.1.15.9", "4.1.9.9", "1"},
		{"1.0", "1.0", "0"},
		{"1.0.1", "1.0", "1"},
		{"0.4.0-alpha", "0.4.1", "-1"},
	}
	for _, c := range cases {
		if got := signI(compareHostedVersion(c[0], c[1])); got != c[2] {
			t.Fatalf("compare(%s,%s)=%s, want %s", c[0], c[1], got, c[2])
		}
	}
}

func signI(n int) string {
	switch {
	case n < 0:
		return "-1"
	case n > 0:
		return "1"
	}
	return "0"
}

func TestHostedRemove(t *testing.T) {
	hm := newTestHostedManager(t)
	d := mkVer(t, hm.versionsRoot, "paddle", "1.0.0", false)
	if _, err := hm.remove("paddle", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(d); !os.IsNotExist(e) {
		t.Fatal("版本目录应已删除")
	}
	if _, err := hm.remove("paddle", "1.0.0"); err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("重复卸载应报未安装: %v", err)
	}
	if _, err := hm.remove("paddle", "../wechat-9.9.9"); err == nil || !strings.Contains(err.Error(), "非法") {
		t.Fatalf("注入名应拒绝: %v", err)
	}
	if _, err := hm.remove("cuda", "1.0"); err == nil {
		t.Fatal("未知引擎名应拒绝")
	}
}

// ---------- 解析链：托管优先与失效自愈（F7 卡片 3） ----------

func TestResolveServiceExeHostedFirst(t *testing.T) {
	base := t.TempDir()
	hanxiDir := filepath.Join(base, "hanxi")
	dataDir := filepath.Join(base, "hanxidata")
	versionsRoot := filepath.Join(base, "versions", hostedDirName)
	if err := os.MkdirAll(hanxiDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 旧同级锚点（托管前世界的默认落位）
	sibling := filepath.Join(base, "hanxi-ocr")
	mkComponentDir(t, sibling, false)
	siblingExe := filepath.Join(sibling, serviceExeName)

	// 无托管树 → 旧行为：同级自动发现
	got, fromStore, err := resolveServiceExe(hanxiDir, dataDir, versionsRoot, EngineWechat, "")
	if err != nil || got != siblingExe || fromStore {
		t.Fatalf("无托管树应走旧同级锚点: %v/%v/%v", got, fromStore, err)
	}

	// 装两个托管版本 → 托管最新版压过同级
	w1 := mkVer(t, versionsRoot, "wechat", "1.0", false)
	w2 := mkVer(t, versionsRoot, "wechat", "1.2", false)
	got, fromStore, err = resolveServiceExe(hanxiDir, dataDir, versionsRoot, EngineWechat, "")
	want := filepath.Join(w2, serviceExeName)
	if err != nil || got != want || fromStore {
		t.Fatalf("托管优先 = %v/%v/%v, want %s", got, fromStore, err, want)
	}

	// 树外显式登记件仍在位 → 用户意图优先（48MB 手动指定行为不动）
	got, fromStore, err = resolveServiceExe(hanxiDir, dataDir, versionsRoot, EngineWechat, siblingExe)
	if err != nil || got != siblingExe || !fromStore {
		t.Fatalf("树外登记件应最高优先: %v/%v/%v", got, fromStore, err)
	}

	// 托管登记件在位 → 命中登记版本（非最新）
	reg := filepath.Join(w1, serviceExeName)
	got, fromStore, err = resolveServiceExe(hanxiDir, dataDir, versionsRoot, EngineWechat, reg)
	if err != nil || got != reg || !fromStore {
		t.Fatalf("托管登记件 = %v/%v/%v", got, fromStore, err)
	}

	// 登记版本被卸载 → 自愈回退到托管树最新（不报"已失效"）
	if err := os.RemoveAll(w1); err != nil {
		t.Fatal(err)
	}
	got, fromStore, err = resolveServiceExe(hanxiDir, dataDir, versionsRoot, EngineWechat, reg)
	if err != nil || got != want || fromStore {
		t.Fatalf("卸载自愈 = %v/%v/%v, want 最新版 %s", got, fromStore, err, want)
	}

	// 引擎整体卸载 → 落回旧锚点链（同级仍在位）
	if err := os.RemoveAll(versionsRoot); err != nil {
		t.Fatal(err)
	}
	got, _, err = resolveServiceExe(hanxiDir, dataDir, versionsRoot, EngineWechat, reg)
	if err != nil || got != siblingExe {
		t.Fatalf("整体卸载应落回旧锚点: %v/%v", got, err)
	}

	// 树外登记件失效 → 一律明示"已失效"，不静默回退
	if _, _, err := resolveServiceExe(hanxiDir, dataDir, versionsRoot, EngineWechat, filepath.Join(base, "gone.exe")); err == nil ||
		!strings.Contains(err.Error(), "失效") {
		t.Fatalf("树外失效登记应明示: %v", err)
	}
}

func TestHostedDirOfExe(t *testing.T) {
	root := filepath.Join(t.TempDir(), hostedDirName)
	inside := filepath.Join(root, "paddle-1.0", serviceExeName)
	if got := hostedDirOfExe(root, inside); got != filepath.Join(root, "paddle-1.0") {
		t.Fatalf("树内登记件应回目录: %q", got)
	}
	if got := hostedDirOfExe(root, filepath.Join(root, serviceExeName)); got != "" {
		t.Fatalf("根下散文件不属于任何版本: %q", got)
	}
	if got := hostedDirOfExe(root, filepath.Join(root, "junk-dir", serviceExeName)); got != "" {
		t.Fatalf("不合规目录名不应认账: %q", got)
	}
	if got := hostedDirOfExe(root, `C:\somewhere\else\hanxi-ocr.exe`); got != "" {
		t.Fatalf("树外路径应为空: %q", got)
	}
	if got := hostedDirOfExe("", inside); got != "" {
		t.Fatalf("未接线根应为空: %q", got)
	}
}
