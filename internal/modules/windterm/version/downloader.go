// downloader.go 无官方摘要降级链的传输件（vscode 历史版先例，ADR-0002 §5
// 信任根分治）：直连 + GitHub 加速镜像逐个回退，每址带重试与流式字节上限，
// 落盘后由 manager 做字节数核对 + zip CRC 闸门 + 布局自检（降级三层）。
package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// perURLAttempts 单镜像重试次数（GitHub 分发域抖动多为瞬时，2 次足够；
// 跨镜像回退本身即第二层重试）。
const perURLAttempts = 2

// downloadWithMirrors 依次尝试候选 URL（首个为主址）下载到 dest。
// 每个 URL 内部重试 perURLAttempts 次；换址/换次从零重写（os.Create 截断），
// 进度回调如实回报新起点。maxBytes>0 时流式断言上限，杜绝异常放大。
// ctx 取消在重试间隙、请求与读流内即时响应（P0 批 2b 生命周期）。
func downloadWithMirrors(ctx context.Context, client *http.Client, urls []string, dest string, maxBytes int64, onProgress func(done int64)) error {
	var lastErr error
	for _, url := range urls {
		for attempt := 0; attempt < perURLAttempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if attempt > 0 {
				select {
				case <-time.After(time.Duration(attempt) * time.Second):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if err := tryDownload(ctx, client, url, dest, maxBytes, onProgress); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				lastErr = fmt.Errorf("%s: %w", url, err)
				continue
			}
			return nil
		}
	}
	if lastErr != nil {
		return fmt.Errorf("全部 %d 个下载源经重试均失败: %w", len(urls), lastErr)
	}
	return fmt.Errorf("无可用下载源")
}

func tryDownload(ctx context.Context, client *http.Client, url, dest string, maxBytes int64, onProgress func(done int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
			done += int64(n)
			if maxBytes > 0 && done > maxBytes {
				return fmt.Errorf("下载超出期望体积 %d 字节，连接已中止", maxBytes)
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
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
	return out.Sync()
}

// githubMirrors 构造主址与加速镜像候选列表（首个为主址；路径模板对任意
// owner/repo/tag/asset 泛化，与 markeron 共用同一组镜像前缀）。
func githubMirrors(tag, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// fileSize 返回文件字节数。
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// fileSHA256 计算文件 sha256（失败返回空串；仅诊断入账，不参与降级链校验）。
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
