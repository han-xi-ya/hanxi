package version

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
)

const (
	exeName          = "cc-switch.exe"
	portableMarkName = "portable.ini" // 官方绿色版标记：exe 同目录即禁用内置 Updater
	dirPrefix        = "ccswitch_"    // 版本隔离目录前缀（与 frp_v0.61.1 / markeron_v2.9.4 同构）
)

// plainVersionRe 纯版本号（如 3.20.0），用于目录名与 FileVersion 校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本目录名（ccswitch_3.20.0）；
// imported- 分支收纳版本探测失败时间戳兜底的导入（无 FileVersion 资源时）
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// Manager CC Switch 版本管理引擎：远程列表、下载完整性校验、保布局解压隔离、本地导入。
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
func (m *Manager) ListRemote() ([]CCRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// exe 缺失/为空或便携标记缺失均视为损坏安装跳过（官方便携 zip 恒含二者）。
func (m *Manager) ListInstalled() ([]CCVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []CCVersionInfo
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
		// 绿色版标记与 exe 同目录是便携模式的激活条件：
		// 缺失则内置 Updater 处于启用态，托管后可能被其自动更新干扰，视为安装损坏
		if _, statErr := os.Stat(filepath.Join(dir, portableMarkName)); statErr != nil {
			continue
		}

		info := CCVersionInfo{
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

// Download 下载便携 zip 并解压安装到 versions/ccswitch_X.Y.Z/。
// 上游提供官方 sha256（GitHub API digest），完整性四层兜底：
//  1. 官方 sha256 校验（第一主依据）；
//  2. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改）；
//  3. archive/zip 读取每个 entry 时强制 CRC32 校验（extractAll 读满不提前返回）；
//  4. 提取后布局自检（exe 非空 + portable.ini 存在），失败清理目录。
//
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
	var rel *CCRelease
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

	tmpZip, err := os.CreateTemp("", "hanxi-ccswitch-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 下载 zip（直连 + 镜像逐个回退）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(version, rel.AssetName), tmpZipPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 3. 字节数校验
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

	// 4. 官方 sha256 校验
	emit("verify", 0, 0, "")
	if err := verifySHA256(tmpZipPath, rel.SHA256); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
	}

	// 5. 解压保布局安装到隔离目录（zip 内建 CRC32 在此阶段逐 entry 校验）
	emit("extract", 0, 0, "")
	targetDir := filepath.Join(m.versionsDir, dirPrefix+strings.TrimPrefix(version, "v"))
	if err := extractAll(tmpZipPath, targetDir); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 6. 落盘元信息
	meta := map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"source":       rel.AssetName,
		"zipSize":      rel.Size,
		"zipSHA256":    fileSHA256(tmpZipPath),
		"assetSHA256":  rel.SHA256,
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

// ResolveExe 返回指定版本的 cc-switch.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（ccswitch_X.Y.Z 或 ccswitch_imported-时间戳）
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(ver) {
		if !importedDirRe.MatchString(ver) {
			return "", fmt.Errorf("非法版本号: %q", version)
		}
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+ver)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// ImportLocal 导入本地已安装的 CC Switch（安装版或旧绿色版均可：只需单 exe）。
// 与 everything 的整套导入不同：cc-switch 的配置恒在 ~/.cc-switch 用户目录，
// 与 exe 位置无关，故只需迁移 exe 与（若存在的）便携标记。
// 调用方需先确保源实例未运行（信号：进程句柄独占不产生写锁，本场景仅防御性约定）。
func (m *Manager) ImportLocal(srcDir string) (CCVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return CCVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return CCVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return CCVersionInfo{}, err
	}

	// 白名单迁移：exe + 便携标记（存在则保持绿色版语义）。其余文件一概不搬。
	var copied []string
	if err := copyFileTo(srcExe, filepath.Join(targetDir, exeName)); err != nil {
		_ = os.RemoveAll(targetDir)
		return CCVersionInfo{}, err
	}
	copied = append(copied, exeName)
	if pi, err := os.Stat(filepath.Join(srcDir, portableMarkName)); err == nil && !pi.IsDir() {
		if err := copyFileTo(filepath.Join(srcDir, portableMarkName), filepath.Join(targetDir, portableMarkName)); err != nil {
			_ = os.RemoveAll(targetDir)
			return CCVersionInfo{}, err
		}
		copied = append(copied, portableMarkName)
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      strings.Join(copied, ", "),
	})

	return CCVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// extractAll 全量解压 zip 到目标目录。每个 entry 必须读满——
// completion 路径中的 io.Copy 跑完触发 archive/zip 内建 CRC32 校验。
// 提取完成后自检布局（exe 非空 + 便携标记存在），不符即清理目标目录报错。
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

	// 布局自检：exe 存在且非空 + 便携标记存在（官方 zip 恒有二者）
	fi, err := os.Stat(filepath.Join(targetDir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fail(fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName))
	}
	if _, err := os.Stat(filepath.Join(targetDir, portableMarkName)); err != nil {
		return fail(fmt.Errorf("zip 布局无效：缺少 %s 便携标记", portableMarkName))
	}
	return nil
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
