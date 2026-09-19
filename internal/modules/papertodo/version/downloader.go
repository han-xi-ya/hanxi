package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// 说明：官方摘要在场路径的下载主流程（多源回退、流式 SHA-256、字节双核、
// 原子临时件）已收口至内核 artifact.Fetch；本文件保留 PaperTodo 领域辅助——
// 镜像 URL 模板，以及上游无 digest 场的降级传输链（manager.Download 第 3 步
// else 分支，完整性由 manager 的"字节数 + MZ + PE 核对"链条收口）。

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/ccswitch/frpc 同一组镜像前缀）。
func assetMirrors(tag, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// downloadTo 依次尝试候选 URL 下载到目标文件，支持重试与镜像故障转移（降级链传输）。
// 默认 http.Client 自动跟随 https 重定向（github.com → CDN），
// tryDownloadSingle 中显式拒绝的 3xx 仅在自定义 CheckRedirect 干预时出现，属防御分支。
func downloadTo(client *http.Client, urls []string, dest string, onProgress func(done int64)) error {
	const maxRetries = 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		for _, u := range urls {
			err := tryDownloadSingle(client, u, dest, onProgress)
			if err == nil {
				return nil
			}
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("所有下载源与重试均失败: %w", lastErr)
	}
	return fmt.Errorf("所有下载源均失败")
}

func tryDownloadSingle(client *http.Client, url, dest string, onProgress func(done int64)) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return fmt.Errorf("unexpected redirect to %s", resp.Header.Get("Location"))
	case resp.StatusCode >= 400:
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	buf := make([]byte, 64*1024)
	var done int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if onProgress != nil {
				onProgress(done)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

// fileSize 返回文件字节数。
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// fileSHA256 计算文件 sha256。作为"下载指纹"写入 hanxi-meta.json
// （篡改/盘上损坏的审计基线，见 manager 完整性链注释；官方摘要在场时
// 硬校验已由内核 Fetch 流式 + 落盘双核收口，本值与官方摘要同值入账）。
func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// checkMZMagic PE 魔数哨兵：下载物必须以 MZ 开头，否则根本不是可执行文件
// （HTML 错误页/代理投毒的最廉价第一道闸，替代 zip 场景的 CRC32 层）。
func checkMZMagic(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf("读取文件头失败: %w", err)
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return fmt.Errorf("下载物不是 Windows 可执行文件（MZ 头缺失，疑似代理返回了错误页）")
	}
	return nil
}
