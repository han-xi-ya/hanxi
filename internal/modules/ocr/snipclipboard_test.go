package ocr

// 剪贴板识图（ocr/snip-clipboard）：不弹覆盖层直取剪贴板图像的链路分叉，
// snipper 打桩 + 在线桩引擎覆盖"无图/防重入/无组件/成功+自动复制"四态。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hanxi/internal/modules/ocr/instance"
)

// onlineProbe 恒报"端口通且是 hanxi-ocr"：refresh 后引擎进入 external 在线态，
// 让 ensureOnlineForSnip 直接放行（外部实例不越权、无需真实进程）。
type onlineProbe struct{}

func (onlineProbe) FindPIDs() []uint32       { return []uint32{4242} }
func (onlineProbe) IsRunning() bool          { return true }
func (onlineProbe) PortOpen(string) bool     { return true }
func (onlineProbe) IsOCRService(string) bool { return true }

// newOnlineTestService 挂 httptest 识别端点并强制 external 在线，返回服务与
// 可编程 fakeSnip（grabOK/grabPNG 控制"剪贴板有没有图"）。
func newOnlineTestService(t *testing.T) (*OcrService, *fakeSnip) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ocr", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"elapsed_ms":88,"text":"你好\n世界","lines":[{"text":"你好","x":1,"y":2},{"text":"世界","x":3,"y":4}]}`))
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"name":"hanxi-ocr","version":"0.4.0"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	s := newTestService(t, srv.URL)
	s.engine = instance.NewEngine(nil, onlineProbe{}, instance.Callbacks{})
	s.refresh() // stopped + 端口可探 → external
	s.snip = &fakeSnip{grabPNG: []byte("PNGDATA"), grabOK: true}
	return s, s.snip.(*fakeSnip)
}

func TestSnipClipboardNoImage(t *testing.T) {
	s := newTestService(t, "")
	s.snip = &fakeSnip{} // grabOK=false
	_, err := s.RecognizeClipboardImage()
	if err == nil || !strings.Contains(err.Error(), "没有图片") {
		t.Fatalf("无图应给中文歧义提示: %v", err)
	}
	// 顺序契约：取图失败先于服务在线检查——不白拉引擎（exeDir 空目录若先
	// ensureOnline 会报"导入"，此处必须仍报"没有图片"）
	if strings.Contains(err.Error(), "导入") {
		t.Fatal("无图时不应触达服务拉起")
	}
}

func TestSnipClipboardReentrancyGuard(t *testing.T) {
	s := newTestService(t, "")
	s.snip = &fakeSnip{}
	s.snipMu.Lock()
	defer s.snipMu.Unlock()
	if _, err := s.RecognizeClipboardImage(); err == nil || !strings.Contains(err.Error(), "进行中") {
		t.Fatalf("热键连按须被防重入拒绝: %v", err)
	}
}

func TestSnipClipboardRequiresComponent(t *testing.T) {
	// 有图但组件缺失：ensureOnlineForSnip 给导入指引（与框选链路同口径）
	s := newTestService(t, "")
	s.snip = &fakeSnip{grabPNG: []byte("X"), grabOK: true}
	_, err := s.RecognizeClipboardImage()
	if err == nil || !strings.Contains(err.Error(), "导入") {
		t.Fatalf("无组件应给导入指引: %v", err)
	}
}

// overlaySpySnip 记录覆盖层/清空触达（剪贴板识图必须两者都不碰）。
type overlaySpySnip struct {
	*fakeSnip
	overlayCalls int
	emptyCalls   int
}

func (o *overlaySpySnip) InvokeOverlay() error { o.overlayCalls++; return nil }
func (o *overlaySpySnip) Empty() error         { o.emptyCalls++; return o.fakeSnip.Empty() }

func TestSnipClipboardSuccessWithAutoCopy(t *testing.T) {
	s, inner := newOnlineTestService(t)
	fs := &overlaySpySnip{fakeSnip: inner}
	s.snip = fs
	if err := s.store.SetAutoCopy(true); err != nil {
		t.Fatal(err)
	}
	res, err := s.RecognizeClipboardImage()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Ok || res.Text != "你好\n世界" || res.LineCount != 2 || res.ElapsedMs != 88 {
		t.Fatalf("结果映射失真: %+v", res)
	}
	if !res.Copied || len(fs.written) != 1 || fs.written[0] != "你好\n世界" {
		t.Fatalf("按开关应自动复制文字: %+v %v", res, fs.written)
	}
	// 悬浮卡通道：无 Wails 实例只记结果不建窗（单测口径），GetSnipResult 可拉取
	card, found, _ := s.GetSnipResult()
	if !found || card.Text != res.Text {
		t.Fatal("成功结果应进悬浮卡通道")
	}
	// 与框选链路的语义差：不弹覆盖层、不清空剪贴板；文本仅识别结果一次写入
	if fs.overlayCalls != 0 || fs.emptyCalls != 0 {
		t.Fatalf("剪贴板识图不得触达覆盖层/清空: overlay=%d empty=%d", fs.overlayCalls, fs.emptyCalls)
	}
	if len(fs.written) != 1 {
		t.Fatalf("剪贴板仅应有一次文本写入（识别结果），实得 %v", fs.written)
	}
}

func TestSnipClipboardAutoCopyOffKeepsImage(t *testing.T) {
	s, fs := newOnlineTestService(t)
	if err := s.store.SetAutoCopy(false); err != nil {
		t.Fatal(err)
	}
	res, err := s.RecognizeClipboardImage()
	if err != nil || !res.Ok {
		t.Fatalf("识别应成功: %+v %v", res, err)
	}
	if res.Copied || len(fs.written) != 0 {
		t.Fatalf("自动复制关闭时不得写剪贴板（保留用户图像）: %v", fs.written)
	}
}

func TestSnipClipboardGrabErrorGuidesRetry(t *testing.T) {
	s := newTestService(t, "")
	s.snip = &grabErrSnip{}
	_, err := s.RecognizeClipboardImage()
	if err == nil || !strings.Contains(err.Error(), "重试") {
		t.Fatalf("持锁竞态读失败应给重按指引: %v", err)
	}
}

// grabErrSnip 读图即报错（极端持锁竞态形态）。
type grabErrSnip struct{ fakeSnip }

func (g *grabErrSnip) GrabImage() ([]byte, bool, error) {
	return nil, false, fmt.Errorf("clipboard locked")
}
