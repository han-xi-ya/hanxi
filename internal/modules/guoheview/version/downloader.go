package version

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// downloadTo 下载到目标文件。上游发布源只有官方单域（无 GitHub 镜像生态可回退），
// 故"候选 URL 列表"退化为同一 URL 多轮重试（网络抖动/半途中断均可能）。
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
		return fmt.Errorf("所有下载尝试均失败: %w", lastErr)
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

// fileMD5Hex 计算文件 md5（官方接口仅提供 MD5，完整性第一层依据）。
// 诚实说明：MD5 抗碰撞性弱，不能防蓄意伪造——但这是上游唯一官方哈希，
// 叠加 HTTPS 传输、字节数、zip entry CRC32 与解压布局自检四层共同兜底。
func fileMD5Hex(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// verifyMD5 校验文件 md5 是否与官方期望一致（大小写不敏感）。
func verifyMD5(path, want string) error {
	got := fileMD5Hex(path)
	if got == "" {
		return fmt.Errorf("无法读取下载文件")
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("md5 不匹配：期望 %s，实际 %s", want, got)
	}
	return nil
}

// fileSHA256 计算文件 sha256（仅作托管侧诊断记录，校验不使用）。
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
