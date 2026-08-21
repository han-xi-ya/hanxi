package version

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Manager frp 版本管理引擎：远程列表、下载硬校验、解压隔离、本地导入。
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
func (m *Manager) ListRemote() ([]FrpRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// 目录名兼容两种命名：frp_v0.61.1（规范）与 frp_0.61.1（历史下载创建）。
func (m *Manager) ListInstalled() ([]FrpVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []FrpVersionInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		version, ok := versionFromDirName(e.Name())
		if !ok {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		exe := filepath.Join(dir, "frpc.exe")
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() {
			continue
		}
		if !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}

		info := FrpVersionInfo{
			Version: version,
			ExePath: exe,
			Size:    fi.Size(),
			SHA256:  fileSHA256(exe),
		}
		// 读取元信息（安装时间、导入标记）
		if meta, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
			var mm map[string]any
			if json.Unmarshal(meta, &mm) == nil {
				if at, ok := mm["installedAt"].(string); ok {
					info.InstalledAt = at
				}
				if imp, ok := mm["isImport"].(bool); ok {
					info.IsImport = imp
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

// Download 下载并硬校验指定版本，解压提取 frpc.exe 到 versions/frp_vX.Y.Z/
// onProgress 可选：实时上报各阶段进度（下载字节、校验、解压）。
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
	var rel *FrpRelease
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
	tmpZip, err := os.CreateTemp("", "hubkit-frp-*.zip")
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

	// 3. SHA256 硬校验（优先官方 checksums；缺失时降级为文件自哈希兜底）
	expected := rel.SHA256
	if expected == "" {
		// 尝试从该版本 checksums 资产补取
		if chk := m.tryFetchChecksum(version); chk != "" {
			expected = chk
		}
	}
	emit("hash", 0, 0, "")
	actual := fileSHA256(tmpZipPath)
	if expected != "" && !strings.EqualFold(expected, actual) {
		err := fmt.Errorf("SHA256 校验失败：期望 %s，实际 %s", expected, actual)
		emit("error", 0, 0, err.Error())
		return err
	}

	// 4. 解压提取 frpc.exe 到隔离目录
	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, "frp_"+strings.TrimPrefix(version, "v"))
	if err := extractFrpc(tmpZipPath, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 5. 落盘元信息
	meta := map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    false,
		"sha256":      actual,
		"source":      rel.AssetName,
	}
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), meta)

	emit("done", 100, 100, "")
	return nil
}

// ImportLocal 手动导入本地 frpc.exe（自动探测版本号）
func (m *Manager) ImportLocal(srcExe string) (FrpVersionInfo, error) {
	fi, err := os.Stat(srcExe)
	if err != nil {
		return FrpVersionInfo{}, err
	}

	version, vErr := detectVersion(srcExe)
	if vErr != nil || version == "" {
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, "frp_"+version)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return FrpVersionInfo{}, err
	}
	targetExe := filepath.Join(targetDir, "frpc.exe")
	src, err := os.Open(srcExe)
	if err != nil {
		return FrpVersionInfo{}, err
	}
	defer src.Close()
	dst, err := os.Create(targetExe)
	if err != nil {
		return FrpVersionInfo{}, err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return FrpVersionInfo{}, err
	}
	dst.Close()

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcExe,
	})

	return FrpVersionInfo{
		Version:     version,
		ExePath:     targetExe,
		Size:        fi.Size(),
		SHA256:      fileSHA256(targetExe),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
	}, nil
}

// Remove 卸载指定版本（删除隔离目录，兼容新旧两种命名）
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// ResolveExe 返回指定版本的 frpc.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "frpc.exe"), nil
}

// resolveVersionDir 定位版本隔离目录（优先 frp_vX.Y.Z，回退 frp_X.Y.Z）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	for _, name := range []string{"frp_v" + ver, "frp_" + ver} {
		dir := filepath.Join(m.versionsDir, name)
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir, nil
		}
	}
	return "", fmt.Errorf("版本 v%s 未安装，请先在版本管理页面下载或导入", ver)
}

// versionFromDirName 解析版本目录名为规范版本号，兼容 frp_vX.Y.Z / frp_X.Y.Z / frp_imported-*
func versionFromDirName(name string) (string, bool) {
	const prefix = "frp_"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rest := name[len(prefix):]
	rest = strings.TrimPrefix(rest, "v")
	if rest == "" {
		return "", false
	}
	if plainVersionRe.MatchString(rest) {
		return "v" + rest, true // 规范化为 vX.Y.Z，与远程 ListReleases 保持一致
	}
	if strings.HasPrefix(rest, "imported-") {
		return rest, true
	}
	return "", false
}

// plainVersionRe 纯版本号（如 0.61.2），用于识别版本隔离目录名
var plainVersionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// ---------- 内部工具 ----------

// assetMirrors 构造直连与镜像下载 URL 候选列表
func (m *Manager) assetMirrors(version, assetName string) []string {
	tag := version
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, assetName)
	urls := []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
	return urls
}

// tryFetchChecksum 尝试从官方 checksums 资产补取版本哈希
func (m *Manager) tryFetchChecksum(version string) string {
	ver := strings.TrimPrefix(version, "v")
	urls := []string{
		"https://github.com/" + repoOwner + "/" + repoName + "/releases/download/" + version + "/frp_" + ver + "_sha256-checksums.txt",
		"https://ghfast.top/https://github.com/" + repoOwner + "/" + repoName + "/releases/download/" + version + "/frp_" + ver + "_sha256-checksums.txt",
	}
	fileName := "frp_" + ver + "_windows_amd64.zip"
	for _, u := range urls {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := apiClient().Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		for _, line := range strings.Split(string(body), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && strings.HasSuffix(fields[1], fileName) {
				return fields[0]
			}
		}
	}
	return ""
}

// downloadTo 依次尝试候选 URL 下载到临时文件（follow redirect）
func (m *Manager) downloadTo(urls []string, dest string, onProgress func(done int64)) error {
	for _, u := range urls {
		err := m.tryDownloadSingle(u, dest, onProgress)
		if err == nil {
			return nil
		}
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

// extractFrpc 解压 zip 提取 frpc.exe（兼容根目录存放与一级子目录两种布局）
func extractFrpc(zipPath, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), "frpc.exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(targetDir, "frpc.exe"))
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
		return nil
	}
	return fmt.Errorf("zip 中未找到 frpc.exe")
}

var versionRe = regexp.MustCompile(`frp(?:c)? version(?:[:=]| )\s*v?(\d+\.\d+\.\d+)`)

// detectVersion 运行 frpc -v 探测版本号
func detectVersion(exe string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-v")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(out))
	if m := versionRe.FindStringSubmatch(text); len(m) == 2 {
		return "v" + m[1], nil
	}
	return text, nil
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
