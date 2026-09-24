package softver

// 下载安装包面单测：目录解析/下载执行器/reveal 全部桩注入，离线断言不碰网络；
// 纯函数（命名清洗、避让落位、端点审校、MZ 断言）直打。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func officialWithLink() *OfficialRelease {
	return &OfficialRelease{
		Version:     "4.1.15",
		DownloadURL: "https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe",
	}
}

// newDownloadService 构造带下载桩的服务：downloadsDir 指临时目录，
// 事件灌入 channel，revealFile 记录路径。
func newDownloadService(t *testing.T) (*SoftverService, string, chan InstallerProgress) {
	t.Helper()
	dir := t.TempDir()
	events := make(chan InstallerProgress, 64)
	s := newTestService(func() (localData, error) { return localData{}, nil })
	s.downloadsDir = func() (string, error) { return dir, nil }
	s.revealFile = func(p string) error { return nil }
	s.emit = func(_ string, p any) { events <- p.(InstallerProgress) }
	return s, dir, events
}

func waitInstallerEvent(t *testing.T, ch chan InstallerProgress, state string) InstallerProgress {
	t.Helper()
	for {
		select {
		case ev := <-ch:
			if ev.State == state {
				return ev
			}
			if ev.State == "error" && state != "error" {
				t.Fatalf("等待 %s 时收到 error: %s", state, ev.Message)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("等待下载事件 %s 超时", state)
			return InstallerProgress{}
		}
	}
}

func TestInstallerFileName(t *testing.T) {
	cases := []struct {
		url, version, want string
	}{
		{"https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe", "4.1.15", "WeChatWin_4.1.15.exe"},
		{"https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.1.15.exe?msclkid=secret", "4.1.15", "WeChatWin_4.1.15.exe"},
		// 路径基名不是 exe（改版/异形态）：回落版本命名，绝不把 URL 片段当路径。
		{"https://dldir1v6.qq.com/weixin/Universal/Windows/get", "4.1.15", "WeChatWin_4.1.15.exe"},
		{"not a url", "4.2.0", "WeChatWin_4.2.0.exe"},
		{"https://dldir1v6.qq.com/../..%2Fevil.zip", "", "WeChatWin_unknown.exe"},
	}
	for _, c := range cases {
		if got := installerFileName(c.url, c.version); got != c.want {
			t.Errorf("installerFileName(%q,%q) = %q, 期望 %q", c.url, c.version, got, c.want)
		}
	}
	if got := installerFileName(`https://x.com/..`, "9"); got != "WeChatWin_9.exe" {
		t.Errorf("逃逸基名应回落: %q", got)
	}
}

func TestUniqueDestPathNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	name := "WeChatWin_4.1.15.exe"
	p1, err := uniqueDestPath(dir, name)
	if err != nil || filepath.Base(p1) != name {
		t.Fatalf("空目录首个落位名异常: %q %v", p1, err)
	}
	if err := os.WriteFile(p1, []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	p2, err := uniqueDestPath(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p2) != "WeChatWin_4.1.15 (1).exe" {
		t.Errorf("同名应退避让位, got %q", filepath.Base(p2))
	}
}

func TestValidateInstallerURL(t *testing.T) {
	if _, err := validateInstallerURL("https://dldir1v6.qq.com/a/WeChatWin_4.1.15.exe"); err != nil {
		t.Errorf("合法 https 直链应通过: %v", err)
	}
	for _, bad := range []string{"http://dldir1v6.qq.com/a.exe", "ftp://x/y.exe", "https:///no-host"} {
		if _, err := validateInstallerURL(bad); err == nil {
			t.Errorf("%q 应被拒（只允许 HTTPS 且带主机名）", bad)
		}
	}
}

func TestVerifyMZHead(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.exe")
	bad := filepath.Join(dir, "bad.exe")
	empty := filepath.Join(dir, "empty.exe")
	os.WriteFile(ok, []byte("MZ\x90\x00payload"), 0644)
	os.WriteFile(bad, []byte("<html>404</html>"), 0644)
	os.WriteFile(empty, nil, 0644)
	if err := verifyMZHead(ok); err != nil {
		t.Errorf("MZ 头应通过: %v", err)
	}
	if err := verifyMZHead(bad); err == nil || !strings.Contains(err.Error(), "MZ") {
		t.Errorf("HTML 伪装应被拒: %v", err)
	}
	if err := verifyMZHead(empty); err == nil {
		t.Error("空文件应被拒")
	}
	if err := verifyMZHead(filepath.Join(dir, "nosuch")); err == nil {
		t.Error("文件不存在应报错")
	}
}

func TestDownloadInstallerFileRejectsInsecureAndEmpty(t *testing.T) {
	// 明文 http 在碰网前即拒（错误早、可诊断）。
	if _, err := downloadInstallerFile(context.Background(), "http://dldir1v6.qq.com/a.exe", filepath.Join(t.TempDir(), "a.exe"), nil); err == nil {
		t.Fatal("http 直链应被拒")
	} else if !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("错误应讲清协议口径: %v", err)
	}
}

