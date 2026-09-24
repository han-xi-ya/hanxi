package softver

// 官方安装包直连下载（N38，明确不托管口径）：把官方更新页解析出的
// dldir1 系直链取回本地下载目录，"拿完包走人"——不入库、不静默安装、
// 不接管启动（douzy「仅版本+下载」型同款边界，安装由用户双击完成）。
//
// 完整性口径（如实降级，对齐 WindTerm 三层兜底里的可用层）：
// 微信官方从不旁挂 SHA-256 摘要，artifact.Fetch 的"官方摘要必检"闸在此
// 先天无源可喂——因此只核验两件事并全程如实标注：
//  1. Content-Length 声明与实收字节双核（防截断/代理篡改）；
//  2. 落盘 MZ 文件头断言（防 CDN 错误页伪装 exe）。
//
// 网络纪律：走 netx 代理链（与浏览器同出口，踩坑 #35）；只允许 https；
// 重定向不跨 scheme、跳数受限；失败/取消即删临时件，不留半成品。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hanxi/packages/go/netx"
)

const (
	// installerDownloadTimeout 整包下载超时预算：微信安装包约 200MB 量级，
	// 慢链路留足 15 分钟（client 本身不设 Timeout，预算挂在 ctx 上，取消语义优先）。
	installerDownloadTimeout = 15 * time.Minute
	// installerMaxBytes 流式大小上限：官方包远小于此，超限即异常放大/挂错资源，拒收。
	installerMaxBytes = int64(1) << 30 // 1 GiB
	// installerMaxRedirects 重定向跳数上限。
	installerMaxRedirects = 10
	// downloadProgressInterval 运行中进度的推送节流（终态必发，不限流）。
	downloadProgressInterval = 250 * time.Millisecond
)

// installerDownloader 是下载执行器的注入形状（service 字段，单测替换后不碰网络）。
// 返回落盘实收字节数。
type installerDownloader func(ctx context.Context, url, destPath string, onBytes func(done, total int64)) (int64, error)

// installerDownloadTransport 共享传输层：外网走 netx 代理链，回环直连
// （artifact.Fetch 同构的混合闸；本下载实际只会打官方 CDN）。
var installerDownloadTransport = &http.Transport{
	Proxy:               netx.LoopbackAwareProxyFunc(),
	DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout: 15 * time.Second,
	MaxIdleConnsPerHost: 2,
}

// downloadInstallerFile 把 url 流式下载到 destPath（先写同目录临时件，
// 字节双核 + MZ 断言通过后原子 rename 落位）。取消/失败即删临时件。
func downloadInstallerFile(ctx context.Context, rawURL, destPath string, onBytes func(done, total int64)) (int64, error) {
	u, err := validateInstallerURL(rawURL)
	if err != nil {
		return 0, err
	}
	opCtx, cancel := context.WithTimeout(ctx, installerDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(opCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("构造下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", browserUA)

	client := &http.Client{Transport: installerDownloadTransport, CheckRedirect: installerCheckRedirect}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("请求下载直链失败: %w", unwrapSoftverURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("下载直链返回异常状态: %s", resp.Status)
	}

	var total int64
	if resp.ContentLength > 0 {
		total = resp.ContentLength
		if total > installerMaxBytes {
			return 0, fmt.Errorf("服务器声明大小 %d 字节超过上限 %d 字节，拒收", total, installerMaxBytes)
		}
	}

	partPath := destPath + ".part-" + randomHexLower(6)
	out, err := os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("创建临时下载文件失败: %w", err)
	}
	var done int64
	copyOK := false
	defer func() {
		if !copyOK {
			out.Close()
			_ = os.Remove(partPath) // 失败/取消收尸临时件，不留半截 exe
		}
	}()

	buf := make([]byte, 64*1024)
	var lastReport time.Time
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if done+int64(n) > installerMaxBytes {
				return 0, fmt.Errorf("实收字节超过上限 %d（异常放大防护）", installerMaxBytes)
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				return 0, fmt.Errorf("写入临时文件失败: %w", werr)
			}
			done += int64(n)
			if onBytes != nil && (done == int64(n) || time.Since(lastReport) >= downloadProgressInterval) {
				lastReport = time.Now()
				onBytes(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctxErr := opCtx.Err(); ctxErr != nil {
				if errors.Is(ctxErr, context.DeadlineExceeded) {
					return 0, fmt.Errorf("下载超时（预算 %s，已收 %d 字节）", installerDownloadTimeout, done)
				}
				return 0, fmt.Errorf("下载已取消: %w", ctxErr)
			}
			return 0, fmt.Errorf("传输中断（已收 %d 字节）: %w", done, readErr)
		}
	}
	if total > 0 && done != total {
		return 0, fmt.Errorf("下载不完整：声明 %d 字节，实际 %d 字节", total, done)
	}
	if err := out.Sync(); err != nil {
		return 0, fmt.Errorf("落盘同步失败: %w", err)
	}
	if err := out.Close(); err != nil {
		return 0, fmt.Errorf("关闭临时文件失败: %w", err)
	}
	copyOK = true
	if err := verifyMZHead(partPath); err != nil {
		_ = os.Remove(partPath)
		return 0, err
	}
	// 原子落位：Windows 同卷 rename 替换语义（目标名由 uniqueDestPath 保证新空）。
	if err := os.Rename(partPath, destPath); err != nil {
		_ = os.Remove(partPath)
		return 0, fmt.Errorf("下载文件落位失败: %w", err)
	}
	return done, nil
}

// validateInstallerURL 下载端点审校：只允许 https 且必须带主机名
// （官方直链恒为 https://dldir1*.qq.com/…，明文 http 一律拒）。
func validateInstallerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("下载直链无效: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return nil, fmt.Errorf("不支持的下载协议 %q（安装包只允许 HTTPS 直链）", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("下载直链缺少主机名")
	}
	return u, nil
}

