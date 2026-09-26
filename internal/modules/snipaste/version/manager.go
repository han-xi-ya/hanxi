package version

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"

	"hanxi/packages/go/netx"
)

const (
	exeName   = "Snipaste.exe"
	dirPrefix = "snipaste_v"
)

var plainVersionRe = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)+(?:-Beta[0-9]*)?$`)

// Manager 管理 Snipaste 官网免安装版的隔离安装目录。
//
// Wave 4 内核委托边界（与 markeron/rufus 的差异皆由 Snipaste 上游事实决定）：
//   - 解包主流程委托 artifact.UnpackZip（ZipSlip/链接/炸弹/CRC32/Windows 文件名
//     纪律全闸门，取代本包此前手写的 extractAll 安全解压副本）；
//   - 下载校验不可委托 artifact.Fetch：内核以官方 SHA-256 为唯一信任根（必检
//     64 位十六进制），而 Snipaste 官网独立校验清单只发布 SHA-1（sha-1.txt）且
//     zip 内无官方 SHA-256 可比对——委托即等于放弃可信校验或造假，故保留本包
//     "官网域名锁 + 尺寸核验 + 官方 SHA-1 + 布局/FileVersion 自检"链；
//   - 落位/扫描/卸载不可委托 artifact.Tree：内核版本树目录名固定为
//     <entry>_<version>，表达不了既有 snipaste_v<version> 布局（多一枚下划线即
//     改变盘上目录名）；内核统一账本 artifact.Meta 也承载不了 SnipasteVersionInfo
//     的 verificationMode/officialHash/hashAlgorithm/isImport 字段（前端契约）。
//     原子落位（staging+rename）与 .removing- 隔离卸载由本包自持，纪律同内核。
type Manager struct {
	versionsDir string
	client      *http.Client
	cache       *releaseCache
	fileVersion func(string) (string, error)
}

// NewManager 以 versions 根目录创建管理器；fileVersion 注入 PE 版本读取，单测可替换。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      netx.NewClient(10*time.Minute, nil),
		cache:       remoteCache,
		fileVersion: versioninfo.FileVersion,
	}
}

// ListRemote 返回官网发布通道列表（releaseCache 缓存命中则免网络）。
func (m *Manager) ListRemote() ([]SnipasteRelease, error) {
	return m.cache.get()
}

// ListInstalled 枚举 snipaste_v<ver> 版本目录；跳过 .installing-/.removing- 前缀的
// 安装/卸载中间态目录，避免把半成品当成功安装项展示。
func (m *Manager) ListInstalled() ([]SnipasteVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []SnipasteVersionInfo
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), dirPrefix) || strings.Contains(entry.Name(), ".installing-") || strings.Contains(entry.Name(), ".removing-") {
			continue
		}
		version := strings.TrimPrefix(entry.Name(), dirPrefix)
		if !plainVersionRe.MatchString(version) {
			continue
		}
		dir := filepath.Join(m.versionsDir, entry.Name())
		exe := filepath.Join(dir, exeName)
		fi, err := os.Stat(exe)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}
		if actual, err := m.fileVersion(exe); err != nil || normalizeVersion(actual) != normalizeVersion(version) {
			continue
		}
		info := SnipasteVersionInfo{Version: version, ExePath: exe, Dir: dir, Size: dirSize(dir, fi.Size())}
		readMeta(filepath.Join(dir, "meta.json"), &info)
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	sort.SliceStable(list, func(i, j int) bool {
		return versioncmp.Compare(list[i].Version, list[j].Version) > 0
	})
	return list, nil
}

func readMeta(path string, info *SnipasteVersionInfo) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var meta struct {
		InstalledAt      string `json:"installedAt"`
		IsImport         bool   `json:"isImport"`
		Source           string `json:"source"`
		PackageSHA256    string `json:"packageSHA256"`
		VerificationMode string `json:"verificationMode"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return
	}
	info.InstalledAt = meta.InstalledAt
	info.IsImport = meta.IsImport
	info.Source = meta.Source
	info.PackageSHA256 = meta.PackageSHA256
	info.VerificationMode = meta.VerificationMode
}

