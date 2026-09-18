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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/modules/ocr/instance"
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

func hostedZipEntriesWithMin(engine, version, minHanxi, extra string) []zipEntry {
	manifest, _ := json.Marshal(hostedManifest{
		Schema: hostedManifestSchema, Engine: engine, Version: version,
		Entry: serviceExeName, MinHanxi: minHanxi, Note: "测试包 " + version,
	})
	return []zipEntry{
		{name: manifestName, body: string(manifest)},
		{name: serviceExeName, body: "MZ-exe-" + version},
		{name: "models", isDir: true},
		{name: "models/model.onnx", body: extra},
	}
}

func hostedZipEntries(engine, version, extra string) []zipEntry {
	return hostedZipEntriesWithMin(engine, version, "", extra)
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
	hm := newHostedManager(filepath.Join(base, "versions", hostedDirName), filepath.Join(base, "installers", hostedDirName))
	t.Cleanup(func() { deleteHostedTreeLock(hm.versionsRoot) })
	return hm
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

func TestHostedMinHanxiSemverMatrix(t *testing.T) {
	cases := []struct {
		name    string
		minimum string
		current string
		wantErr string
	}{
		{name: "空值兼容旧包", minimum: "", current: "0.3.0"},
		{name: "低于当前", minimum: "0.2.9", current: "0.3.0"},
		{name: "恰等于当前", minimum: "0.3.0", current: "0.3.0"},
		{name: "构建元数据不影响优先级", minimum: "0.3.0+pack.7", current: "0.3.0+hanxi.2"},
		{name: "当前正式版高于同版本预发布", minimum: "0.3.0-rc.1", current: "0.3.0"},
		{name: "当前预发布低于正式版", minimum: "0.3.0", current: "0.3.0-rc.1", wantErr: "请先升级"},
		{name: "数字预发布按数值", minimum: "0.3.0-rc.10", current: "0.3.0-rc.2", wantErr: "请先升级"},
		{name: "最低版本更高", minimum: "0.4.0", current: "0.3.0", wantErr: "请先升级"},
		{name: "最低版本非法", minimum: "0.3", current: "0.3.0", wantErr: "minHanxi"},
		{name: "最低版本前导零非法", minimum: "0.03.0", current: "0.3.0", wantErr: "minHanxi"},
		{name: "当前版本非法", minimum: "0.3.0", current: "dev", wantErr: "当前 Hanxi 版本"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMinHanxi(tc.minimum, tc.current)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error = %v, want 含 %q", err, tc.wantErr)
			}
		})
	}
}

