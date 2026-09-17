package ocr

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/history"
	"hanxi/internal/modules/ocr/instance"
)

// ---------- 纯函数 ----------

// mkComponentDir 造一个含 exe（+可选 manifest）的组件目录。
func mkComponentDir(t *testing.T, dir string, withManifest bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hanxi-ocr.exe"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if withManifest {
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"version":"0.4.0"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveServiceExeWechat(t *testing.T) {
	base := t.TempDir()
	hanxiDir := filepath.Join(base, "hanxi")
	dataDir := filepath.Join(base, "hanxidata")
	sibling := filepath.Join(base, "hanxi-ocr")
	if err := os.MkdirAll(hanxiDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 1) 同级自动发现
	realExe := filepath.Join(sibling, "hanxi-ocr.exe")
	if err := os.MkdirAll(sibling, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realExe, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	got, fromStore, err := resolveServiceExe(hanxiDir, dataDir, EngineWechat, "")
	if err != nil || got != realExe || fromStore {
		t.Fatalf("自动发现 = %v/%v/%v, want 同级路径", got, fromStore, err)
	}

	// 2) 用户设定路径优先（存在）
	own := filepath.Join(base, "custom", "hanxi-ocr.exe")
	if err := os.MkdirAll(filepath.Dir(own), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(own, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	got, fromStore, err = resolveServiceExe(hanxiDir, dataDir, EngineWechat, own)
	if err != nil || got != own || !fromStore {
		t.Fatalf("设定路径 = %v/%v/%v", got, fromStore, err)
	}

	// 3) 设定路径失效 → 明确报错，不静默回退
	if _, _, err := resolveServiceExe(hanxiDir, dataDir, EngineWechat, filepath.Join(base, "gone.exe")); err == nil ||
		!strings.Contains(err.Error(), "失效") {
		t.Fatalf("失效路径应报「已失效」错误: %v", err)
	}
}

func TestResolveServiceExePaddle(t *testing.T) {
	base := t.TempDir()
	hanxiDir := filepath.Join(base, "hanxi")
	dataDir := filepath.Join(base, "hanxidata")
	if err := os.MkdirAll(hanxiDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 1) 登记路径优先
	regExe := filepath.Join(base, "elsewhere", "hanxi-ocr.exe")
	if err := os.MkdirAll(filepath.Dir(regExe), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(regExe, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	got, fromStore, err := resolveServiceExe(hanxiDir, dataDir, EnginePaddle, regExe)
	if err != nil || got != regExe || !fromStore {
		t.Fatalf("登记路径 = %v/%v/%v", got, fromStore, err)
	}
	// 登记路径失效 → 报错不静默回退
	if _, _, err := resolveServiceExe(hanxiDir, dataDir, EnginePaddle, filepath.Join(base, "gone.exe")); err == nil ||
		!strings.Contains(err.Error(), "失效") {
		t.Fatalf("失效登记应报错: %v", err)
	}

	// 2) ocr-engines 自动发现：只认含 manifest.json 的组件目录
	enginesRoot := filepath.Join(dataDir, "ocr-engines")
	plain := filepath.Join(enginesRoot, "plain-no-manifest")
	mkComponentDir(t, plain, false) // 无 manifest，不得命中
	comp := filepath.Join(enginesRoot, "pp-ocrv6")
	mkComponentDir(t, comp, true)
	got, fromStore, err = resolveServiceExe(hanxiDir, dataDir, EnginePaddle, "")
	want := filepath.Join(comp, "hanxi-ocr.exe")
	if err != nil || got != want || fromStore {
		t.Fatalf("ocr-engines 自动发现 = %v/%v/%v, want %s", got, fromStore, err, want)
	}

	// 3) exe 同级 ../hanxi-ocr-paddle/ 兜底（ocr-engines 为空时）
	paddleSibling := filepath.Join(base, "hanxi-ocr-paddle")
	mkComponentDir(t, paddleSibling, false) // 同级锚点不要求 manifest
	os.RemoveAll(enginesRoot)
	got, _, err = resolveServiceExe(hanxiDir, dataDir, EnginePaddle, "")
	if err != nil || got != filepath.Join(paddleSibling, "hanxi-ocr.exe") {
		t.Fatalf("同级 paddle 发现 = %v/%v, want paddle sibling", got, err)
	}

	// 4) 两个锚点皆空 → 中文报错带导入指引
	if err := os.RemoveAll(paddleSibling); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveServiceExe(hanxiDir, dataDir, EnginePaddle, ""); err == nil ||
		!strings.Contains(err.Error(), "导入") {
		t.Fatalf("未安装应给导入指引: %v", err)
	}
}

func TestResolveServiceExeUnknownEngine(t *testing.T) {
	if _, _, err := resolveServiceExe(t.TempDir(), t.TempDir(), "cuda", ""); err == nil ||
		!strings.Contains(err.Error(), "未知") {
		t.Fatalf("未知引擎应报错: %v", err)
	}
}

func TestDataURLRoundTrip(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3}
	u := buildDataURL("image/png", raw)
	data, mime, err := decodeDataURL(u)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" || string(data) != string(raw) {
		t.Fatalf("roundtrip 失败: %s %v", mime, data)
	}
	if _, _, err := decodeDataURL("http://not-data"); err == nil {
		t.Fatal("非 DataURL 应被拒")
	}
	if _, _, err := decodeDataURL("data:image/png;base64,!!!"); err == nil {
		t.Fatal("非法 base64 应被拒")
	}
}

func TestImageHelpers(t *testing.T) {
	if e, ok := imageExtForMIME("image/webp"); !ok || e != ".webp" {
		t.Fatalf("webp 映射 = %s/%v", e, ok)
	}
	if _, ok := imageExtForMIME("text/plain"); ok {
		t.Fatal("text/plain 不应是图片")
	}
	if mimeForExt(".JPG") != "image/jpeg" {
		t.Fatal("大小写扩展名映射失败")
	}
	// 消毒：路径成分剥离 + 非法字符替换 + 去扩展名
	if got := sanitizeImageName(`C:\evil\..\img:name*.png`); got == "" ||
		strings.ContainsAny(got, `:*"<>|/`) {
		t.Fatalf("sanitize 失败: %q", got)
	}
}

// ---------- 转发层（httptest 模拟上游契约） ----------

// stubProbe 空转探测器：恒报"无进程、端口不通"，让 refresh 走离线分支。
type stubProbe struct{}

func (stubProbe) FindPIDs() []uint32       { return nil }
func (stubProbe) IsRunning() bool          { return false }
func (stubProbe) PortOpen(string) bool     { return false }
func (stubProbe) IsOCRService(string) bool { return false }

func newTestService(t *testing.T, url string) *OcrService {
	t.Helper()
	port := strings.TrimPrefix(url, "http://127.0.0.1:")
	var p int
	fmt.Sscanf(port, "%d", &p)
	s := &OcrService{
		store:   newOcrStore(t.TempDir()),
		client:  &http.Client{Transport: &http.Transport{Proxy: nil}},
		exeDir:  t.TempDir(),
		dataDir: t.TempDir(),
		tmpDir:  t.TempDir(),
	}
	s.engine = instance.NewEngine(nil, stubProbe{}, instance.Callbacks{})
	if p > 0 {
		if err := s.store.SetListenPort(p); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func realImagePath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "测试图片.png")
	if err := os.WriteFile(p, []byte{0x89, 'P', 'N', 'G'}, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProbeStatusContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		// 上游真实字段命名：snake_case（开源版上报 engine=pp-ocrv6-*、engine_mode 如实透传）
		_, _ = w.Write([]byte(`{"ok":true,"name":"hanxi-ocr","version":"0.2.0","engine":"wxocr@8094","engine_mode":"dir","engine_running":true,"engine_hung":false}`))
	}))
	defer srv.Close()
	s := newTestService(t, srv.URL)

	cache := s.probeStatus(strings.TrimPrefix(srv.URL, "http://"))
	if !cache.online || cache.version != "0.2.0" || cache.engine != "wxocr@8094" ||
		cache.engineMode != "dir" || !cache.engineRunning {
		t.Fatalf("契约探测失真: %+v", cache)
	}
}

func TestContractNameSet(t *testing.T) {
	// 契约名集合（v0.4 放宽）：两版本组件同名 hanxi-ocr，集合外一律不认
	if !isContractName(serviceContractName) || !isContractName("hanxi-ocr") {
		t.Fatal("hanxi-ocr 应在契约名集合内")
	}
	if isContractName("hanxi-ocr-paddle") || isContractName("something-else") || isContractName("") {
		t.Fatal("集合外名称不得按契约命中")
	}
}

// TestServiceStateEngineFields engine / engine_mode 经 probeCache 透传到 ServiceState，
// engineID 恒为 store 登记的活跃引擎（默认 wechat；注册表切换后随动）。
func TestServiceStateEngineFields(t *testing.T) {
	srv := statusServer(t, `{"ok":true,"name":"hanxi-ocr","version":"0.4.0","engine":"pp-ocrv6-small","engine_mode":"small","engine_running":true,"engine_hung":false}`)
	s := newTestService(t, srv.URL)
	s.probeStatus(strings.TrimPrefix(srv.URL, "http://"))

	st := s.buildState(s.engine.Snapshot())
	if st.Engine != "pp-ocrv6-small" || st.EngineMode != "small" {
		t.Fatalf("engine/engine_mode 未透传: %+v", st)
	}
	if st.EngineID != EngineWechat {
		t.Fatalf("engineID = %s, want 默认 %s", st.EngineID, EngineWechat)
	}

	// store 层切换活跃引擎 → 状态模型随动（服务层 API 属 §5.4，此处只验透传链）
	paddle := mkFakeExe(t, t.TempDir(), "hanxi-ocr.exe", 1<<20)
	if err := s.store.SetEnginePath(EnginePaddle, paddle); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetActiveEngine(EnginePaddle); err != nil {
		t.Fatal(err)
	}
	if got := s.buildState(s.engine.Snapshot()).EngineID; got != EnginePaddle {
		t.Fatalf("engineID = %s, want %s", got, EnginePaddle)
	}
}

// statusServer 按固定应答起一个 /api/status 模拟上游。
func statusServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeStatusRejectsForeign(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"name":"something-else"}`)) // 端口开但非 hanxi-ocr
	}))
	defer srv.Close()
	s := newTestService(t, srv.URL)
	if cache := s.probeStatus(strings.TrimPrefix(srv.URL, "http://")); cache.online {
		t.Fatal("他程序应答不应判在线")
	}
}

func TestRecognizeImageSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ocr" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !strings.HasSuffix(req.Path, "测试图片.png") {
			t.Errorf("path 透传失真: %q", req.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"elapsed_ms":777,"text":"你好\n世界","lines":[{"text":"你好","x":618.7,"y":117.2},{"text":"世界","x":1,"y":2}]}`))
	}))
	defer srv.Close()
	s := newTestService(t, srv.URL)

	out, err := s.RecognizeImage(realImagePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Ok || out.ElapsedMs != 777 || len(out.Lines) != 2 || out.Lines[0].X != 618 {
		t.Fatalf("结果映射失真: %+v", out)
	}
}

