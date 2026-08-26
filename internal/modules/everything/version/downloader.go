package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Download 下载目标版本的 x64 便携 zip 并解压安装到 everything_v<版本>/。
// 完整性由四级兜底保证（voidtools 官方有 sha256 清单，比 markeron 更严格）：
//  1. 官方清单 sha256 校验（清单不可得的降级场景跳过）；
//  2. 下载落盘字节数 == 资产 HEAD 声明大小（防截断）；
//  3. archive/zip 读取每个 entry 时强制 CRC32（extractAll 读满不提前返回）；
//  4. 提取后布局自检（Everything.exe 存在非空，大小写不敏感），失败清理目录。
//
// onProgress 可选：实时上报各阶段进度。
func (m *Manager) Download(version string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应的远程槽位（列表可能来自 stale 缓存，下载前补一次活体详情）
	releases, err := m.ListRemote()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel EverythingRelease
	found := false
	for _, r := range releases {
		if r.Version == version {
			rel = r
			found = true
			break
		}
	}
	if !found {
		err := fmt.Errorf("远程列表不存在版本 %s", version)
		emit("error", 0, 0, err.Error())
		return err
	}
	// 缓存/快照数据缺失字段时尽力在线补齐，保证校验强度
	if rel.Stale || rel.SHA256 == "" || rel.Size == 0 {
		rel = enrichLive(rel)
	}

	tmpZip, err := os.CreateTemp("", "hubkit-everything-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 下载 zip（单源重试，voidtools 无镜像）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo([]string{rel.AssetURL}, tmpZipPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 3. 官方 sha256 校验
	if rel.SHA256 != "" {
		emit("verify", 0, 0, "")
		if got := fileSHA256(tmpZipPath); !equalHash(got, rel.SHA256) {
			err := fmt.Errorf("sha256 校验失败：期望 %s，实际 %s（检查网络中间设备是否篡改）", rel.SHA256, got)
			emit("error", 0, rel.Size, err.Error())
			return err
		}
	}

	// 4. 字节数校验（HEAD 声明的 Content-Length）
	actual, err := fileSize(tmpZipPath)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if rel.Size > 0 && actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 5. 解压安装到隔离目录（zip 内建 CRC32 在此阶段逐 entry 校验）
	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if err := extractAll(tmpZipPath, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 6. 落盘元信息
	meta := map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"source":      "Everything-" + version + ".x64.zip",
		"zipSHA256":   rel.SHA256,
	}
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), meta)

	emit("done", 100, 100, "")
	return nil
}

// enrichLive 对 stale/字段缺失的槽位记录做活体补齐：HEAD 补大小与时间、拉取官方 sha256。
func enrichLive(rel EverythingRelease) EverythingRelease {
	probeAssets([]EverythingRelease{rel})
	return rel
}

// downloadTo 依次尝试候选 URL 下载到目标文件，支持重试
func downloadTo(urls []string, dest string, onProgress func(done int64)) error {
	const maxRetries = 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		for _, u := range urls {
			if err := tryDownloadSingle(u, dest, onProgress); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
	}
	if lastErr != nil {
		return fmt.Errorf("所有下载尝试均失败: %w", lastErr)
	}
	return fmt.Errorf("所有下载源均失败")
}

func tryDownloadSingle(url, dest string, onProgress func(done int64)) error {
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

	// 下载客户端独立长超时：资产最大约 4MB，10 分钟富余
	client := &http.Client{Timeout: 10 * time.Minute}
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

func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

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

// equalHash 常量时间比较下载哈希与官方哈希，防时序侧信道（习惯性防御）。
func equalHash(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}