// Download 下载官网免安装 zip 并解压到隔离目录：下载→尺寸/官方哈希核验→
// 内核安全解包 staging→布局与 FileVersion 自检→Rename 原子落位；
// onProgress 收 resolve/downloading/verify-*/verify-archive/install/done|error（既有词表）。
// 版本号先过 plainVersionRe 白名单再拼路径，拒绝注入；已安装直接报错不覆盖。
func (m *Manager) Download(targetVersion string, onProgress func(DownloadProgress)) error {
	version := normalizeVersion(targetVersion)
	emit := func(stage string, done, total int64, message string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: message})
		}
	}
	if !plainVersionRe.MatchString(version) {
		return fmt.Errorf("非法版本号: %q", targetVersion)
	}
	if _, err := m.ResolveExe(version); err == nil {
		return fmt.Errorf("版本 %s 已安装", version)
	}

	emit("resolve", 0, 0, "正在解析官网版本")
	releases, err := m.cache.get()
	if err != nil {
		return err
	}
	var rel *SnipasteRelease
	for i := range releases {
		if strings.EqualFold(releases[i].Version, version) {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		return fmt.Errorf("官网版本列表中不存在 %s", version)
	}
	if rel.OfficialHash == "" && rel.Size <= 0 {
		return fmt.Errorf("官网未提供版本 %s 的可验证哈希或文件大小，Hanxi 拒绝安装", version)
	}

	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return err
	}
	tmpZip, err := os.CreateTemp(m.versionsDir, ".snipaste-download-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	tmpZip.Close()
	defer os.Remove(tmpZipPath)

	emit("downloading", 0, rel.Size, "")
	_, err = downloadTo(m.client, rel.AssetURL, tmpZipPath, func(done, total int64) {
		if rel.Size > 0 {
			total = rel.Size
		}
		emit("downloading", done, total, "")
	})
	if err != nil {
		return fmt.Errorf("下载 Snipaste %s 失败: %w", version, err)
	}

	emit("verify-size", 0, rel.Size, "正在校验文件大小")
	actualSize, err := fileSize(tmpZipPath)
	if err != nil {
		return err
	}
	if rel.Size > 0 && actualSize != rel.Size {
		return fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actualSize)
	}
	if rel.Size <= 0 {
		rel.Size = actualSize
	}

	verificationMode := "size+crc+layout"
	if rel.OfficialHash != "" {
		emit("verify-hash", 0, 0, "正在校验官网 "+strings.ToUpper(rel.HashAlgorithm))
		if err := verifyOfficialHash(tmpZipPath, rel.HashAlgorithm, rel.OfficialHash); err != nil {
			return fmt.Errorf("官网哈希校验失败: %w", err)
		}
		verificationMode = "official-" + strings.ToLower(rel.HashAlgorithm) + "+size+crc+layout"
	}
	packageSHA256 := fileSHA256(tmpZipPath)

	stagingDir := filepath.Join(m.versionsDir, dirPrefix+version+fmt.Sprintf(".installing-%d", time.Now().UnixNano()))
	defer os.RemoveAll(stagingDir)
	emit("verify-archive", 0, 0, "正在校验 ZIP 与解压布局")
	// 解包主流程委托内核（恶意 zip 全部安全闸门收口于 artifact.UnpackZip）；
	// "唯一 Snipaste.exe、根或单层包装目录"是 Snipaste 布局策略，留在本包自检。
	if err := artifact.UnpackZip(tmpZipPath, stagingDir, artifact.DefaultLimits, nil); err != nil {
		return err
	}
	installRoot, err := locateInstallRoot(stagingDir)
	if err != nil {
		return err
	}

	exe := filepath.Join(installRoot, exeName)
	actualVersion, err := m.fileVersion(exe)
	if err != nil {
		return fmt.Errorf("读取 Snipaste.exe 版本失败: %w", err)
	}
	if normalizeVersion(actualVersion) != normalizeVersion(version) {
		return fmt.Errorf("文件版本不匹配：期望 %s，实际 %s", version, actualVersion)
	}

	meta := map[string]any{
		"installedAt":      time.Now().Format("2006-01-02 15:04:05"),
		"source":           rel.AssetName,
		"packageSize":      actualSize,
		"packageSHA256":    packageSHA256,
		"officialHash":     rel.OfficialHash,
		"hashAlgorithm":    rel.HashAlgorithm,
		"verificationMode": verificationMode,
	}
	if err := writeJSON(filepath.Join(installRoot, "meta.json"), meta); err != nil {
		return err
	}

	finalDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(finalDir); err == nil {
		return fmt.Errorf("版本 %s 已安装", version)
	}
	emit("install", 0, 0, "正在完成原子安装")
	if installRoot != stagingDir {
		if err := os.Rename(installRoot, finalDir); err != nil {
			return err
		}
	} else if err := os.Rename(stagingDir, finalDir); err != nil {
		return err
	}
	emit("done", 100, 100, "安装完成")
	return nil
}

