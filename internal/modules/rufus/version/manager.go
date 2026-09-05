package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/versioninfo"
)

const (
	exeName   = "rufus.exe" // 落盘定名（与版本无关）：ResolveExe 确定性寻径、进程名探测稳定
	dirPrefix = "rufus_"    // 版本隔离目录前缀（与 rustdesk_ / litemonitor_ 同构）

	// iniFileName 上游便携模式开关：exe 同目录存在 rufus.ini（哪怕空文件）
	// 即全部设置落 ini 而非注册表（src/rufus.c 实证），托管目录预置见 instance 包。
	iniFileName = "rufus.ini"
)

// plainVersionRe 纯版本号（如 4.15）：Rufus 上游自 1.x 起恒为两段式，用于目录名校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本目录名（rufus_4.15）；imported- 分支收纳探测失败的导入
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// assetVerRe 官方资产文件名中的版本号段（rufus-4.15p.exe → 4.15）
var assetVerRe = regexp.MustCompile(`(?i)^rufus-?v?(\d+\.\d+)`)

// importCandidateRe 导入候选：官方形态单文件 exe（rufus.exe / rufus-4.15p.exe /
// rufus-4.15.exe）；.sig、_x86/_arm64 变体与自造命名（rufus-setup.exe 等）拒收。
var importCandidateRe = regexp.MustCompile(`(?i)^rufus(-\d[\w.]*)?\.exe$`)

