package wsl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/modules/wsl/netx"
	"hanxi/internal/product"
)

// EventMsiDownload 下载进度事件名（app.go 注册载荷类型）。
const EventMsiDownload = "wsl:msi-download"

// msiMaxSize WSL 官方 MSI 为几十 MB 量级，1GB 上限是防挂死的粗护栏。
const msiMaxSize = 1 << 30

// DownloadProgress MSI 下载进度事件载荷（与托管模块 version-download 事件同构）。
type DownloadProgress struct {
	Tag      string `json:"tag"`
	Platform string `json:"platform"` // x64 | ARM64
	Stage    string `json:"stage"`    // downloading | done | error
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
	Path     string `json:"path,omitempty"`  // done 时落盘绝对路径
	Error    string `json:"error,omitempty"` // error 时失败原因
}

// DownloadMsi 把官方 MSI 下到系统"下载"文件夹（应用内进度、绝不甩给浏览器）。
// 单飞：同一时刻仅允许一路下载；tag/文件名走 assetDownloadURL 双重白名单。
func (s *WslService) DownloadMsi(tag, name string) (OperationOutcome, error) {
	rawURL, err := assetDownloadURL(tag, name)
	if err != nil {
		return OperationOutcome{}, err
	}
	s.mu.Lock()
	if s.dlBusy {
		s.mu.Unlock()
		return OperationOutcome{}, fmt.Errorf("已有下载正在进行，请等待完成或先取消浏览器外的其它任务")
	}
	s.dlBusy = true
	s.mu.Unlock()

	key := tag + "/" + name
	dir := downloadDir()
	opCtx, opCancel := context.WithCancel(context.Background())
	if !s.registerDownload(opCancel) {
		return OperationOutcome{}, fmt.Errorf("已有下载正在进行，请等待完成或取消")
	}
	go func() {
		defer opCancel()
		defer s.finishDownload()
		defer func() {
			s.mu.Lock()
			s.dlBusy = false
			s.mu.Unlock()
		}()
		path, derr := s.downloadTo(opCtx, tag, name, dir, rawURL)
		if derr != nil {
			msg := derr.Error()
			if canceled(derr) {
				msg = "下载已按请求取消（临时 .part 文件已清理）"
			}
			s.emit(EventMsiDownload, DownloadProgress{Tag: tag, Platform: assetPlatform(name), Stage: "error", Error: msg})
			return
		}
		s.mu.Lock()
		s.dlPaths[key] = path
		s.mu.Unlock()
		s.emit(EventMsiDownload, DownloadProgress{Tag: tag, Platform: assetPlatform(name), Stage: "done", Path: path})
	}()
	return OperationOutcome{Success: true, Message: fmt.Sprintf("开始下载 %s（保存到 %s）", name, dir)}, nil
}

// RevealDownload 在资源管理器中定位已下载的 MSI；只认本会话自己登记过的路径，
// 不接受前端传入任意路径（envcheck RevealToolPath 同款红线）。
func (s *WslService) RevealDownload(tag, name string) error {
	key := strings.TrimSpace(strings.TrimPrefix(tag, "v")) + "/" + strings.TrimSpace(name)
	s.mu.Lock()
	path := s.dlPaths[key]
	s.mu.Unlock()
	if path == "" {
		return fmt.Errorf("该文件尚未由 Hanxi 下载完成")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("文件已不在原位: %w", err)
	}
	return revealInExplorer(path)
}

func (s *WslService) downloadTo(parent context.Context, tag, name, dir, rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()

	client := netx.NewClient(30*time.Minute, func(req *http.Request, via []*http.Request) error {
		host := strings.ToLower(req.URL.Hostname())
		if req.URL.Scheme != "https" || (host != "github.com" && host != "objects.githubusercontent.com" && host != "release-assets.githubusercontent.com") {
			return fmt.Errorf("拒绝对非官方主机继续下载: %s", req.URL.Redacted())
		}
		if len(via) >= 10 {
			return fmt.Errorf("重定向次数过多")
		}
		return nil
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", product.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载请求失败（GitHub 直连被拦时可换网络/代理后重试）: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > msiMaxSize {
		return "", fmt.Errorf("文件超过 %d 字节护栏", msiMaxSize)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建下载目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, name+".*.part")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // rename 成功后此为 no-op

	var written int64
	lastEmit := time.Now()
	buf := make([]byte, 256*1024)
	emit := func(stage string, done, total int64) {
		s.emit(EventMsiDownload, DownloadProgress{Tag: tag, Platform: assetPlatform(name), Stage: stage, Done: done, Total: total})
	}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				tmp.Close()
				return "", fmt.Errorf("写盘失败: %w", werr)
			}
			written += int64(n)
			if written > msiMaxSize {
				tmp.Close()
				return "", fmt.Errorf("下载超出 %d 字节护栏", msiMaxSize)
			}
			if time.Since(lastEmit) >= 300*time.Millisecond {
				emit("downloading", written, resp.ContentLength)
				lastEmit = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			tmp.Close()
			return "", fmt.Errorf("下载中断: %w", rerr)
		}
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("关闭临时文件失败: %w", err)
	}
	final := uniquePath(dir, name)
	if err := os.Rename(tmpName, final); err != nil {
		return "", fmt.Errorf("落盘改名失败: %w", err)
	}
	return final, nil
}

// downloadDir 系统下载文件夹；极端环境缺 Home 时回落临时目录。
func downloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "hanxi-wsl")
	}
	return filepath.Join(home, "Downloads")
}

// uniquePath 同名文件不覆盖：name.msi → name (1).msi → (2)…
func uniquePath(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		candidate := filepath.Join(dir, name)
		if i > 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func assetPlatform(name string) string {
	if strings.Contains(strings.ToLower(name), "arm64") {
		return "ARM64"
	}
	return "x64"
}

// revealInExplorer 包级函数变量：单测可替换，避免测试中真实唤起 explorer.exe。
var revealInExplorer = func(path string) error {
	// /select, 与路径必须同参数（markeron 事故教训：裸路径语义是"执行"）。
	return exec.Command("explorer.exe", "/select,"+path).Start()
}