func TestInspectHostedZipRejectsMinHanxiBeforeExtraction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hanxi-ocr-paddle-1.0.0.zip")
	makeHostedZip(t, path, hostedZipEntriesWithMin("paddle", "1.0.0", "999.0.0", "model"))
	if _, _, err := inspectHostedZip(path); err == nil || !strings.Contains(err.Error(), "请先升级") {
		t.Fatalf("更高 minHanxi 应在解压前拒收: %v", err)
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

func TestHostedInstallIdempotentSameVersion(t *testing.T) {
	hm := newTestHostedManager(t)
	dir := t.TempDir()

	zipPath := makeValidHostedZip(t, dir, "wechat", "1.0.0")
	writeHostedSidecar(t, zipPath)
	if _, err := hm.installZip(zipPath); err != nil {
		t.Fatal(err)
	}

	// 重复安装同版本同哈希：幂等成功、无 tmp/old 残留、只有一份版本目录
	zipPath2 := filepath.Join(t.TempDir(), "hanxi-ocr-wechat-1.0.0.zip")
	if err := os.WriteFile(zipPath2, mustRead(t, filepath.Join(hm.installersRoot, filepath.Base(zipPath))), 0644); err != nil {
		t.Fatal(err)
	}
	writeHostedSidecar(t, zipPath2)
	if _, err := hm.installZip(zipPath2); err != nil {
		t.Fatalf("重复幂等安装应成功: %v", err)
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
		t.Fatalf("幂等后应仍只有一份版本, got %d", n)
	}
}

func TestHostedInstallSameVersionHashPolicy(t *testing.T) {
	hm := newTestHostedManager(t)
	firstDir := t.TempDir()
	first := makeValidHostedZip(t, firstDir, "wechat", "1.0.0")
	writeHostedSidecar(t, first)
	if _, err := hm.installZip(first); err != nil {
		t.Fatal(err)
	}
	installedExe := filepath.Join(hm.versionsRoot, "wechat-1.0.0", serviceExeName)
	before := mustRead(t, installedExe)
	archived := filepath.Join(hm.installersRoot, filepath.Base(first))

	// 同 engine+version 且包哈希相同：幂等成功，不改版本树；源包也不归档/删除。
	same := filepath.Join(t.TempDir(), filepath.Base(first))
	if err := os.WriteFile(same, mustRead(t, archived), 0644); err != nil {
		t.Fatal(err)
	}
	writeHostedSidecar(t, same)
	if hv, err := hm.installZip(same); err != nil || hv.State != hostedStateReady {
		t.Fatalf("同哈希应幂等成功: %+v %v", hv, err)
	}
	if !isRegularFile(same) || !isRegularFile(same+".sha256") {
		t.Fatal("幂等命中不应移动调用方安装包")
	}
	if got := mustRead(t, installedExe); string(got) != string(before) {
		t.Fatal("幂等安装不得改写已装内容")
	}

	// 同 engine+version 但包哈希不同：拒绝覆盖，并要求发布新版本号。
	different := filepath.Join(t.TempDir(), filepath.Base(first))
	makeHostedZip(t, different, hostedZipEntries("wechat", "1.0.0", "different-model"))
	writeHostedSidecar(t, different)
	if _, err := hm.installZip(different); err == nil || !strings.Contains(err.Error(), "新版本号") {
		t.Fatalf("不同哈希应拒绝覆盖: %v", err)
	}
	if got := mustRead(t, installedExe); string(got) != string(before) {
		t.Fatal("拒绝覆盖后已装内容必须保持不变")
	}
}

func TestHostedInstallConcurrentSameVersion(t *testing.T) {
	hm := newTestHostedManager(t)
	const workers = 8
	packages := make([]string, workers)
	for i := range packages {
		dir := t.TempDir()
		packages[i] = makeValidHostedZip(t, dir, "paddle", "1.2.3")
		writeHostedSidecar(t, packages[i])
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var failures atomic.Int32
	for _, packagePath := range packages {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			<-start
			if _, err := hm.installZip(path); err != nil {
				failures.Add(1)
			}
		}(packagePath)
	}
	close(start)
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("并发同哈希安装应全部幂等成功，失败 %d 次", failures.Load())
	}
	if list := hm.list(); len(list) != 1 || list[0].State != hostedStateReady {
		t.Fatalf("并发安装后版本树异常: %+v", list)
	}
	entries, err := os.ReadDir(hm.versionsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), hostedTmpPrefix) || strings.Contains(entry.Name(), ".old-") {
			t.Fatalf("并发安装留下半成品: %s", entry.Name())
		}
	}
}

func TestHostedTreeLockSerializesWritesAndResolve(t *testing.T) {
	hm := newTestHostedManager(t)
	hm.mu.Lock()
	writeDone := make(chan struct{})
	resolveDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_, _ = hm.remove("paddle", "1.0.0")
	}()
	go func() {
		defer close(resolveDone)
		_, _, _ = hm.resolveLatest(EnginePaddle)
	}()
	select {
	case <-writeDone:
		t.Fatal("写操作未等待托管树写锁")
	case <-resolveDone:
		t.Fatal("resolve 未等待托管树写锁")
	case <-time.After(30 * time.Millisecond):
	}
	hm.mu.Unlock()
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("释放写锁后写操作未完成")
	}
	select {
	case <-resolveDone:
	case <-time.After(time.Second):
		t.Fatal("释放写锁后 resolve 未完成")
	}
}

func TestHostedResolveWaitsForTreeWriteLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), hostedDirName)
	hm := newHostedManager(root, filepath.Join(t.TempDir(), hostedDirName))
	t.Cleanup(func() { deleteHostedTreeLock(root) })
	hm.mu.Lock()
	resolveDone := make(chan struct{})
	go func() {
		defer close(resolveDone)
		_, _, _ = hostedResolveLatest(root, EnginePaddle)
	}()
	select {
	case <-resolveDone:
		t.Fatal("解析链未等待同根托管树写锁")
	case <-time.After(30 * time.Millisecond):
	}
	hm.mu.Unlock()
	select {
	case <-resolveDone:
	case <-time.After(time.Second):
		t.Fatal("释放写锁后解析链未完成")
	}
}