// installerCheckRedirect 重定向闸门：跳数受限、不跨 scheme、逐跳复检。
func installerCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > installerMaxRedirects {
		return fmt.Errorf("重定向超过 %d 跳上限", installerMaxRedirects)
	}
	if !strings.EqualFold(via[0].URL.Scheme, req.URL.Scheme) {
		return fmt.Errorf("拒绝跨协议重定向 %s -> %s（降级/逃逸防护）", via[0].URL.Scheme, req.URL.Scheme)
	}
	if _, err := validateInstallerURL(req.URL.String()); err != nil {
		return fmt.Errorf("重定向目标被拒: %w", err)
	}
	return nil
}

// verifyMZHead MZ 文件头断言：落地文件"确实是可执行体而非 HTML 错误页/
// 截断残片"的最低声明（douzy/rustdesk 便携下载同谱口径）。
func verifyMZHead(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("无法打开下载文件: %w", err)
	}
	defer f.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil {
		return fmt.Errorf("读取文件头失败（疑似空文件）: %w", err)
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return errors.New("下载文件不是有效的 Windows 可执行体（MZ 头缺失，疑似官方 CDN 返回了错误页）")
	}
	return nil
}

// installerFileName 从直链取安装包基名：剥 query、只留基名、Windows 非法
// 字符清洗；取不到可信名字回落带版本的保守命名（绝不把 URL 片段当路径）。
func installerFileName(rawURL, version string) string {
	base := ""
	if u, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		base = filepath.Base(filepath.FromSlash(u.Path))
	}
	base = sanitizeInstallerName(base)
	if base == "" || !strings.EqualFold(filepath.Ext(base), ".exe") {
		v := sanitizeInstallerName(version)
		if v == "" {
			v = "unknown"
		}
		return "WeChatWin_" + v + ".exe"
	}
	return base
}

// sanitizeInstallerName 清洗为可安全用于文件名的基名：拒路径分隔与点号逃逸，
// 只留字母数字与 . _ -，截断到 128 字符。
func sanitizeInstallerName(raw string) string {
	raw = strings.ReplaceAll(raw, "\\", "/")
	if i := strings.LastIndexByte(raw, '/'); i >= 0 {
		raw = raw[i+1:]
	}
	var b strings.Builder
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 128 {
		ext := filepath.Ext(out)
		if len(ext) > 8 {
			ext = ""
		}
		out = out[:128-len(ext)] + ext
	}
	if out == "" || out == "." || out == ".." {
		return ""
	}
	return out
}

// uniqueDestPath 在下载目录内为 name 选不覆盖既有文件的落位名：
// 同名（Windows 口径不区分大小写）已存在时退避 "Name (1).ext" 递增，
// 连续避让 50 个仍冲突即拒（目录里堆出这个量级说明用户另有用途，停手报信）。
func uniqueDestPath(dir, name string) (string, error) {
	candidate := filepath.Join(dir, name)
	if !pathTaken(candidate) {
		return candidate, nil
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	ext := filepath.Ext(name)
	for i := 1; i <= 50; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if !pathTaken(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("下载目录中 %q 同名文件过多（已避让 50 次），请清理后重试", name)
}

// pathTaken 判断路径是否已被占用（文件或目录，含软链占名；Windows 大小写不敏感
// 由 stat 天然覆盖——本机卷均为大小写不敏感，如实按 stat 结果判）。
func pathTaken(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// randomHexLower n 字节随机十六进制串（临时件后缀，同 artifact 纪律）。
func randomHexLower(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return hex.EncodeToString(b)
}

// unwrapSoftverURLError 把 *url.Error 的噪声剥掉，只留原始错误（artifact 同款）。
func unwrapSoftverURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