func TestRecognizeImageFailureStates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ok":false,"error":"识别超时(30s),引擎已复位,请重试"}`))
	}))
	defer srv.Close()
	s := newTestService(t, srv.URL)

	// 业务失败：中文透传，error 通道为 nil
	out, err := s.RecognizeImage(realImagePath(t))
	if err != nil || out.Ok || !strings.Contains(out.Error, "引擎已复位") {
		t.Fatalf("业务失败透传失真: %+v %v", out, err)
	}

	// 服务不可达（已关闭的 server）
	srv.Close()
	out, err = s.RecognizeImage(realImagePath(t))
	if err != nil || out.Ok || !strings.Contains(out.Error, "无法连接") {
		t.Fatalf("不可达应给启动指引: %+v %v", out, err)
	}

	// 参数与本地文件校验
	if out, _ := s.RecognizeImage("  "); out.Ok || out.Error == "" {
		t.Fatal("空路径应拒绝")
	}
	if out, _ := s.RecognizeImage(`C:\no\such\img.png`); out.Ok || !strings.Contains(out.Error, "不存在") {
		t.Fatal("缺文件应拒绝")
	}
}

// ---------- 统一历史（defer 单点：成败同记 / Q1 档位 / 来源标记） ----------

func ocrSuccessServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ocr" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"elapsed_ms":42,"text":"你好\n世界","lines":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRecognizeImageRecordsHistory(t *testing.T) {
	s := newTestService(t, ocrSuccessServer(t).URL)
	h := history.NewStore(t.TempDir())
	s.SetHistory(h, func() bool { return true })

	img := realImagePath(t)
	if _, err := s.RecognizeImage(img); err != nil {
		t.Fatal(err)
	}
	list, err := h.List(ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("识别成功应记 1 条: %+v", list)
	}
	r := list[0]
	if r.Input != img || !strings.Contains(r.Output, "你好") || r.Extra != "ui" {
		t.Fatalf("记录字段失真: %+v", r)
	}
	// "你好\n世界" 共 5 rune（含换行）
	if !strings.Contains(r.Summary, "测试图片.png") || !strings.Contains(r.Summary, "5 字") {
		t.Fatalf("摘要应含文件名与 rune 字数: %q", r.Summary)
	}
}

func TestRecognizeHistoryFailureAndSnipSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ok":false,"error":"识别超时"}`))
	}))
	defer srv.Close()
	s := newTestService(t, srv.URL)
	h := history.NewStore(t.TempDir())
	s.SetHistory(h, nil) // nil 档位读取器 = 默认全文开

	// snip 来源 + 失败标记同记
	if _, err := s.recognizeImage(realImagePath(t), "snip"); err != nil {
		t.Fatal(err)
	}
	list, _ := h.List(ID, "")
	if len(list) != 1 || !strings.HasSuffix(list[0].Extra, "snip|fail") || !strings.Contains(list[0].Output, "识别超时") {
		t.Fatalf("失败记录失真: %+v", list)
	}
	// 空路径入参拒绝不入库（防刷桶）
	_, _ = s.RecognizeImage("   ")
	if got, _ := h.List(ID, ""); len(got) != 1 {
		t.Fatal("空路径不得入库")
	}
}

func TestRecognizeHistoryGateOffDropsText(t *testing.T) {
	s := newTestService(t, ocrSuccessServer(t).URL)
	h := history.NewStore(t.TempDir())
	s.SetHistory(h, func() bool { return false })

	if _, err := s.RecognizeImage(realImagePath(t)); err != nil {
		t.Fatal(err)
	}
	list, _ := h.List(ID, "")
	if len(list) != 1 {
		t.Fatalf("档位关闭也应记 1 条: %+v", list)
	}
	if list[0].Output != "" || !strings.Contains(list[0].Extra, "nofull") ||
		!strings.Contains(list[0].Summary, "全文未记录") {
		t.Fatalf("Q1 档位关：不得存识别文本: %+v", list[0])
	}
}

