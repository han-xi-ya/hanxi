package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// 上游二进制仅官方域名（update.code.visualstudio.com → vscode CDN）分发，
// 无 GitHub 镜像生态可回退——下载仅保留重试（微软 CDN 偶发 DNS/TLS 抖动，
// everything 官网下载同族先例），失败如实报错，不引入第三方镜像。
const downloadAttempts = 3

// downloadTo 带重试地下载单个 URL 到目标文件，onProgress 回报累计字节。
// ctx 取消在重试间隙与请求/读流内即时响应（P0 批 2b）。
func downloadTo(ctx context.Context, client *http.Client, url, dest string, onProgress func(done int64)) error {
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
		if err := tryDownloadSingle(ctx, client, url, dest, onProgress); err != nil {
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

func tryDownloadSingle(ctx context.Context, client *http.Client, url, dest string, onProgress func(done int64)) error {
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

// fileSHA256 计算文件 sha256（失败返回空串）。
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

// verifySHA256 校验文件 sha256 是否与期望一致（大小写不敏感）。
func verifySHA256(path, want string) error {
	got := fileSHA256(path)
	if got == "" {
		return fmt.Errorf("无法读取下载文件")
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 不匹配：期望 %s，实际 %s", want, got)
	}
	return nil
}

// verifyMZ 校验文件以 MZ 魔数开头（exe 最小可信形态，安装器无官方哈希时的兜底）。
func verifyMZ(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil {
		return fmt.Errorf("读取文件头失败: %w", err)
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return fmt.Errorf("文件头非 MZ 魔数，疑似下载损坏")
	}
	return nil
}