func TestStartInstallerDownloadGuards(t *testing.T) {
	s, _, _ := newDownloadService(t)
	// 未取官方读数：拒绝并指路。
	if err := s.StartInstallerDownload(); err == nil || !strings.Contains(err.Error(), "获取官方最新版") {
		t.Errorf("无官方缓存应指路: %v", err)
	}
	// 页面改版只出版本号无直链：同样拒。
	s.official = &OfficialRelease{Version: "4.1.15"}
	if err := s.StartInstallerDownload(); err == nil {
		t.Error("无直链不应凭空下载")
	}
	// 目录解析失败：如实透传中文报错。
	s.official = officialWithLink()
	s.downloadsDir = func() (string, error) { return "", errors.New("无法解析系统下载目录") }
	if err := s.StartInstallerDownload(); err == nil || !strings.Contains(err.Error(), "无法解析") {
		t.Errorf("目录错误应透传: %v", err)
	}
	// 桩缺失（构造方忘记注入平台默认）：拒而不 panic。
	s.downloadsDir = nil
	if err := s.StartInstallerDownload(); err == nil {
		t.Error("能力缺失应如实报错")
	}
	// 官方缓存读取竞态护栏：登记后 goroutine 不再回读可变缓存。
	s, dir, events := newDownloadService(t)
	s.official = officialWithLink()
	block := make(chan struct{})
	release := make(chan struct{})
	s.downloadFile = func(ctx context.Context, url, dest string, onBytes func(int64, int64)) (int64, error) {
		block <- struct{}{}
		<-release
		return 0, ctx.Err()
	}
	if err := s.StartInstallerDownload(); err != nil {
		t.Fatal(err)
	}
	<-block
	if err := s.StartInstallerDownload(); err == nil || !strings.Contains(err.Error(), "进行中") {
		t.Errorf("并发第二个下载应被拒: %v", err)
	}
	snap, err := s.Snapshot()
	if err != nil || !snap.Downloading || snap.Downloaded != nil {
		t.Errorf("进行中快照应 downloading=true 且无成品: %+v %v", snap, err)
	}
	if err := s.CancelInstallerDownload(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if ev := waitInstallerEvent(t, events, "canceled"); !strings.Contains(ev.Message, "清理") {
		t.Errorf("取消应如实说明临时件处置: %+v", ev)
	}
	if _, err := os.Stat(filepath.Join(dir, "WeChatWin_4.1.15.exe")); !os.IsNotExist(err) {
		t.Error("取消后不应留下成品文件")
	}
	if err := s.CancelInstallerDownload(); err == nil {
		t.Error("无进行中下载的取消应报错")
	}
}

func TestInstallerDownloadDoneAndReveal(t *testing.T) {
	s, _, events := newDownloadService(t)
	s.official = officialWithLink()
	var revealed []string
	s.revealFile = func(p string) error { revealed = append(revealed, p); return nil }
	s.downloadFile = func(ctx context.Context, url, dest string, onBytes func(int64, int64)) (int64, error) {
		if !strings.HasPrefix(url, "https://dldir1v6.qq.com/") {
			t.Errorf("应下载官方直链, got %q", url)
		}
		if onBytes != nil {
			onBytes(1024, 2048)
		}
		return 2048, os.WriteFile(dest, []byte("MZfake"), 0644)
	}
	if err := s.StartInstallerDownload(); err != nil {
		t.Fatal(err)
	}
	ev := waitInstallerEvent(t, events, "done")
	if ev.File == nil || ev.File.Version != "4.1.15" || ev.File.Bytes != 2048 {
		t.Fatalf("done 事件缺成品记录: %+v", ev)
	}
	if !strings.Contains(ev.Message, "未提供校验值") {
		t.Errorf("完成消息必须如实标注校验口径: %q", ev.Message)
	}
	snap, _ := s.Snapshot()
	if snap.Downloaded == nil || snap.Downloading {
		t.Errorf("快照应挂成品记录且不再进行中: %+v", snap)
	}
	if err := s.RevealInstallerFile(); err != nil || len(revealed) != 1 || revealed[0] != ev.File.Path {
		t.Errorf("RevealInstallerFile = %v revealed=%v", err, revealed)
	}
	// 文件被用户移走后：如实报错，不谎报打开成功。
	os.Remove(ev.File.Path)
	if err := s.RevealInstallerFile(); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("成品丢失应报错: %v", err)
	}
}

func TestInstallerDownloadFailureNoFalseSuccess(t *testing.T) {
	s, _, events := newDownloadService(t)
	s.official = officialWithLink()
	s.downloadFile = func(ctx context.Context, _, _ string, _ func(int64, int64)) (int64, error) {
		return 0, errors.New("传输中断（已收 4096 字节）: connection reset")
	}
	if err := s.StartInstallerDownload(); err != nil {
		t.Fatal(err)
	}
	ev := waitInstallerEvent(t, events, "error")
	if !strings.Contains(ev.Message, "传输中断") {
		t.Errorf("失败原因应原样上抛: %+v", ev)
	}
	snap, _ := s.Snapshot()
	if snap.Downloaded != nil || snap.Downloading {
		t.Errorf("失败后快照不得有成品/进行中: %+v", snap)
	}
	if err := s.RevealInstallerFile(); err == nil {
		t.Error("从未成功下载时打开位置应报错")
	}
}

func TestCancelActiveDownloadLifecycle(t *testing.T) {
	s, _, events := newDownloadService(t)
	s.official = officialWithLink()
	block := make(chan struct{})
	s.downloadFile = func(ctx context.Context, _, _ string, _ func(int64, int64)) (int64, error) {
		close(block)
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if err := s.StartInstallerDownload(); err != nil {
		t.Fatal(err)
	}
	<-block
	s.cancelActiveDownload()
	waitInstallerEvent(t, events, "canceled")
	snap, _ := s.Snapshot()
	if snap.Downloading {
		t.Error("模块退出取消后不应仍显示进行中")
	}
}
