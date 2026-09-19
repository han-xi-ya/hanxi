package wechat

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// PNG 魔数样例：http.DetectContentType 仅嗅探前 512 字节，头部签名即可命中。
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

func TestBuildImageDataURL(t *testing.T) {
	url, err := buildImageDataURL(pngMagic)
	if err != nil {
		t.Fatalf("buildImageDataURL(png) error = %v", err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("buildImageDataURL(png) = %q, want png data URL prefix", url)
	}
	payload := strings.TrimPrefix(url, "data:image/png;base64,")
	if decoded, err := base64.StdEncoding.DecodeString(payload); err != nil || string(decoded) != string(pngMagic) {
		t.Fatalf("data URL payload does not round-trip: %v", err)
	}
}

func TestInspectOutgoingAttachmentDetectsImageContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clipboard.bin")
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}
	draft, err := inspectOutgoingAttachment(path)
	if err != nil {
		t.Fatalf("inspectOutgoingAttachment() error = %v", err)
	}
	if !draft.IsImage || draft.PreviewURL == "" || draft.FileSize != int64(len(png)) {
		t.Fatalf("inspectOutgoingAttachment() = %+v", draft)
	}
}

func TestInspectOutgoingAttachmentRejectsSpoofedImageExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.png")
	if err := os.WriteFile(path, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectOutgoingAttachment(path); err == nil {
		t.Fatal("inspectOutgoingAttachment(spoofed image) unexpectedly succeeded")
	}
}

func TestDecodeClipboardDataURLForFile(t *testing.T) {
	content := []byte("plain attachment")
	url := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(content)
	data, mime, err := decodeClipboardDataURL(url)
	if err != nil {
		t.Fatalf("decodeClipboardDataURL() error = %v", err)
	}
	if !bytes.Equal(data, content) || mime == "" {
		t.Fatalf("decodeClipboardDataURL() mime=%q data=%q", mime, data)
	}
}

func TestDecodeClipboardImageDataURL(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	data, ext, err := decodeClipboardImageDataURL(url)
	if err != nil {
		t.Fatalf("decodeClipboardImageDataURL() error = %v", err)
	}
	if ext != ".png" || !bytes.Equal(data, png) {
		t.Fatalf("decodeClipboardImageDataURL() ext=%q data=%x", ext, data)
	}
}

func TestBuildImageDataURLRejectsNonImage(t *testing.T) {
	for _, data := range [][]byte{
		{},
		[]byte("MZ\x90\x00 fake executable"),
		[]byte("<html><script>alert(1)</script></html>"),
	} {
		if _, err := buildImageDataURL(data); err == nil {
			t.Fatalf("buildImageDataURL(%q) unexpectedly succeeded", data)
		}
	}
}

func TestInboundImageFileName(t *testing.T) {
	name := inboundImageFileName(time.Date(2026, 9, 6, 12, 3, 4, 0, time.Local))
	if name != "微信图片_20260906_120304.jpg" {
		t.Fatalf("inboundImageFileName() = %q", name)
	}
}

func TestLocalImagePreview(t *testing.T) {
	dir := t.TempDir()

	// 白名单内扩展名 + 真实 PNG 魔数：正常出 Data URL
	good := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(good, pngMagic, 0o600); err != nil {
		t.Fatal(err)
	}
	url, err := localImagePreview(good)
	if err != nil {
		t.Fatalf("localImagePreview(png) error = %v", err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("localImagePreview(png) = %q", url)
	}

	// 非图片扩展名：拒绝（OpenLocalImage 同源白名单，杜绝借道打开任意文件）
	exe := filepath.Join(dir, "payload.exe")
	if err := os.WriteFile(exe, pngMagic, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := localImagePreview(exe); err == nil {
		t.Fatal("localImagePreview(exe) unexpectedly succeeded")
	}

	// 伪装图片扩展名的非图片内容：魔数嗅探兜底拒绝
	spoof := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(spoof, []byte("MZ\x90\x00 not an image at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := localImagePreview(spoof); err == nil {
		t.Fatal("localImagePreview(spoofed png) unexpectedly succeeded")
	}

	// 路径缺失：明确报错而非 panic
	if _, err := localImagePreview(filepath.Join(dir, "missing.png")); err == nil {
		t.Fatal("localImagePreview(missing) unexpectedly succeeded")
	}
}

// newWechatTestService 在临时目录装载空配置，构造可测的完整服务实例。
func newWechatTestService(t *testing.T) *WechatService {
	t.Helper()
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("settings.NewStore: %v", err)
	}
	return NewWechatService(store, extapi.NewLeaseHolder(ID))
}

func TestPreviewInboundImageRejectsMissingAttachment(t *testing.T) {
	svc := newWechatTestService(t)
	if _, err := svc.PreviewInboundImage("no-such-id"); err == nil {
		t.Fatal("PreviewInboundImage(unknown) unexpectedly succeeded")
	}
}

func TestPreviewInboundImageRejectsNonImageKind(t *testing.T) {
	svc := newWechatTestService(t)
	id, err := svc.attachments.register("acc", InboundFilePayload{
		FileName: "report.pdf",
		Media:    InboundMedia{EncryptQueryParam: "q", AESKey: "k"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 文件类附件走预览通道必须被拒（即便凭据是假的，也应先在 kind 关卡拦下，
	// 绝不能触达 CDN 下载）。
	if _, err := svc.PreviewInboundImage(id); err == nil {
		t.Fatal("file-kind attachment unexpectedly accepted by preview channel")
	}
}
