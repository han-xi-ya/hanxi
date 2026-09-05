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

	"hanxi/internal/platform/versioninfo"
)

const (
	exeName   = "subnetdesk.exe" // packer 外层落盘定名：ResolveExe 确定性寻径；外层名不影响内层（提取目录由内层条目名派生 %LOCALAPPDATA%\subnetdesk）
	dirPrefix = "subnetdesk_"    // 版本隔离目录前缀（与 ccswitch_ / frpc_v 同构）
)

// plainVersionRe 纯版本号（如 1.3.0），用于目录名校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本目录名（subnetdesk_1.3.0）；imported- 分支收纳探测失败的时间戳兜底
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// assetVerRe 官方资产文件名中的版本号段（subnetdesk-1.3.0-x86_64.exe → 1.3.0）
var assetVerRe = regexp.MustCompile(`(?i)^subnetdesk-?v?(\d+\.\d+\.\d+)`)

// importCandidateRe 导入候选：官方形态便携 exe；msi 安装器与 sciter/aarch64 变体绝不可收
var importCandidateRe = regexp.MustCompile(`(?i)^subnetdesk-.*\.exe$`)

// Manager SubnetDesk 版本管理引擎：远程列表、单文件下载校验、隔离目录与本地导入。
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
func (m *Manager) ListRemote() ([]SDRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。exe 缺失/为空视为损坏安装跳过。
func (m *Manager) ListInstalled() ([]SDVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []SDVersionInfo
	for _, e := range entries {
		if !e.IsDir() || !dirNameRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		exe := filepath.Join(dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}

		info := SDVersionInfo{
			Version: "v" + strings.TrimPrefix(e.Name(), dirPrefix),
			ExePath: exe,
			Dir:     dir,
			Size:    fi.Size(),
		}
		// 读取元信息（安装时间、导入来源）
		if meta, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
			var mm map[string]any
			if json.Unmarshal(meta, &mm) == nil {
				if at, ok := mm["installedAt"].(string); ok {
					info.InstalledAt = at
				}
				if isIm, ok := mm["isImport"].(bool); ok {
					info.IsImport = isIm
				}
				if src, ok := mm["source"].(string); ok {
					info.Source = src
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

// Download 下载便携 packer exe 安装到 versions/subnetdesk_X.Y.Z/subnetdesk.exe。
// 单文件无 zip 布局，完整性校验：
//  1. 官方 sha256（GitHub digest，第一主依据）；
//  2. 落盘字节数 == release API 声明 size（防截断/代理篡改）；
//  3. MZ 魔数断言（防镜像错误页伪装 exe）；
//  4. meta.json 记录期望哈希与实际哈希（事后诊断）。
//
// onProgress 可选：实时上报各阶段进度。
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
	var rel *SDRelease
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

	tmpFile, err := os.CreateTemp("", "hanxi-subnetdesk-*.exe")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	tmpFile.Close()

	// 2. 下载 exe（直连 + 镜像逐个回退）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(version, rel.AssetName), tmpPath, func(done int64) {
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

	// 5. 落位到隔离目录（rename 优先，跨卷回退复制）
	emit("install", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+strings.TrimPrefix(version, "v"))
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		emit("error", 0, 0, fmt.Sprintf("创建版本目录失败: %v", err))
		return err
	}
	targetExe := filepath.Join(targetDir, exeName)
	if err := placeFile(tmpPath, targetExe); err != nil {
		_ = os.RemoveAll(targetDir)
		emit("error", 0, 0, fmt.Sprintf("安装失败: %v", err))
		return err
	}

	// 6. 落盘元信息
	meta := map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"source":       rel.AssetName,
		"exeSize":      rel.Size,
		"assetSHA256":  rel.SHA256,
		"actualSHA256": fileSHA256(targetExe),
		"verifiedHash": true,
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

// ResolveExe 返回指定版本的 packer exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（subnetdesk_X.Y.Z 或 subnetdesk_imported-时间戳）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(ver) && !importedDirRe.MatchString(ver) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// ImportLocal 导入本机已有的 SubnetDesk 便携 exe（文件路径或所在目录均可）。
// 配置恒在 %APPDATA%\SubnetDesk（Roaming，与安装位置无关），故只搬 exe 本体。
// 安装版（Program Files）与 msi 拒收：msi 不是可托管的可执行体，安装版含
// 系统服务/驱动组件，单搬 exe 无法还原其完整形态。
// 调用方需先确保源实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(path string) (SDVersionInfo, error) {
	srcExe, err := resolveImportExe(path)
	if err != nil {
		return SDVersionInfo{}, err
	}
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return SDVersionInfo{}, fmt.Errorf("源文件不可用: %s", srcExe)
	}
	// install.exe 后缀会被 packer 判定为安装器（is_setup 规则），收进来等于托管一个自安装炸弹
	if strings.HasSuffix(strings.ToLower(fi.Name()), "install.exe") {
		return SDVersionInfo{}, fmt.Errorf("检测到安装器形态文件名（以 install.exe 结尾），请改用便携版原始文件名导入")
	}
	if err := verifyPEMagic(srcExe); err != nil {
		return SDVersionInfo{}, fmt.Errorf("源文件不是有效的 Windows 可执行体: %w", err)
	}

	version := versionFromImportName(fi.Name())
	if version == "" {
		// 文件名无版本段（用户改名）：读 PE 版本资源；再失败时间戳兜底（与 frpc 同构）
		if fv, vErr := versioninfo.FileVersion(srcExe); vErr == nil && plainVersionRe.MatchString(fv) {
			version = fv
		} else {
			version = "imported-" + time.Now().Format("20060102-150405")
		}
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return SDVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return SDVersionInfo{}, err
	}
	if err := copyFileTo(srcExe, filepath.Join(targetDir, exeName)); err != nil {
		_ = os.RemoveAll(targetDir)
		return SDVersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      filepath.Dir(srcExe),
		"copied":      fi.Name(),
		"exeSHA256":   fileSHA256(filepath.Join(targetDir, exeName)),
	})

	return SDVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      filepath.Dir(srcExe),
	}, nil
}

