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

	"hanxi/internal/modules/ocr/instance"
)

// ---------- 纯函数 ----------

func TestResolveServiceExe(t *testing.T) {
	base := t.TempDir()
	hanxiDir := filepath.Join(base, "hanxi")
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
	got, fromStore, err := resolveServiceExe(hanxiDir, "")
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
	got, fromStore, err = resolveServiceExe(hanxiDir, own)
	if err != nil || got != own || !fromStore {
		t.Fatalf("设定路径 = %v/%v/%v", got, fromStore, err)
	}

	// 3) 设定路径失效 → 明确报错，不静默回退
	if _, _, err := resolveServiceExe(hanxiDir, filepath.Join(base, "gone.exe")); err == nil ||
		!strings.Contains(err.Error(), "失效") {
		t.Fatalf("失效路径应报「已失效」错误: %v", err)
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

func (stubProbe) FindPIDs() []uint32         { return nil }
func (stubProbe) IsRunning() bool            { return false }
func (stubProbe) PortOpen(string) bool       { return false }
func (stubProbe) IsOCRService(string) bool   { return false }

func newTestService(t *testing.T, url string) *OcrService {
	t.Helper()
	port := strings.TrimPrefix(url, "http://127.0.0.1:")
	var p int
	fmt.Sscanf(port, "%d", &p)
	s := &OcrService{
		store:  newOcrStore(t.TempDir()),
		client: &http.Client{Transport: &http.Transport{Proxy: nil}},
		exeDir: t.TempDir(),
		tmpDir: t.TempDir(),
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
		// 上游真实字段命名：snake_case
		_, _ = w.Write([]byte(`{"ok":true,"name":"hanxi-ocr","version":"0.2.0","engine":"wxocr@8094","engine_running":true,"engine_hung":false}`))
	}))
	defer srv.Close()
	s := newTestService(t, srv.URL)

	cache := s.probeStatus(strings.TrimPrefix(srv.URL, "http://"))
	if !cache.online || cache.version != "0.2.0" || cache.engine != "wxocr@8094" || !cache.engineRunning {
		t.Fatalf("契约探测失真: %+v", cache)
	}
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