func TestHistoryWiringIsOptional(t *testing.T) {
	// 未 SetHistory（nil store）：识别照常、不 panic、不落任何文件
	s := newTestService(t, ocrSuccessServer(t).URL)
	out, err := s.RecognizeImage(realImagePath(t))
	if err != nil || !out.Ok {
		t.Fatalf("未接历史不得影响识别: %+v %v", out, err)
	}
}

func TestSavePastedImageLifecycle(t *testing.T) {
	s := &OcrService{
		store:  newOcrStore(t.TempDir()),
		client: &http.Client{},
		tmpDir: filepath.Join(t.TempDir(), "ocr"),
	}
	dataURL := buildDataURL("image/png", []byte{1, 2, 3})

	ref, err := s.SavePastedImage("截图.png", dataURL)
	if err != nil {
		t.Fatal(err)
	}
	if !ref.Temporary || !strings.HasSuffix(ref.Path, ".png") || ref.Size != 3 {
		t.Fatalf("落盘失真: %+v", ref)
	}
	first := ref.Path

	ref2, err := s.SavePastedImage("", "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString([]byte{9}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("旧粘贴件应被替换删除")
	}
	defer os.Remove(ref2.Path)

	if _, err := s.SavePastedImage("x", "data:text/plain;base64,AAA="); err == nil {
		t.Fatal("非图片 MIME 应被拒")
	}
}

// ---------- 组件导入（拖放/对话框，引用式校验） ----------

// mkFakeExe 造指定体积的假 hanxi-ocr.exe（Truncate 稀疏文件，秒建不占实际空间）。
func mkFakeExe(t *testing.T, dir, name string, size int64) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if size > 0 {
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.Close()
	return p
}

func TestImportServiceExeValidation(t *testing.T) {
	s := newTestService(t, "")

	cases := []struct {
		name string
		path string
		want string // 期望中文提示关键词
	}{
		{"空路径", "  ", "未收到文件路径"},
		{"改名件", filepath.Join(t.TempDir(), "我改名了.exe"), "hanxi-ocr.exe"},
		{"不存在", filepath.Join(t.TempDir(), "hanxi-ocr.exe"), "不存在"},
	}
	for _, c := range cases {
		res, err := s.ImportServiceExe(c.path)
		if err != nil {
			t.Fatalf("%s: 业务失败不应走 error 通道: %v", c.name, err)
		}
		if res.Ok || !strings.Contains(res.Message, c.want) {
			t.Fatalf("%s: 应被拒且提示含 %q，实得 %+v", c.name, c.want, res)
		}
		if s.store.GetExePath() != "" {
			t.Fatalf("%s: 失败导入不得改动设定", c.name)
		}
	}
}

