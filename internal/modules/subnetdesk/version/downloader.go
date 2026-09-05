package version

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/ccswitch 同一组镜像前缀）。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// downloadTo 依次尝试候选 URL 下载到目标文件，支持重试与镜像故障转移。
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

// fileSHA256 计算文件 sha256（仅作记录诊断；下载校验使用官方 release digest）。
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

// verifyPEMagic 校验文件头 MZ 魔数：单文件 exe 无 zip 布局可自检，
// PE 魔数是"落地文件确实是可执行体而非 HTML 错误页/截断残片"的最低断言。
func verifyPEMagic(path string) error {
	head, err := readHead(path, 2)
	if err != nil {
		return err
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return fmt.Errorf("文件不是有效的 Windows 可执行体（MZ 头缺失，疑似镜像返回了错误页）")
	}
	return nil
}

// verifyMSIMagic 校验 MSI 复合文档魔数 D0 CF 11 E0（OLE 头）。
// 勿复用 verifyPEMagic：MSI 不是 PE 文件，MZ 断言会把好包全部误拒。
func verifyMSIMagic(path string) error {
	head, err := readHead(path, 4)
	if err != nil {
		return err
	}
	if head[0] != 0xD0 || head[1] != 0xCF || head[2] != 0x11 || head[3] != 0xE0 {
		return fmt.Errorf("文件不是有效的 Windows Installer 包（MSI 复合文档头缺失，疑似镜像返回了错误页）")
	}
	return nil
}

// readHead 读取文件前 n 字节。
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, n)
	if _, err := io.ReadFull(f, head); err != nil {
		return nil, fmt.Errorf("读取文件头失败: %w", err)
	}
	return head, nil
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
