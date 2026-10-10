package wechat

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// openTestAESKey 与 download_test.go 同口径的 16 字节明文密钥（parseInboundAESKey
// 对 base64(16B) 直接放行 AES-128）。
var openTestAESKey = []byte("0123456789abcdef")

// newOpenInboundTestService 构造把 CDN 指向本地假源站的服务：所有下载都返回同一份
// 加密后的明文；downloads 记录真实下载次数（幂等复用的直接判据）。
func newOpenInboundTestService(t *testing.T, plaintext []byte) (*WechatService, *atomic.Int32) {
	t.Helper()
	ciphertext, err := aesEcbEncrypt(plaintext, openTestAESKey)
	if err != nil {
		t.Fatal(err)
	}

	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		w.Write(ciphertext)
	}))
	t.Cleanup(server.Close)

	svc := newWechatTestService(t)
	svc.defaultClient.cdnBase = server.URL
	svc.defaultClient.httpClient = server.Client()
	return svc, &downloads
}

// stubOpenFile 注入"打开"接缝：真实现会经 rundll32 真的拉起查看器窗口，测试必须换替身。
func stubOpenFile(t *testing.T, openErr error) *[]string {
	t.Helper()
	var paths []string
	prev := openFileHandler
	openFileHandler = func(path string) error {
		paths = append(paths, path)
		return openErr
	}
	t.Cleanup(func() { openFileHandler = prev })
	return &paths
}

func registerOpenAttachment(t *testing.T, svc *WechatService, fileName string) string {
	t.Helper()
	id, err := svc.attachments.register("acc", InboundFilePayload{
		FileName: fileName,
		Media: InboundMedia{
			EncryptQueryParam: "query",
			AESKey:            base64.StdEncoding.EncodeToString(openTestAESKey),
			EncryptType:       1,
		},
	})
	if err != nil {
		t.Fatalf("register(%q): %v", fileName, err)
	}
	return id
}

func TestOpenInboundFileReusesManagedCopy(t *testing.T) {
	svc, downloads := newOpenInboundTestService(t, []byte("chat file plaintext"))
	paths := stubOpenFile(t, nil)
	id := registerOpenAttachment(t, svc, "report.txt")

	var first string
	for i := 0; i < 3; i++ {
		res, err := svc.OpenInboundFile(id)
		if err != nil {
			t.Fatalf("OpenInboundFile #%d: %v", i, err)
		}
		if i == 0 {
			first = res.Path
		} else if res.Path != first {
			t.Fatalf("重复打开返回不同路径: %q vs %q", res.Path, first)
		}
	}
	if got := downloads.Load(); got != 1 {
		t.Fatalf("downloads = %d, want 1（幂等复用不得重复下载）", got)
	}
	if len(*paths) != 3 {
		t.Fatalf("打开调用 %d 次, want 3", len(*paths))
	}
	if data, err := os.ReadFile(first); err != nil || string(data) != "chat file plaintext" {
		t.Fatalf("受管明文副本内容异常: %v %q", err, data)
	}

	svc.attachments.mu.RLock()
	dir, openedCount := svc.attachments.openDir, len(svc.attachments.opened)
	svc.attachments.mu.RUnlock()
	if filepath.Dir(first) != dir {
		t.Fatalf("明文副本未收口进受管目录: %q vs %q", first, dir)
	}
	if openedCount != 1 {
		t.Fatalf("opened = %d, want 1", openedCount)
	}

	// 服务销毁整目录回收：修复点的最硬验收——不留任何散 Temp。
	svc.destroy()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("destroy 后受管目录未回收: %v", err)
	}
}

func TestOpenInboundFileReDownloadsWhenCopyVanishedExternally(t *testing.T) {
	svc, downloads := newOpenInboundTestService(t, []byte("hello wechat"))
	stubOpenFile(t, nil)
	id := registerOpenAttachment(t, svc, "note.txt")

	first, err := svc.OpenInboundFile(id)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟系统 Temp 清理/用户手动删除：登记还在、文件已不在盘。
	if err := os.Remove(first.Path); err != nil {
		t.Fatal(err)
	}

	second, err := svc.OpenInboundFile(id)
	if err != nil {
		t.Fatalf("副本消失后重新打开失败: %v", err)
	}
	if second.Path != first.Path {
		t.Fatalf("路径漂移: %q vs %q", second.Path, first.Path)
	}
	if got := downloads.Load(); got != 2 {
		t.Fatalf("downloads = %d, want 2（副本不在盘必须回到下载路径补齐）", got)
	}
	if data, err := os.ReadFile(second.Path); err != nil || string(data) != "hello wechat" {
		t.Fatalf("重建副本内容异常: %v %q", err, data)
	}
	svc.destroy()
}

func TestOpenInboundFileOpenedCopiesBounded(t *testing.T) {
	svc, _ := newOpenInboundTestService(t, []byte("x"))
	stubOpenFile(t, nil)

	const total = maxOpenedFiles + 4
	paths := make([]string, 0, total)
	for i := 0; i < total; i++ {
		id := registerOpenAttachment(t, svc, fmt.Sprintf("f%02d.bin", i))
		res, err := svc.OpenInboundFile(id)
		if err != nil {
			t.Fatalf("OpenInboundFile #%d: %v", i, err)
		}
		paths = append(paths, res.Path)
	}

	svc.attachments.mu.RLock()
	dir := svc.attachments.openDir
	openedCount := len(svc.attachments.opened)
	svc.attachments.mu.RUnlock()
	if openedCount > maxOpenedFiles {
		t.Fatalf("受管明文副本登记 %d 条, 超上限 %d", openedCount, maxOpenedFiles)
	}

	survivors := 0
	for _, p := range paths {
		if filepath.Dir(p) != dir {
			t.Fatalf("明文副本 %q 散落在受管目录 %q 之外", p, dir)
		}
		if _, err := os.Stat(p); err == nil {
			survivors++
		}
	}
	if survivors != maxOpenedFiles {
		t.Fatalf("在盘副本 %d 份，want %d（连续打开 N 次必须有界，最旧副本随淘汰删除）", survivors, maxOpenedFiles)
	}

	svc.destroy()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("destroy 后受管目录未整目录回收: %v", err)
	}
}

func TestOpenInboundFileFailedOpenKeepsRealPathAndRecoverable(t *testing.T) {
	svc, _ := newOpenInboundTestService(t, []byte("secret chat"))
	paths := stubOpenFile(t, errors.New("no associated program available"))
	id := registerOpenAttachment(t, svc, "note.txt")

	res, err := svc.OpenInboundFile(id)
	if err == nil || !strings.Contains(err.Error(), "打开失败") {
		t.Fatalf("err = %v, want 打开失败", err)
	}
	if res.Path == "" || !strings.Contains(err.Error(), res.Path) {
		t.Fatalf("错误文本应引用真实路径: path=%q err=%v", res.Path, err)
	}
	if len(*paths) != 1 {
		t.Fatalf("打开调用 %d 次, want 1", len(*paths))
	}
	// 回执语义如实："文件已下载到 <path>"里的 path 此刻必须真的在盘。
	if _, statErr := os.Stat(res.Path); statErr != nil {
		t.Fatalf("回执宣称已下载的路径不存在: %v", statErr)
	}

	// 打开失败路径同样可回收：明文副本已受管，随注册项被账号删除一并清走。
	svc.attachments.deleteAccount("acc")
	if _, statErr := os.Stat(res.Path); !os.IsNotExist(statErr) {
		t.Fatal("打开失败残留的明文未随 deleteAccount 回收")
	}
	svc.destroy()
}