// ImportLocal 导入用户自备的 Snipaste 免安装目录：以 exe 的 PE FileVersion 定版本号复制建档
// （VerificationMode=local-import，无官网包哈希可比对）。
func (m *Manager) ImportLocal(srcDir string) (SnipasteVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return SnipasteVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}
	version, err := m.fileVersion(srcExe)
	if err != nil || !plainVersionRe.MatchString(normalizeVersion(version)) {
		return SnipasteVersionInfo{}, fmt.Errorf("无法读取可信的 Snipaste 文件版本: %w", err)
	}
	version = normalizeVersion(version)
	finalDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(finalDir); err == nil {
		return SnipasteVersionInfo{}, fmt.Errorf("版本 %s 已安装", version)
	}
	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return SnipasteVersionInfo{}, err
	}
	stagingDir := finalDir + fmt.Sprintf(".installing-%d", time.Now().UnixNano())
	defer os.RemoveAll(stagingDir)
	if err := copyPortableDir(srcDir, stagingDir); err != nil {
		return SnipasteVersionInfo{}, err
	}
	copiedExe := filepath.Join(stagingDir, exeName)
	copiedInfo, err := os.Stat(copiedExe)
	if err != nil || !copiedInfo.Mode().IsRegular() || copiedInfo.Size() == 0 {
		return SnipasteVersionInfo{}, fmt.Errorf("导入目录布局无效：缺少可用的 %s", exeName)
	}
	installedAt := time.Now().Format("2006-01-02 15:04:05")
	packageSHA256 := fileSHA256(copiedExe)
	if err := writeJSON(filepath.Join(stagingDir, "meta.json"), map[string]any{
		"installedAt":      installedAt,
		"isImport":         true,
		"source":           srcDir,
		"packageSHA256":    packageSHA256,
		"verificationMode": "local-import+layout",
	}); err != nil {
		return SnipasteVersionInfo{}, err
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return SnipasteVersionInfo{}, err
	}
	return SnipasteVersionInfo{
		Version: version, ExePath: filepath.Join(finalDir, exeName), Dir: finalDir,
		Size: dirSize(finalDir, copiedInfo.Size()), InstalledAt: installedAt, IsImport: true, Source: srcDir,
		PackageSHA256: packageSHA256, VerificationMode: "local-import+layout",
	}, nil
}

// Remove 先 Rename 到 .removing-<nano> 再递归删除：Windows 下文件被占用时 Rename 也会失败，
// 借此把"进程仍在用"转化为可读错误提示，而不会留下半删目录。
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	removing := dir + fmt.Sprintf(".removing-%d", time.Now().UnixNano())
	if err := os.Rename(dir, removing); err != nil {
		return fmt.Errorf("无法卸载，相关文件可能正在被 Snipaste 使用；请先从原生托盘退出: %w", err)
	}
	if err := os.RemoveAll(removing); err != nil {
		return fmt.Errorf("清理版本目录失败: %w", err)
	}
	return nil
}

// ResolveExe 返回版本目录内主程序路径并验证为非空常规文件；缺失即"安装损坏"。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	exe := filepath.Join(dir, exeName)
	fi, err := os.Stat(exe)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return "", fmt.Errorf("版本 %s 安装损坏：缺少可用的 %s", version, exeName)
	}
	return exe, nil
}

func (m *Manager) resolveVersionDir(version string) (string, error) {
	version = normalizeVersion(version)
	if !plainVersionRe.MatchString(version) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir := filepath.Join(m.versionsDir, dirPrefix+version)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("版本 %s 未安装，请先下载或导入", version)
	}
	return dir, nil
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	return strings.ReplaceAll(version, " ", "")
}

// locateInstallRoot Snipaste 解包布局自检（模块策略，内核不感知）：Snipaste.exe
// 必须唯一存在（常规文件且非空）于 staging 根目录或单层包装目录下，返回实际
// 安装根目录。安全闸门（ZipSlip/链接/炸弹/CRC32/Windows 文件名纪律）已由
// artifact.UnpackZip 收口，此处只做"包内容是否符合 Snipaste 免安装形态"的领域判定。
func locateInstallRoot(stagingDir string) (string, error) {
	var roots []string
	err := filepath.Walk(stagingDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if strings.EqualFold(info.Name(), exeName) && info.Mode().IsRegular() && info.Size() > 0 {
			roots = append(roots, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("扫描解压布局失败: %w", err)
	}
	if len(roots) != 1 {
		return "", fmt.Errorf("ZIP 布局无效：期望唯一的 %s，实际找到 %d 个", exeName, len(roots))
	}
	relRoot, err := filepath.Rel(stagingDir, roots[0])
	if err != nil {
		return "", err
	}
	if relRoot != "." && strings.Contains(relRoot, string(filepath.Separator)) {
		return "", fmt.Errorf("ZIP 布局过深：%s 必须位于根目录或单层包装目录", exeName)
	}
	return roots[0], nil
}

func copyPortableDir(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dstDir, 0755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("导入目录包含不支持的符号链接: %s", path)
		}
		if info.Mode()&os.ModeType != 0 && !info.IsDir() {
			return fmt.Errorf("导入目录包含不支持的特殊文件: %s", path)
		}
		name := strings.ToLower(info.Name())
		if name == "meta.json" || strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, "~") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		return copyFile(path, dst, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// dirSize 版本目录整树字节和：主程序文件只是入口，多文件载荷（Snipaste 免
// 安装版含配置与图像资源）才是体量的主体，单报主 exe 尺寸与真实安装体量级
// 失真。dirstats 度量恒跳过符号链接/重解析点防环；2s 挂钟预算超限或度量
// 失败回退旧口径（主程序文件大小）并 Debug 报账，不谎报全量。
func dirSize(dir string, fallback int64) int64 {
	st := dirstats.MeasureBudgeted(dir, 2*time.Second)
	if st.Err != nil || st.Partial || st.Bytes <= 0 {
		slog.Debug("snipaste 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
