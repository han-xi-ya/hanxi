// downloader.go 官方直链 bespoke 传输件（无官方摘要降级链的下载段，
// WindTerm/vscode 历史版同族）：带重试、ctx 感知、流式字节上限。与 GitHub
// 家族的实质差异仅"单源无镜像回退"（见 remote.go 事实注记）。
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

// downloadAttempts 单源重试次数（微软 CDN 偶发抖动靠重试消化）。
const downloadAttempts = 3

// downloadTo 下载 url → dest，onProgress 回报累计字节；maxBytes>0 时流式断言
// 上限防异常放大。每次尝试从零重写（os.Create 截断）。
func downloadTo(ctx context.Context, client *http.Client, url, dest string, maxBytes int64, onProgress func(done int64)) error {
	var lastErr error
	for attempt := 0; attempt < downloadAttempts; attempt++ {
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
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("官方源下载经 %d 次尝试均失败: %w", downloadAttempts, lastErr)
	}
	return fmt.Errorf("官方源下载失败")
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

// fileSize 返回文件字节数。
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// fileSHA256 计算文件全量 SHA-256（失败返回空串；仅诊断入账）。
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