// Manager Rufus 版本管理引擎：远程列表、单文件下载校验、隔离目录与本地导入。
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
func (m *Manager) ListRemote() ([]RufusRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。exe 缺失/为空视为损坏安装跳过。
func (m *Manager) ListInstalled() ([]RufusVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []RufusVersionInfo
	for _, e := range entries {
		if !e.IsDir() || !dirNameRe.MatchString(e.Name()) ||
			strings.Contains(e.Name(), ".installing-") || strings.Contains(e.Name(), ".removing-") {
			continue
		}
		dir := filepath.Join(m.versionsDir, e.Name())
		exe := filepath.Join(dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}

		info := RufusVersionInfo{
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
	sort.SliceStable(list, func(i, j int) bool {
		// imported- 兜底目录恒沉底：versioncmp 对非数字段退化字典序，
		// "imported-…" 会被误判为比 "4.x" 更新——冷启动回退"最新已装"绝不能选中它
		ri, rj := importedRank(list[i].Version), importedRank(list[j].Version)
		if ri != rj {
			return ri < rj
		}
		return versioncmp.Compare(
			strings.TrimPrefix(list[i].Version, "v"),
			strings.TrimPrefix(list[j].Version, "v")) > 0
	})
	return list, nil
}

// importedRank 排序权重：正常语义版本 0，imported- 兜底目录 1（沉底）。
func importedRank(version string) int {
	if strings.HasPrefix(version, "vimported-") {
		return 1
	}
	return 0
}

// Download 下载便携单文件 exe 安装到 versions/rufus_X.Y/rufus.exe。
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
	emit("resolve", 0, 0, "")
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *RufusRelease
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

	tmpFile, err := os.CreateTemp("", "hanxi-rufus-*.exe")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	tmpFile.Close()

	// 2. 下载 exe（直连 + 镜像逐个回退）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(rel.Version, rel.AssetName), tmpPath, func(done int64) {
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

// Remove 卸载指定版本（删除隔离目录）。
// 先 rename 到 .removing- 临时名再删：Rufus 运行中会锁 exe，
// rename 失败即给出人性化指引（与 litemonitor 同构）。
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	removing := dir + fmt.Sprintf(".removing-%d", time.Now().UnixNano())
	if err := os.Rename(dir, removing); err != nil {
		return fmt.Errorf("无法卸载，相关文件可能正在被 Rufus 使用；请先退出后重试: %w", err)
	}
	if err := os.RemoveAll(removing); err != nil {
		return fmt.Errorf("清理版本目录失败: %w", err)
	}
	return nil
}

// ResolveExe 返回指定版本的 rufus.exe 路径（不存在返回错误）
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

// resolveVersionDir 定位版本隔离目录（rufus_X.Y 或 rufus_imported-时间戳）
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

// ImportLocal 导入本机已有的 Rufus 便携 exe（文件路径或所在目录均可）。
// 便携模式用户的个性化设置在同目录 rufus.ini 中（源码实证），源旁存在时
// 一并搬运保住配置；运行配置由托管侧 instance.seedPortableSettings 兜底播种。
// 调用方需先确保源实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(path string) (RufusVersionInfo, error) {
	srcExe, srcDir, err := resolveImportExe(path)
	if err != nil {
		return RufusVersionInfo{}, err
	}
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return RufusVersionInfo{}, fmt.Errorf("源文件不可用: %s", srcExe)
	}
	if err := verifyPEMagic(srcExe); err != nil {
		return RufusVersionInfo{}, fmt.Errorf("源文件不是有效的 Windows 可执行体: %w", err)
	}

	version := versionFromImportName(fi.Name())
	if version == "" {
		// 文件名无版本段（用户改名）：读 PE 版本资源；再失败时间戳兜底（与 rustdesk 同构）
		if fv, vErr := versioninfo.FileVersion(srcExe); vErr == nil {
			version = firstTwoSegments(fv)
		}
		if !plainVersionRe.MatchString(version) {
			version = "imported-" + time.Now().Format("20060102-150405")
		}
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return RufusVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return RufusVersionInfo{}, err
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return RufusVersionInfo{}, err
	}
	if err := copyFileTo(srcExe, filepath.Join(targetDir, exeName)); err != nil {
		_ = os.RemoveAll(targetDir)
		return RufusVersionInfo{}, err
	}
	// 源旁便携配置一并搬运（存在即搬，尊重用户已有设置不覆盖不播种）
	hadIni := false
	if _, err := os.Stat(filepath.Join(srcDir, iniFileName)); err == nil {
		if err := copyFileTo(filepath.Join(srcDir, iniFileName), filepath.Join(targetDir, iniFileName)); err == nil {
			hadIni = true
		}
	}

	installedAt := time.Now().Format("2006-01-02 15:04:05")
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": installedAt,
		"isImport":    true,
		"source":      srcDir,
		"copied":      fi.Name(),
		"carriedIni":  hadIni,
		"exeSHA256":   fileSHA256(filepath.Join(targetDir, exeName)),
	})

	return RufusVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: installedAt,
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// resolveImportExe 归一化导入入参：文件路径直接用（须为官方便携形态名）；
// 目录则在官方形态中挑选版本最大的 x64 exe。
// 返回 (exe 路径, exe 所在目录)——后者用于查找随行的 rufus.ini。
func resolveImportExe(path string) (string, string, error) {
	path = strings.TrimSpace(path)
	fi, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("路径不存在或不可访问: %s", path)
	}
	if !fi.IsDir() {
		if !importCandidateRe.MatchString(fi.Name()) {
			return "", "", fmt.Errorf("文件名不符合官方便携版形态（rufus.exe / rufus-版本p.exe）: %s", fi.Name())
		}
		lower := strings.ToLower(fi.Name())
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "_x86") {
			return "", "", fmt.Errorf("不支持非 x64 架构变体: %s", fi.Name())
		}
		return path, filepath.Dir(path), nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", "", err
	}
	var best, bestVer string
	for _, e := range entries {
		if e.IsDir() || !importCandidateRe.MatchString(e.Name()) {
			continue
		}
		lower := strings.ToLower(e.Name())
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "_x86") {
			continue
		}
		ver := versionFromImportName(e.Name())
		if best == "" || (ver != "" && (bestVer == "" || versioncmp.Compare(ver, bestVer) > 0)) ||
			(ver == "" && bestVer == "" && strings.ToLower(e.Name()) > strings.ToLower(best)) {
			best, bestVer = e.Name(), ver
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("目录中未找到官方形态的便携 exe（rufus[-版本].exe）: %s", path)
	}
	return filepath.Join(path, best), path, nil
}

// versionFromImportName 从官方资产文件名提取版本号（无则空串）
func versionFromImportName(name string) string {
	if mm := assetVerRe.FindStringSubmatch(name); len(mm) == 2 && plainVersionRe.MatchString(mm[1]) {
		return mm[1]
	}
	return ""
}

// firstTwoSegments 截取版本号前两段（Rufus FileVersion 为 "X.Y.BUILD.0" 四段，
// 含构建号——与两段式 tag 比对前只取 X.Y）；段非数字时原样返回交由正则校验拒绝。
func firstTwoSegments(fv string) string {
	parts := strings.Split(strings.TrimSpace(fv), ".")
	if len(parts) >= 2 {
		return strings.Join(parts[:2], ".")
	}
	return fv
}

// placeFile 将临时文件落位到目标路径：同卷 rename 原子；跨卷（EXDEV）回退复制。
func placeFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyFileTo(src, dst)
}