func TestImportServiceExeRejectsOversizedJunkWithoutEngine(t *testing.T) {
	// 目录版启动器体积（<35MB）且同级无 wcocr.dll → 拒收并指引要单文件版
	dir := t.TempDir()
	p := mkFakeExe(t, dir, "hanxi-ocr.exe", 9<<20)
	s := newTestService(t, "")
	res, err := s.ImportServiceExe(p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Ok || !strings.Contains(res.Message, "单文件版") {
		t.Fatalf("孤零小 exe 应被拒并提示单文件版，实得 %+v", res)
	}
}

func TestImportServiceExeAcceptsBothForms(t *testing.T) {
	singleDir := t.TempDir()
	single := mkFakeExe(t, singleDir, "hanxi-ocr.exe", 36<<20) // 稀疏假单文件版
	s := newTestService(t, "")
	res, err := s.ImportServiceExe(single)
	if err != nil || !res.Ok {
		t.Fatalf("单文件版应导入成功: %+v %v", res, err)
	}
	if got := s.store.GetExePath(); got != single {
		t.Fatalf("设定应指向导入件: %s", got)
	}
	if res.ExePath != single || res.Kind != "import" {
		t.Fatalf("回执字段异常: %+v", res)
	}

	// 目录版：小 exe 但同级有 wcocr.dll → 同样可导入
	folderDir := t.TempDir()
	fake := mkFakeExe(t, folderDir, "hanxi-ocr.exe", 9<<20)
	if err := os.WriteFile(filepath.Join(folderDir, "wcocr.dll"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	res2, err := s.ImportServiceExe(fake)
	if err != nil || !res2.Ok {
		t.Fatalf("目录版(带引擎同级)应导入成功: %+v %v", res2, err)
	}
}

// ---------- 框选截屏识别（snipper 打桩） ----------

// fakeSnip 可编程剪贴板/截屏桩。
type fakeSnip struct {
	text     string
	hasText  bool
	emptyErr error
	grabPNG  []byte
	grabOK   bool
	written  []string
	cursorXY [2]int
}

func (f *fakeSnip) SnapshotText() (string, bool) { return f.text, f.hasText }
func (f *fakeSnip) Empty() error                 { return f.emptyErr }
func (f *fakeSnip) WriteText(s string) error     { f.written = append(f.written, s); return nil }
func (f *fakeSnip) GrabImage() ([]byte, bool, error) {
	return f.grabPNG, f.grabOK, nil
}
func (f *fakeSnip) InvokeOverlay() error         { return nil }
func (f *fakeSnip) CursorPos() (int, int, error) { return f.cursorXY[0], f.cursorXY[1], nil }

func TestSnipReentrancyGuard(t *testing.T) {
	s := newTestService(t, "")
	s.snip = &fakeSnip{}
	s.snipMu.Lock() // 模拟进行中
	defer s.snipMu.Unlock()
	if _, err := s.SnipAndRecognize(); err == nil || !strings.Contains(err.Error(), "进行中") {
		t.Fatalf("并发触发应被防重入拒绝: %v", err)
	}
}

func TestSnipRequiresComponent(t *testing.T) {
	// stopped + 自动发现失败（exeDir 空目录无同级组件）→ 中文导入指引
	s := newTestService(t, "")
	s.snip = &fakeSnip{}
	if _, err := s.SnipAndRecognize(); err == nil || !strings.Contains(err.Error(), "导入") {
		t.Fatalf("无组件应给导入指引: %v", err)
	}
}

func TestSnipCopyTextAndDismiss(t *testing.T) {
	s := newTestService(t, "")
	fs := &fakeSnip{}
	s.snip = fs
	// 无结果时拒绝
	if err := s.SnipCopyText(); err == nil {
		t.Fatal("空结果不应可复制")
	}
	s.lastSnip = SnipResult{Ok: true, Text: "你好"}
	if err := s.SnipCopyText(); err != nil {
		t.Fatal(err)
	}
	if len(fs.written) != 1 || fs.written[0] != "你好" {
		t.Fatalf("复制代理未落到剪贴板写入: %v", fs.written)
	}
}

func TestShowSnipCardWithoutApp(t *testing.T) {
	// 无 Wails 实例（单测环境）：只记结果不建窗，不 panic
	s := newTestService(t, "")
	s.snip = &fakeSnip{}
	s.showSnipCard(SnipResult{Ok: true, Text: "x"})
	res := s.cardResult()
	if !res.Ok || res.Text != "x" {
		t.Fatal("结果应被记录供 GetSnipResult 拉取")
	}
	if _, found := s.GetSnipResult(); !found {
		t.Fatal("有结果时 found 应为 true")
	}
}

func TestHandleNativeDropRouting(t *testing.T) {
	s := newTestService(t, "")

	// .exe 落放 → 导入分支（以 store 副作用观测；无 Wails 实例时事件静默丢弃）
	single := mkFakeExe(t, t.TempDir(), "hanxi-ocr.exe", 36<<20)
	s.HandleNativeDrop([]string{single})
	if s.store.GetExePath() != single {
		t.Fatal("exe 落放应走导入分支并更新设定")
	}

	// 图片落放 → 选图分支，不得动组件设定
	s.HandleNativeDrop([]string{realImagePath(t)})
	if s.store.GetExePath() != single {
		t.Fatal("图片落放不应改动组件设定")
	}

	// 杂项与空列表：无害
	s.HandleNativeDrop([]string{mkFakeExe(t, t.TempDir(), "readme.txt", 10)})
	s.HandleNativeDrop(nil)
}

// ---------- 双引擎：导入分流 / 引擎列表 / 切换（计划 §5.3 / §5.4） ----------

// liveProbe 可配置端口/契约探针桩（external 态构造用；对照固定全 false 的 stubProbe）。
type liveProbe struct{ port, serving bool }

func (liveProbe) FindPIDs() []uint32          { return nil }
func (liveProbe) IsRunning() bool             { return false }
func (p *liveProbe) PortOpen(string) bool     { return p.port }
func (p *liveProbe) IsOCRService(string) bool { return p.serving }

// makeExternal 把服务引擎换成在位探针并刷新，构造"外部实例在服务"态。
func makeExternal(t *testing.T, s *OcrService) {
	t.Helper()
	s.engine = instance.NewEngine(nil, &liveProbe{port: true, serving: true}, instance.Callbacks{})
	s.engine.RefreshExternal(s.addr())
	if got := s.engine.Snapshot().State; got != instance.StateExternal {
		t.Fatalf("构造 external 失败: %s", got)
	}
}

// mkPaddleDir 造合法 PP-OCR 引擎目录（exe + manifest，version 写进 manifest）。
func mkPaddleDir(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hanxi-ocr.exe"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"version":%q,"engine":"pp-ocrv6-small"}`, version)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestImportPaddleDirRegistersAndActivates(t *testing.T) {
	s := newTestService(t, "")
	dir := mkPaddleDir(t, "0.4.0")

	res, err := s.ImportPaddleDir(dir)
	if err != nil || !res.Ok {
		t.Fatalf("合法引擎目录应登记成功: %+v %v", res, err)
	}
	exe := filepath.Join(dir, "hanxi-ocr.exe")
	if res.Kind != "import" || res.ExePath != exe {
		t.Fatalf("回执字段失真: %+v", res)
	}
	if got := s.store.GetEnginePath(EnginePaddle); got != exe {
		t.Fatalf("paddle 登记 = %q", got)
	}
	if got := s.store.GetEngineVersion(EnginePaddle); got != "0.4.0" {
		t.Fatalf("version 应读自 manifest: %q", got)
	}
	if got := s.store.GetActiveEngine(); got != EnginePaddle {
		t.Fatalf("导入即激活，active = %s", got)
	}
}

func TestImportPaddleDirRejectsInvalidLayout(t *testing.T) {
	s := newTestService(t, "")

	missingExe := t.TempDir()
	if err := os.WriteFile(filepath.Join(missingExe, "manifest.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	missingManifest := t.TempDir()
	if err := os.WriteFile(filepath.Join(missingManifest, "hanxi-ocr.exe"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, path, want string }{
		{"空路径", "  ", "未收到目录路径"},
		{"目录不存在", filepath.Join(t.TempDir(), "no-such-dir"), "引擎目录不存在"},
		{"文件当目录", missingExe + `\hanxi-ocr.exe`, "引擎目录不存在"},
		{"缺 exe", missingExe, "hanxi-ocr.exe"},
		{"缺 manifest（疑似微信件目录）", missingManifest, "manifest.json"},
	}
	for _, c := range cases {
		res, err := s.ImportPaddleDir(c.path)
		if err != nil {
			t.Fatalf("%s: 业务失败不应走 error 通道: %v", c.name, err)
		}
		if res.Ok || !strings.Contains(res.Message, c.want) {
			t.Fatalf("%s: 应被拒且提示含 %q，实得 %+v", c.name, c.want, res)
		}
		if s.store.GetEnginePath(EnginePaddle) != "" || s.store.GetActiveEngine() != EngineWechat {
			t.Fatalf("%s: 失败导入不得改动注册表", c.name)
		}
	}
}

func TestImportServiceExeRoutesDirectoryToPaddle(t *testing.T) {
	s := newTestService(t, "")
	dir := mkPaddleDir(t, "0.4.0")
	// 统一导入入口按形态分流：目录 → PP-OCR 引擎包登记并激活
	res, err := s.ImportServiceExe(dir)
	if err != nil || !res.Ok {
		t.Fatalf("目录应分流到 paddle 导入: %+v %v", res, err)
	}
	if s.store.GetActiveEngine() != EnginePaddle {
		t.Fatal("目录分流后应切 paddle")
	}

	// 原生拖放目录同样进导入分支（而非「无法识别的拖放文件类型」）
	s2 := newTestService(t, "")
	s2.HandleNativeDrop([]string{mkPaddleDir(t, "0.4.0")})
	if s2.store.GetActiveEngine() != EnginePaddle {
		t.Fatal("HandleNativeDrop 目录落放应走引擎包导入")
	}
}

func TestGetEngines(t *testing.T) {
	srv := statusServer(t, `{"ok":true,"name":"hanxi-ocr","version":"0.4.1","engine":"pp-ocrv6-small","engine_mode":"small","engine_running":true,"engine_hung":false}`)
	s := newTestService(t, srv.URL)

	// 双引擎均未安装：两行枚举、wechat 活跃、中文原因
	list, err := s.GetEngines()
	if err != nil || len(list) != 2 {
		t.Fatalf("应枚举两引擎: %+v %v", list, err)
	}
	if list[0].ID != EngineWechat || !list[0].Active || list[0].Installed || list[0].Error == "" {
		t.Fatalf("wechat 初始态失真: %+v", list[0])
	}
	if list[0].Label != "微信引擎" || list[1].Label != "PP-OCR 开源引擎" {
		t.Fatalf("Label 应为中文: %+v %+v", list[0], list[1])
	}
	if list[1].ID != EnginePaddle || list[1].Active {
		t.Fatalf("paddle 初始态失真: %+v", list[1])
	}

	// 导入微信件（不切活跃）+ 导入 paddle（登记即激活）+ 外部在线实例
	single := mkFakeExe(t, t.TempDir(), "hanxi-ocr.exe", 36<<20)
	if _, err := s.ImportServiceExe(single); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportPaddleDir(mkPaddleDir(t, "0.4.0")); err != nil {
		t.Fatal(err)
	}
	makeExternal(t, s)
	s.probeStatus(strings.TrimPrefix(srv.URL, "http://"))

	list, _ = s.GetEngines()
	wechat, paddle := list[0], list[1]
	if !wechat.Installed || wechat.Path != single || wechat.Auto || wechat.Active {
		t.Fatalf("导入后 wechat 失真: %+v", wechat)
	}
	if !paddle.Installed || !paddle.Active || paddle.Auto ||
		!strings.HasSuffix(paddle.Path, "hanxi-ocr.exe") || paddle.Path == wechat.Path {
		t.Fatalf("导入后 paddle 失真: %+v", paddle)
	}
	// 活跃引擎在线：实测版本（0.4.1）覆盖 manifest 登记版本（0.4.0）
	if paddle.Version != "0.4.1" {
		t.Fatalf("paddle version = %q, want 在线实测 0.4.1", paddle.Version)
	}
	if wechat.Version != "" {
		t.Fatalf("非活跃引擎不得被在线版本污染: %q", wechat.Version)
	}
}

func TestSetActiveEngineGuards(t *testing.T) {
	s := newTestService(t, "")

	if _, err := s.SetActiveEngine("cuda"); err == nil || !strings.Contains(err.Error(), "未知") {
		t.Fatalf("未知 ID 应中文拒绝: %v", err)
	}
	// 同引擎幂等（大小写/空白归一）
	out, err := s.SetActiveEngine(" WECHAT ")
	if err != nil || out.Action != "already-active" {
		t.Fatalf("切到同引擎应幂等: %+v %v", out, err)
	}
	// 目标未安装：中文报错带导入指引，注册表不动
	if _, err := s.SetActiveEngine(EnginePaddle); err == nil ||
		!strings.Contains(err.Error(), "无法切换") || !strings.Contains(err.Error(), "导入") {
		t.Fatalf("未安装应拒绝并给指引: %v", err)
	}
	if s.store.GetActiveEngine() != EngineWechat {
		t.Fatal("被拒切换不得改注册表")
	}
}

func TestSetActiveEngineIdleSwitchDoesNotStart(t *testing.T) {
	s := newTestService(t, "")
	out, err := s.ImportPaddleDir(mkPaddleDir(t, "0.4.0"))
	if err != nil || !out.Ok {
		t.Fatalf("导入应成功: %+v %v", out, err)
	}
	// stopped 态切换只动注册表，不擅自拉起进程（stubProbe 下引擎从未成功 Start）
	if st := s.engine.Snapshot().State; st != instance.StateStopped {
		t.Fatalf("stopped 态切换不得拉起: %s", st)
	}
	// 切回未安装的微信件 → 拒绝在写注册表之前，active 保持 paddle
	if _, err := s.SetActiveEngine(EngineWechat); err == nil ||
		!strings.Contains(err.Error(), "未找到 hanxi-ocr.exe") {
		t.Fatalf("切回未安装件应被拒: %v", err)
	}
	if s.store.GetActiveEngine() != EnginePaddle {
		t.Fatal("被拒切换不得翻转 active")
	}
}

func TestSetActiveEngineExternalRefuses(t *testing.T) {
	s := newTestService(t, "")
	single := mkFakeExe(t, t.TempDir(), "hanxi-ocr.exe", 36<<20)
	if _, err := s.ImportServiceExe(single); err != nil {
		t.Fatal(err) // 微信件先装好（不切活跃）
	}
	if _, err := s.ImportPaddleDir(mkPaddleDir(t, "0.4.0")); err != nil {
		t.Fatal(err) // 当前 active=paddle
	}
	makeExternal(t, s)

	out, err := s.SetActiveEngine(EngineWechat) // 目标已安装，但外部实例占位
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != "external-unmanaged" || !out.External || !strings.Contains(out.Message, "外部") {
		t.Fatalf("external 应拒绝切换: %+v", out)
	}
	if s.store.GetActiveEngine() != EnginePaddle {
		t.Fatal("external 拒绝时不得改注册表")
	}
}

// TestImportPaddleDirExternalKeepsRegistration 外部实例在服务时导入：
// 登记成功但不激活（不越权），回执 Ok=false 说明原委。
func TestImportPaddleDirExternalKeepsRegistration(t *testing.T) {
	s := newTestService(t, "")
	makeExternal(t, s)
	res, err := s.ImportPaddleDir(mkPaddleDir(t, "0.4.0"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Ok || !strings.Contains(res.Message, "外部") {
		t.Fatalf("external 占位时导入应登记不激活: %+v", res)
	}
	if s.store.GetEnginePath(EnginePaddle) == "" {
		t.Fatal("登记动作本身应生效")
	}
	if s.store.GetActiveEngine() != EngineWechat {
		t.Fatal("external 拒绝下 active 不得翻转")
	}
}

func TestProbeStatusRegistersVersion(t *testing.T) {
	srv := statusServer(t, `{"ok":true,"name":"hanxi-ocr","version":"0.4.1","engine":"pp-ocrv6-small","engine_mode":"small","engine_running":true,"engine_hung":false}`)
	s := newTestService(t, srv.URL)
	addr := strings.TrimPrefix(srv.URL, "http://")

	// 未在线（state=stopped，探测虽应答但不属托管/外部在位）：不回写注册表
	s.probeStatus(addr)
	if v := s.store.GetEngineVersion(EngineWechat); v != "" {
		t.Fatalf("非在位状态不得登记版本: %q", v)
	}

	// external 在位：版本回写 active 引擎（当前默认 wechat）
	makeExternal(t, s)
	s.probeStatus(addr)
	if v := s.store.GetEngineVersion(EngineWechat); v != "0.4.1" {
		t.Fatalf("在线版本应回写注册表: %q", v)
	}

	// 服务下线：探测失败不清版本（登记值是「最近已知」而非「当前事实」）
	srv.Close()
	s.engine = instance.NewEngine(nil, &liveProbe{port: false, serving: false}, instance.Callbacks{})
	s.probeStatus(addr)
	if v := s.store.GetEngineVersion(EngineWechat); v != "0.4.1" {
		t.Fatalf("离线不得清空登记版本: %q", v)
	}
}

// TestSnipUsesActiveEngine 截屏自动拉起改走 active 引擎解析结果（计划 §5.4）：
// paddle 登记件存在（微信件缺失）时，ensureOnlineForSnip 应越过"请先导入"指引、
// 瞄准 paddle 登记件进入进程启动环节（假件非 PE，报错应带该 exe 路径）。
func TestSnipUsesActiveEngine(t *testing.T) {
	s := newTestService(t, "")
	dir := mkPaddleDir(t, "0.4.0")
	if _, err := s.ImportPaddleDir(dir); err != nil {
		t.Fatal(err)
	}
	s.snip = &fakeSnip{}
	err := s.ensureOnlineForSnip()
	if err == nil {
		t.Fatal("假件启动应失败")
	}
	// 关键：不再是"未找到组件/请先导入"，且失败信息指向 paddle 登记件
	if strings.Contains(err.Error(), "导入") || !strings.Contains(err.Error(), dir) {
		t.Fatalf("应针对活跃引擎组件发起拉起，实得: %v", err)
	}
}
