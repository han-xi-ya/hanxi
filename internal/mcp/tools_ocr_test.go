package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/client"

	"hanxi/internal/modules/ocr"
)

type fakeRecognizer struct {
	outcome ocr.OcrOutcome
	err     error
	lastP   string
	calls   int
}

func (f *fakeRecognizer) RecognizeImage(path string) (ocr.OcrOutcome, error) {
	f.lastP, f.calls = path, f.calls+1
	return f.outcome, f.err
}

// TestOCRToolValidation 参数闸门：相对路径拒、不存在拒、超限拒、全过才达后端。
func TestOCRToolValidation(t *testing.T) {
	fr := &fakeRecognizer{outcome: ocr.OcrOutcome{Ok: true, Text: "hello"}}
	c := ocrClient(t, fr)

	// 相对路径
	res, text := callText(t, c, toolOCR, map[string]any{"path": "shots\\a.png"})
	if !res.IsError || !strings.Contains(text, "绝对路径") {
		t.Fatalf("relative path must be rejected: %s", text)
	}
	if fr.calls != 0 {
		t.Errorf("rejected input must not reach backend")
	}

	// 不存在
	res, text = callText(t, c, toolOCR, map[string]any{"path": t.TempDir() + "\\nope.png"})
	if !res.IsError || !strings.Contains(text, "不存在") {
		t.Fatalf("missing file must be rejected: %s", text)
	}

	// 真实存在的小文件 → 放行到后端
	good := t.TempDir() + "\\ok.png"
	if err := os.WriteFile(good, []byte("fake png"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, text = callText(t, c, toolOCR, map[string]any{"path": good})
	if res.IsError {
		t.Fatalf("valid path rejected: %s", text)
	}
	if fr.lastP != good {
		t.Errorf("backend got %q, want %q", fr.lastP, good)
	}
}

// TestOCRSizeGate 超 64MB 图片在 MCP 侧先行拒绝（注入 stat 假件免造大文件）。
func TestOCRSizeGate(t *testing.T) {
	orig := statFileFn
	statFileFn = func(string) (os.FileInfo, error) { return fakeBigFileInfo{}, nil }
	t.Cleanup(func() { statFileFn = orig })

	fr := &fakeRecognizer{}
	c := ocrClient(t, fr)
	res, text := callText(t, c, toolOCR, map[string]any{"path": `C:\anywhere.png`})
	if !res.IsError || !strings.Contains(text, "过大") {
		t.Fatalf("oversize must be rejected: %s", text)
	}
	if fr.calls != 0 {
		t.Error("oversize must not reach service")
	}
}

type fakeBigFileInfo struct{}

func (fakeBigFileInfo) Name() string      { return "big.png" }
func (fakeBigFileInfo) Size() int64       { return maxImageFileBytes + 1 }
func (fakeBigFileInfo) Mode() os.FileMode { return 0o644 }
func (fakeBigFileInfo) ModTime() time.Time {
	return time.Time{}
}
func (fakeBigFileInfo) IsDir() bool { return false }
func (fakeBigFileInfo) Sys() any    { return nil }

// TestOCROfflineGuidance 服务离线（业务错误折进 outcome）：工具报错给"先启动服务"
// 指引文案而非程序性崩溃（C4 验收：无组件/离线环境返回指引）。
func TestOCROfflineGuidance(t *testing.T) {
	fr := &fakeRecognizer{outcome: ocr.OcrOutcome{
		Error: "无法连接 hanxi-ocr 服务（127.0.0.1:53120），请先启动服务",
	}}
	c := ocrClient(t, fr)
	res, text := callText(t, c, toolOCR, map[string]any{"path": anyFile(t)})
	if !res.IsError || !strings.Contains(text, "请先启动") {
		t.Fatalf("offline must return guidance, got: %s", text)
	}
}

// TestOCROutputRedactedAndTruncated 识别文本进云端前的两道处理：Redact 口径 + 预算截断。
func TestOCROutputRedactedAndTruncated(t *testing.T) {
	long := strings.Repeat("字", 700*1024) // >1MB UTF-8
	fr := &fakeRecognizer{outcome: ocr.OcrOutcome{
		Ok: true, Text: long + "\nAPI token=SKTOPSECRET123", ElapsedMs: 42,
	}}
	c := ocrClient(t, fr)
	res, text := callText(t, c, toolOCR, map[string]any{"path": anyFile(t)})
	if res.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if strings.Contains(text, "SKTOPSECRET123") {
		t.Errorf("secret must be scrubbed from model-visible OCR text")
	}
	var payload struct {
		Ok        bool   `json:"ok"`
		Text      string `json:"text"`
		ElapsedMs int64  `json:"elapsedMs"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if !payload.Truncated || payload.ElapsedMs != 42 {
		t.Errorf("oversize text must set truncated, keep metadata: %+v", payload)
	}
	if !utf8OK(payload.Text) {
		t.Error("truncated text must not cut in the middle of a rune")
	}

	// 短文本也要过 Redact（不依赖截断顺带消灭敏感串）
	fr2 := &fakeRecognizer{outcome: ocr.OcrOutcome{Ok: true, Text: "配置行 token=ABCDEFGHIJKLMNOP"}}
	c2 := ocrClient(t, fr2)
	_, text2 := callText(t, c2, toolOCR, map[string]any{"path": anyFile(t)})
	if strings.Contains(text2, "ABCDEFGHIJKLMNOP") || !strings.Contains(text2, "******") {
		t.Errorf("short-text secret must be scrubbed via Redact: %s", text2)
	}
}

// TestOCRDescriptionHonesty description 如实声明"不代为启动服务"。
func TestOCRDescriptionHonesty(t *testing.T) {
	tool, _ := buildOcrTool(Deps{})
	for _, phrase := range []string{"不代为启动", "绝对路径"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description must contain %q, got: %s", phrase, tool.Description)
		}
	}
}

// TestOCRServiceErrorChannel service 返回程序性 error（非业务折返）同样报错不 panic。
func TestOCRServiceErrorChannel(t *testing.T) {
	fr := &fakeRecognizer{err: errors.New("boom")}
	c := ocrClient(t, fr)
	res, text := callText(t, c, toolOCR, map[string]any{"path": anyFile(t)})
	if !res.IsError || !strings.Contains(text, "boom") {
		t.Fatalf("service error must surface: %s", text)
	}
}

// helpers

func anyFile(t *testing.T) string {
	t.Helper()
	p := t.TempDir() + "/img.png"
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func ocrClient(t *testing.T, fr *fakeRecognizer) *client.Client {
	t.Helper()
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"ocr": true})
	deps.OCR = fr
	return inProcClient(t, deps)
}

func utf8OK(s string) bool { return utf8.ValidString(s) }
