package version

import (
	"crypto/sha1" // #nosec G505 -- Snipaste 官网发布的独立校验清单当前使用 SHA-1。
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func downloadTo(client *http.Client, rawURL, dest string, onProgress func(done, total int64)) (int64, error) {
	if !isOfficialAssetURL(rawURL) {
		return 0, fmt.Errorf("拒绝非 Snipaste 官方下载地址: %s", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	if resp.Request == nil || resp.Request.URL == nil || !isOfficialAssetURL(resp.Request.URL.String()) {
		return 0, fmt.Errorf("下载被重定向到非官方地址")
	}

	out, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	total := resp.ContentLength
	buf := make([]byte, 64*1024)
	var done int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return done, err
			}
			done += int64(n)
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return done, readErr
		}
	}
	return done, nil
}

func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func fileSHA1(path string) string {
	return fileHash(path, sha1.New()) // #nosec G401 -- 用于核对官网发布的 SHA-1 值。
}

func fileSHA256(path string) string {
	return fileHash(path, sha256.New())
}

func fileHash(path string, hash io.Writer) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := io.Copy(hash, f); err != nil {
		return ""
	}
	type summer interface{ Sum([]byte) []byte }
	s, ok := hash.(summer)
	if !ok {
		return ""
	}
	return hex.EncodeToString(s.Sum(nil))
}

func verifyOfficialHash(path, algorithm, want string) error {
	if strings.TrimSpace(want) == "" {
		return nil
	}
	var got string
	switch strings.ToLower(strings.TrimSpace(algorithm)) {
	case "sha1":
		got = fileSHA1(path)
	case "sha256":
		got = fileSHA256(path)
	default:
		return fmt.Errorf("不支持的官方哈希算法: %s", algorithm)
	}
	if got == "" {
		return fmt.Errorf("无法读取下载文件进行 %s 校验", algorithm)
	}
	if !strings.EqualFold(got, strings.TrimSpace(want)) {
		return fmt.Errorf("%s 不匹配：期望 %s，实际 %s", algorithm, want, got)
	}
	return nil
}
