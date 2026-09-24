package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/packages/go/netx"
)

const (
	dirPrefix = "douzy_" // 版本隔离目录前缀（与 ccswitch_ / rustdesk_ 同构）
	metaName  = "meta.json"
)

// plainVersionRe 纯版本号（如 0.11.5），用于目录名校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// dirNameRe 版本隔离目录名（douzy_0.11.5）。本模块不做本地导入，无 imported- 分支。
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `\d+\.\d+\.\d+$`)

// Manager Douzy 版本管理引擎：远程列表、安装包下载与校验、隔离目录存档。
// 仅"下载成品安装包"，不含任何进程托管职责（无 ResolveExe / 启停 / 探测）。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（长超时，适配 ~147MB 包）
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      netx.NewClient(10*time.Minute, nil),
	}
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]DouzyRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已下载安装包目录。目录内安装包缺失/为空视为损坏跳过。
func (m *Manager) ListInstalled() ([]DouzyVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []DouzyVersionInfo
	for _, e := range entries {
		if !e.IsDir() || !dirNameRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		installer, err := installerFile(dir)
		if err != nil {
			continue // 无可用安装包（损坏/半途中断）
		}
		fi, _ := os.Stat(installer)

		info := DouzyVersionInfo{
			Version: "v" + strings.TrimPrefix(e.Name(), dirPrefix),
			ExePath: installer,
			Dir:     dir,
		}
		if fi != nil {
			info.Size = fi.Size()
		}
		if meta, err := os.ReadFile(filepath.Join(dir, metaName)); err == nil {
			var mm map[string]any
			if json.Unmarshal(meta, &mm) == nil {
				if at, ok := mm["installedAt"].(string); ok {
					info.InstalledAt = at
				}
				if sha, ok := mm["actualSHA256"].(string); ok {
					info.SHA256 = sha
				}
			}
		}
		if info.InstalledAt == "" && fi != nil {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// installerFile 定位隔离目录中的安装包：优先 meta.installerName（官方原名），
// 回退目录内唯一的 *.exe。文件缺失/为空/非常规返回错误。
func installerFile(dir string) (string, error) {
	if meta, err := os.ReadFile(filepath.Join(dir, metaName)); err == nil {
		var mm map[string]any
		if json.Unmarshal(meta, &mm) == nil {
			if name, ok := mm["installerName"].(string); ok && name != "" {
				p := filepath.Join(dir, name)
				if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
					return p, nil
				}
			}
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.exe"))
	for _, p := range matches {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("目录中未找到安装包 exe")
}

// resolveRelease 按规范版本号（vX.Y.Z）定位远程 release（10 分钟缓存复用）。
func (m *Manager) resolveRelease(version string) (*DouzyRelease, error) {
	releases, err := remoteCache.get()
	if err != nil {
		return nil, fmt.Errorf("获取远程版本列表失败: %w", err)
	}
	for i := range releases {
		if releases[i].Version == version {
			return &releases[i], nil
		}
	}
	return nil, fmt.Errorf("远程列表不存在版本 %s", version)
}

// Download 下载 Windows 安装包到 versions/douzy_X.Y.Z/<官方原名>。
// 单文件无 zip 布局，完整性三层兜底（对齐 rustdesk 便携下载纪律）：
//  1. 官方 sha256（GitHub digest，第一主依据）；
//  2. 落盘字节数 == release API 声明 size（防截断/代理篡改）；
//  3. MZ 魔数断言（防镜像错误页伪装 exe）。
//
// meta.json 记录期望/实际哈希供事后诊断。onProgress 可选：实时上报各阶段进度。
// 注意：本方法只负责"取包并校验"，安装动作交还用户（见 service.LaunchInstaller）。
func (m *Manager) Download(version string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应的远程资产
	rel, err := m.resolveRelease(version)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpFile, err := os.CreateTemp("", "hanxi-douzy-*.exe")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	tmpFile.Close()

	// 2. 下载安装包（直连 + 镜像逐个回退；传原始 tag 拼下载 URL）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(rel.Tag, rel.AssetName), tmpPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 3. 字节数校验
	actual, err := fileSize(tmpPath)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 4. 官方 sha256 + PE 魔数校验
	emit("verify", 0, 0, "")
	if err := verifySHA256(tmpPath, rel.SHA256); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
	}
	if err := verifyPEMagic(tmpPath); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 5. 落位到隔离目录（rename 优先，跨卷回退复制；保留官方资产原名）
	emit("install", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+strings.TrimPrefix(version, "v"))
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		emit("error", 0, 0, fmt.Sprintf("创建目录失败: %v", err))
		return err
	}
	targetExe := filepath.Join(targetDir, rel.AssetName)
	if err := placeFile(tmpPath, targetExe); err != nil {
		_ = os.RemoveAll(targetDir)
		emit("error", 0, 0, fmt.Sprintf("安装包落位失败: %v", err))
		return err
	}

	// 6. 落盘元信息
	meta := map[string]any{
		"installedAt":   time.Now().Format("2006-01-02 15:04:05"),
		"installerName": rel.AssetName,
		"exeSize":       rel.Size,
		"assetSHA256":   rel.SHA256,
		"actualSHA256":  fileSHA256(targetExe),
		"verifiedHash":  true,
	}
	_ = writeJSON(filepath.Join(targetDir, metaName), meta)

	emit("done", 100, 100, "")
	return nil
}

// InstallerPath 返回指定版本已下载安装包的完整路径（供 service 拉起安装向导）。
// 未下载/损坏返回错误。
func (m *Manager) InstallerPath(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return installerFile(dir)
}

// Remove 删除指定版本的已下载安装包目录
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// resolveVersionDir 定位版本隔离目录（douzy_X.Y.Z）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(ver) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("尚未下载版本 %s 的安装包", version)
}

// placeFile 将临时文件落位到目标路径：同卷 rename 原子；跨卷（EXDEV）回退复制。
func placeFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyFileTo(src, dst)
}