// resolveImportExe 归一化导入入参：文件路径直接用（须为便携 exe 形态名）；
// 目录则在官方资产命名形态中挑选 x86_64 便携 exe（多版本取文件名序最大者）。
func resolveImportExe(path string) (string, error) {
	path = strings.TrimSpace(path)
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("路径不存在或不可访问: %s", path)
	}
	if !fi.IsDir() {
		name := strings.ToLower(fi.Name())
		if !importCandidateRe.MatchString(fi.Name()) {
			return "", fmt.Errorf("文件名不符合官方便携版形态（subnetdesk-版本-x86_64.exe）: %s", fi.Name())
		}
		if strings.Contains(name, "aarch64") || strings.Contains(name, "sciter") {
			return "", fmt.Errorf("不支持非 x64/sciter 变体: %s", fi.Name())
		}
		return path, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	var best string
	for _, e := range entries {
		if e.IsDir() || !importCandidateRe.MatchString(e.Name()) {
			continue
		}
		lower := strings.ToLower(e.Name())
		if strings.Contains(lower, "aarch64") || strings.Contains(lower, "sciter") {
			continue
		}
		if best == "" || strings.ToLower(e.Name()) > strings.ToLower(best) {
			best = e.Name()
		}
	}
	if best == "" {
		return "", fmt.Errorf("目录中未找到官方形态的便携 exe（subnetdesk-*-x86_64.exe）: %s", path)
	}
	return filepath.Join(path, best), nil
}

// versionFromImportName 从官方资产文件名提取版本号（无则空串）
func versionFromImportName(name string) string {
	if mm := assetVerRe.FindStringSubmatch(name); len(mm) == 2 && plainVersionRe.MatchString(mm[1]) {
		return mm[1]
	}
	return ""
}

// placeFile 将临时文件落位到目标路径：同卷 rename 原子；跨卷（EXDEV）回退复制。
func placeFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyFileTo(src, dst)
}