func TestHostedImmutableVersionRefusalLeavesTargetIntact(t *testing.T) {
	hm := newTestHostedManager(t)
	target := filepath.Join(hm.versionsRoot, "paddle-1.0.0")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "KEEP.txt")
	if err := os.WriteFile(marker, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	// 新不可变版本契约要求既有目录带可信 meta.sha256 才允许同 hash 幂等判断；
	// 本用例要走到「解压中途失败仍保旧目录」分支，先给旧目录写入与安装包一致的摘要。

	zipPath := makeValidHostedZip(t, t.TempDir(), "paddle", "1.0.0")
	writeHostedSidecar(t, zipPath)
	zipSum, err := verifyHostedZipSHA(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHostedJSON(filepath.Join(target, "meta.json"), map[string]any{"sha256": zipSum}); err != nil {
		t.Fatal(err)
	}

	// 同版本同摘要按不可变版本契约直接幂等成功：不得再解压、不得改旧目录，
	// 即使当前解压预算被故意压到 4 字节也不应触碰安装链。
	origFile := maxHostedFileBytes
	maxHostedFileBytes = 4
	defer func() { maxHostedFileBytes = origFile }()
	if _, err := hm.installZip(zipPath); err != nil {
		t.Fatalf("同摘要重装应幂等成功且不触碰旧目录: %v", err)
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
	// 幂等安装的源包按现有行为仍留原位（没有重复归档必要）。
	if !isRegularFile(zipPath) {
		t.Fatal("拒收包应留在原位供用户排查")
	}
}

// ---------- manifest.files 逐文件自校验（后厨超集令） ----------

// zipSHA 计算字节串 sha256（十六进制小写）。
func zipSHA(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// makeFilesZip 造带 manifest.files 逐文件清单的包；mode：
// good=清单全对 / tamper=exe 摘要篡一位 / escape=清单含逃逸路径。
func makeFilesZip(t *testing.T, dir, mode string) string {
	t.Helper()
	exeBody := "MZ-exe-real"
	modelBody := "model-bytes"
	exeSum, modelSum := zipSHA(exeBody), zipSHA(modelBody)
	if mode == "tamper" {
		exeSum = zipSHA("wrong")
	}
	files := []hostedManifestEntry{
		{Path: serviceExeName, SHA256: exeSum},
		{Path: "models/model.onnx", SHA256: modelSum},
	}
	if mode == "escape" {
		files = append(files, hostedManifestEntry{Path: "../escape.txt", SHA256: zipSHA("x")})
	}
	manifest, _ := json.Marshal(hostedManifest{
		Schema: hostedManifestSchema, Engine: "paddle", Version: "1.0.0", Entry: serviceExeName,
		Files: files,
	})
	path := filepath.Join(dir, "hanxi-ocr-paddle-1.0.0.zip")
	makeHostedZip(t, path, []zipEntry{
		{name: manifestName, body: string(manifest)},
		{name: serviceExeName, body: exeBody},
		{name: "models/model.onnx", body: modelBody},
	})
	return path
}

func TestHostedFilesSelfCheck(t *testing.T) {
	// 清单一致（good）→ 安装成功
	hm := newTestHostedManager(t)
	p := makeFilesZip(t, t.TempDir(), "good")
	writeHostedSidecar(t, p)
	if _, err := hm.installZip(p); err != nil {
		t.Fatalf("一致清单应安装成功: %v", err)
	}

	// 摘要不符 → 拒收
	p2 := makeFilesZip(t, t.TempDir(), "tamper")
	writeHostedSidecar(t, p2)
	hm2 := newTestHostedManager(t)
	if _, err := hm2.installZip(p2); err == nil || !strings.Contains(err.Error(), "逐文件校验失败") {
		t.Fatalf("篡改摘要应拦下: %v", err)
	}
	if len(hm2.list()) != 0 {
		t.Fatal("拒收不得留下版本")
	}

	// 清单路径逃逸 → 独立拒收（manifest 不可信其字面）
	p3 := makeFilesZip(t, t.TempDir(), "escape")
	writeHostedSidecar(t, p3)
	hm3 := newTestHostedManager(t)
	if _, err := hm3.installZip(p3); err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("files 逃逸路径应拒绝: %v", err)
	}
}

// ---------- 后厨真包集成测（2026-09-18 双包已交付；只读引用，产物只进 TempDir） ----------

func realPackagePath(t *testing.T, env, file string) string {
	t.Helper()
	p := os.Getenv(env)
	if p == "" {
		p = filepath.Join(`E:\System\桌面\工具\hanxi-ocr-dev\dist`, file)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("真包未就位（%s），跳过集成测", p)
	}
	return p
}

func TestRealPaddlePackageFullChain(t *testing.T) {
	src := realPackagePath(t, "HANXI_OCR_PADDLE_ZIP", "hanxi-ocr-paddle-0.4.0-alpha.zip")
	// 安装链会把包移存 installers/——必须拷贝进 TempDir，绝不触碰只读成品目录
	dir := t.TempDir()
	zipPath := filepath.Join(dir, filepath.Base(src))
	if err := os.WriteFile(zipPath, mustRead(t, src), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath+".sha256", mustRead(t, src+".sha256"), 0644); err != nil {
		t.Fatal(err)
	}

	hm := newTestHostedManager(t)
	hv, err := hm.installZip(zipPath)
	if err != nil {
		t.Fatalf("真 paddle 包安装失败: %v", err)
	}
	if hv.Version != "0.4.0-alpha" || hv.State != hostedStateReady {
		t.Fatalf("回执异常: %+v", hv)
	}
	if !isRegularFile(hv.ExePath) || hv.Size < 1<<20 {
		t.Fatalf("入口异常: %+v", hv)
	}
	exe, ver, ok := hostedResolveLatest(hm.versionsRoot, EnginePaddle)
	if !ok || ver != "0.4.0-alpha" || exe != hv.ExePath {
		t.Fatalf("resolve 未命中托管入口: %s/%s/%v", exe, ver, ok)
	}
	if len(hm.list()) != 1 {
		t.Fatalf("列表应恰一项: %+v", hm.list())
	}
}

func TestRealWechatPackageValidationOnly(t *testing.T) {
	// wechat 私发件：只跑校验闸（读原路径，不解压不拷贝——49MB 不进任何落盘面）
	src := realPackagePath(t, "HANXI_OCR_WECHAT_ZIP", "hanxi-ocr-wechat-4.1.15.9.zip")
	if _, err := verifyHostedZipSHA(src); err != nil {
		t.Fatalf("真 wechat 包旁挂核对失败: %v", err)
	}
	m, _, err := inspectHostedZip(src)
	if err != nil || m.Engine != EngineWechat || m.Version != "4.1.15.9" {
		t.Fatalf("真 wechat 包契约校验失败: %+v %v", m, err)
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

// ---------- 服务面：安装 / 列表 / 卸载（F7 卡片 2） ----------

// newTestHostedService 在通用测试服务上接线独立的托管版本树。
func newTestHostedService(t *testing.T) (*OcrService, *hostedManager) {
	t.Helper()
	s := newTestService(t, "")
	base := t.TempDir()
	hm := newHostedManager(filepath.Join(base, "versions", hostedDirName), filepath.Join(base, "installers", hostedDirName))
	t.Cleanup(func() { deleteHostedTreeLock(hm.versionsRoot) })
	s.hosted = hm
	return s, s.hosted
}

func TestServiceResolveWaitsForTreeWriteLock(t *testing.T) {
	s, hm := newTestHostedService(t)
	hm.mu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = s.resolveEngineExe(EngineWechat)
	}()
	select {
	case <-done:
		t.Fatal("服务 resolve 未等待托管树写锁")
	case <-time.After(30 * time.Millisecond):
	}
	hm.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("释放写锁后服务 resolve 未完成")
	}
}

func TestInstallHostedZipServiceWechat(t *testing.T) {
	s, hm := newTestHostedService(t)
	zipPath := makeValidHostedZip(t, t.TempDir(), "wechat", "4.1.15.9")
	writeHostedSidecar(t, zipPath)

	res, err := s.InstallHostedZip(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Ok || res.Kind != "import" {
		t.Fatalf("安装回执失败: %+v", res)
	}
	exe := filepath.Join(hm.versionsRoot, "wechat-4.1.15.9", serviceExeName)
	if res.ExePath != exe {
		t.Fatalf("回执入口路径 %s", res.ExePath)
	}
	if s.store.GetEnginePath(EngineWechat) != exe {
		t.Fatalf("微信引擎应登记托管入口: %q", s.store.GetEnginePath(EngineWechat))
	}
	if v := s.store.GetEngineVersion(EngineWechat); v != "4.1.15.9" {
		t.Fatalf("版本登记 = %q", v)
	}
	// 默认活跃引擎即 wechat → 生效标记命中
	list, err := s.ListHostedVersions()
	if err != nil || len(list) != 1 || !list[0].Effective {
		t.Fatalf("列表/生效标记异常: %+v %v", list, err)
	}
}

func TestInstallHostedZipServicePaddleActivates(t *testing.T) {
	s, _ := newTestHostedService(t)
	zipPath := makeValidHostedZip(t, t.TempDir(), "paddle", "0.4.0")
	writeHostedSidecar(t, zipPath)
	res, err := s.InstallHostedZip(zipPath)
	if err != nil || !res.Ok {
		t.Fatalf("paddle 安装失败: %+v %v", res, err)
	}
	if s.store.GetActiveEngine() != EnginePaddle {
		t.Fatalf("paddle 登记应即激活, active=%q", s.store.GetActiveEngine())
	}
}

func TestInstallHostedZipRejections(t *testing.T) {
	s, hm := newTestHostedService(t)

	// 缺旁挂件
	p := makeValidHostedZip(t, t.TempDir(), "wechat", "1.0.0")
	res, err := s.InstallHostedZip(p)
	if err != nil || res.Ok || !strings.Contains(res.Message, "缺少校验文件") {
		t.Fatalf("缺旁挂应拒: %+v %v", res, err)
	}

	// manifest 契约违规（未知引擎）
	zipPath := makeBadManifestZip(t, filepath.Join(t.TempDir(), "hanxi-ocr-cuda-1.0.0.zip"))
	writeHostedSidecar(t, zipPath)
	if res, _ := s.InstallHostedZip(zipPath); res.Ok {
		t.Fatal("非法 manifest 应拒")
	}
	if n := len(hm.list()); n != 0 {
		t.Fatalf("拒收不得留下版本: %+v", hm.list())
	}
}

func makeBadManifestZip(t *testing.T, path string) string {
	t.Helper()
	manifest, _ := json.Marshal(hostedManifest{Schema: 1, Engine: "cuda", Version: "1.0.0", Entry: serviceExeName})
	makeHostedZip(t, path, []zipEntry{
		{name: manifestName, body: string(manifest)},
		{name: serviceExeName, body: "MZ"},
	})
	return path
}

func TestUninstallHostedVersionFlow(t *testing.T) {
	s, hm := newTestHostedService(t)
	zipPath := makeValidHostedZip(t, t.TempDir(), "wechat", "1.0.0")
	writeHostedSidecar(t, zipPath)
	if res, err := s.InstallHostedZip(zipPath); err != nil || !res.Ok {
		t.Fatalf("前置安装失败: %+v %v", res, err)
	}
	// 同 engine+version 且同 hash：服务面幂等成功。
	archive := filepath.Join(hm.installersRoot, "hanxi-ocr-wechat-1.0.0.zip")
	same := filepath.Join(t.TempDir(), filepath.Base(archive))
	if err := os.WriteFile(same, mustRead(t, archive), 0644); err != nil {
		t.Fatal(err)
	}
	writeHostedSidecar(t, same)
	if res, err := s.InstallHostedZip(same); err != nil || !res.Ok {
		t.Fatalf("同哈希服务重装应幂等成功: %+v %v", res, err)
	}

	// 同 engine+version 但不同 hash：服务面折进业务失败，要求新版本号。
	different := filepath.Join(t.TempDir(), filepath.Base(archive))
	makeHostedZip(t, different, hostedZipEntries("wechat", "1.0.0", "changed"))
	writeHostedSidecar(t, different)
	if res, err := s.InstallHostedZip(different); err != nil || res.Ok || !strings.Contains(res.Message, "新版本号") {
		t.Fatalf("不同哈希服务重装应拒绝: %+v %v", res, err)
	}

	out, err := s.UninstallHostedVersion("wechat", "1.0.0")
	if err != nil || out.Action != "uninstalled" {
		t.Fatalf("卸载失败: %+v %v", out, err)
	}
	if len(hm.list()) != 0 {
		t.Fatal("版本目录应删除")
	}
	if p := s.store.GetEnginePath(EngineWechat); p != "" {
		t.Fatalf("悬空登记应复位自动发现, got %q", p)
	}
	if _, err := s.UninstallHostedVersion("wechat", "1.0.0"); err == nil {
		t.Fatal("重复卸载应报错（未安装）")
	}
	if _, err := s.UninstallHostedVersion("cuda", "1.0.0"); err == nil {
		t.Fatal("未知引擎应报错")
	}
	if _, err := s.UninstallHostedVersion("wechat", `../x`); err == nil {
		t.Fatal("非法版本名应报错")
	}
}

func TestHostedExeInUsePure(t *testing.T) {
	dir := filepath.Join("versions", "hanxi-ocr", "wechat-1.0.0")
	exe := filepath.Join(dir, serviceExeName)
	if !hostedExeInUse(instance.StateRunning, exe, dir) {
		t.Fatal("running 命中目录应在用")
	}
	if !hostedExeInUse(instance.StateRunning, strings.ToUpper(exe), strings.ToLower(dir)) {
		t.Fatal("Windows 路径大小写不同仍应判定在用")
	}
	if !hostedExeInUse(instance.StateStarting, exe, filepath.Clean(dir)) {
		t.Fatal("starting 亦应在用")
	}
	if hostedExeInUse(instance.StateStopped, exe, dir) {
		t.Fatal("stopped 不在用")
	}
	if hostedExeInUse(instance.StateExternal, exe, dir) {
		t.Fatal("external 不归本引擎管")
	}
	other := filepath.Join("versions", "hanxi-ocr", "wechat-2.0.0")
	if hostedExeInUse(instance.StateRunning, exe, other) {
		t.Fatal("其他版本目录不受影响")
	}
	if hostedExeInUse(instance.StateRunning, filepath.Join("C:\\elsewhere", serviceExeName), dir) {
		t.Fatal("树外路径不在用")
	}
}

func TestHandleNativeDropZip(t *testing.T) {
	s, hm := newTestHostedService(t)
	zipPath := makeValidHostedZip(t, t.TempDir(), "paddle", "3.0.0")
	writeHostedSidecar(t, zipPath)
	s.HandleNativeDrop([]string{zipPath})
	if len(hm.list()) != 1 || s.store.GetActiveEngine() != EnginePaddle {
		t.Fatalf("拖放 .zip 应完成安装并激活: %+v active=%s", hm.list(), s.store.GetActiveEngine())
	}
}

func TestHostedDirOfExe(t *testing.T) {
	root := filepath.Join(t.TempDir(), hostedDirName)
	inside := filepath.Join(root, "paddle-1.0", serviceExeName)
	if got := hostedDirOfExe(root, inside); got != filepath.Join(root, "paddle-1.0") {
		t.Fatalf("树内登记件应回目录: %q", got)
	}
	caseRoot := strings.ToUpper(root)
	caseInside := filepath.Join(caseRoot, "PADDLE-1.0", strings.ToUpper(serviceExeName))
	if got := hostedDirOfExe(root, caseInside); !samePathFold(got, filepath.Join(root, "paddle-1.0")) {
		t.Fatalf("Windows 大小写不同仍应认作树内路径: %q", got)
	}
	prefixSibling := root + "-backup"
	if got := hostedDirOfExe(root, filepath.Join(prefixSibling, "paddle-1.0", serviceExeName)); got != "" {
		t.Fatalf("仅字符串前缀相同的兄弟目录不得认作树内: %q", got)
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
