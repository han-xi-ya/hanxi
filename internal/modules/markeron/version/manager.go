package version

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	exeName          = "MarkerOn.exe"
	portableMarkName = "markeron.portable" // 0 字节标记：与 exe 同目录即激活便携模式
)

// Manager MarkerOn 版本管理引擎：远程列表、下载完整性校验、保布局解压隔离。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（长超时）
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 10 * time.Minute},
	}
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]MarkerRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// 目录命名 markeron_vX.Y.Z；exe 缺失/为空或便携标记缺失均视为损坏安装跳过。
func (m *Manager) ListInstalled() ([]MarkerVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []MarkerVersionInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		version, ok := versionFromDirName(e.Name())
		if !ok {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		exe := filepath.Join(dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() {
			continue
		}
		if !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}
		// 便携标记与 exe 同目录是便携模式激活的硬条件，缺失视为安装损坏
		if _, statErr := os.Stat(filepath.Join(dir, portableMarkName)); statErr != nil {
			continue
		}

		info := MarkerVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     dir,
			Size:    fi.Size(),
			SHA256:  fileSHA256(exe),
		}
		// 读取元信息（安装时间、来源资产）
		if meta, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
			var mm map[string]any
			if json.Unmarshal(meta, &mm) == nil {
				if at, ok := mm["installedAt"].(string); ok {
					info.InstalledAt = at
				}
			}
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// Download 下载便携 zip 并解压安装到 versions/markeron_vX.Y.Z/。
// 上游无官方 checksums 资产，完整性依赖三级兜底：
//  1. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改文件头级错误）；
//  2. archive/zip 读取每个 entry 时强制 CRC32 校验（extractAll 读满不提前返回）；
//  3. 提取后布局自检（exe 非空 + 便携标记存在），失败清理目录。
//
// onProgress 可选：实时上报各阶段进度（下载字节、解压）。
func (m *Manager) Download(version string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应的远程资产
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *MarkerRelease
	for i := range releases {
		if releases[i].Version == version {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		err := fmt.Errorf("远程列表不存在版本 %s", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	zipURLs := m.assetMirrors(version, rel.AssetName)
	tmpZip, err := os.CreateTemp("", "hubkit-markeron-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 下载 zip（直连 + 镜像逐个回退）
	emit("downloading", 0, rel.Size, "")
	if err := m.downloadTo(zipURLs, tmpZipPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 3. 下载完整性：落盘字节数必须与 release API 声明的 size 一致
	actual, err := fileSize(tmpZipPath)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 4. 解压保布局安装到隔离目录（zip 内建 CRC32 在此阶段逐 entry 校验）
	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, "markeron_"+strings.TrimPrefix(version, "v"))
	if err := extractAll(tmpZipPath, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 5. 落盘元信息（zip 自哈希仅作诊断记录，非校验依据）
	meta := map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"source":      rel.AssetName,
		"zipSize":     rel.Size,
		"zipSHA256":   fileSHA256(tmpZipPath),
	}
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), meta)

	emit("done", 100, 100, "")
	return nil
}

// Remove 卸载指定版本（删除隔离目录）
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// ResolveExe 返回指定版本的 MarkerOn.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（markeron_vX.Y.Z）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	dir := filepath.Join(m.versionsDir, "markeron_"+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 v%s 未安装，请先在下方版本管理下载", ver)
}

// versionFromDirName 解析版本目录名为规范版本号（markeron_vX.Y.Z → vX.Y.Z）
func versionFromDirName(name string) (string, bool) {
	const prefix = "markeron_"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(name[len(prefix):], "v")
	if rest == "" {
		return "", false
	}
	if plainVersionRe.MatchString(rest) {
		return "v" + rest, true // 规范化为 vX.Y.Z，与远程 ListReleases 保持一致
	}
	return "", false
}

// plainVersionRe 纯版本号（如 2.9.4），用于识别版本隔离目录名
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// ---------- 内部工具 ----------

// assetMirrors 构造直连与镜像下载 URL 候选列表。
// 路径模板对任意 owner/repo 泛化，与 frpc 模块共用同一组镜像前缀。
func (m *Manager) assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// downloadTo 依次尝试候选 URL 下载到目标文件，支持重试与镜像故障转移
func (m *Manager) downloadTo(urls []string, dest string, onProgress func(done int64)) error {
	const maxRetries = 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		for _, u := range urls {
			err := m.tryDownloadSingle(u, dest, onProgress)
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

func (m *Manager) tryDownloadSingle(url, dest string, onProgress func(done int64)) error {
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

	resp, err := m.client.Do(req)
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

// extractAll 全量解压 zip 到目标目录（保持原布局：exe、0 字节 portable 标记、README）。
// 每个 entry 必须读满——completion 路径中的 io.Copy 跑完触发 archive/zip 内建 CRC32 校验。
// 提取完成后自检布局，不符（缺 exe/缺标记/恶意条目）即清理目标目录报错。
func extractAll(zipPath, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	fail := func(err error) error {
		_ = os.RemoveAll(targetDir)
		return err
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		// ZipSlip 防护：拒绝绝对路径与逃逸出目标目录的条目
		clean := filepath.Clean(f.Name)
		if filepath.IsAbs(clean) || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fail(fmt.Errorf("zip 含非法路径条目 %q", f.Name))
		}
		target := filepath.Join(targetDir, clean)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fail(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fail(err)
		}
		rc, err := f.Open()
		if err != nil {
			return fail(err)
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return fail(err)
		}
		// 必须读满：提前返回会跳过 CRC32 校验
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return fail(copyErr)
		}
	}

	// 布局自检：exe 存在且非空 + 便携标记存在（0 字节合法）
	fi, err := os.Stat(filepath.Join(targetDir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fail(fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName))
	}
	if _, err := os.Stat(filepath.Join(targetDir, portableMarkName)); err != nil {
		return fail(fmt.Errorf("zip 布局无效：缺少 %s 便携标记", portableMarkName))
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

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}