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
	"runtime"
	"sort"
	"strings"
	"time"

	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/versioninfo"
)

const (
	exeName = "LiteMonitor.exe"
	// langAnchorRel 布局自检锚点：官方 zip 恒含中文语言包
	// （settings.json 不在 zip 内——首启按上游默认生成，故不能作锚点）。
	langAnchorRel = "resources" + string(filepath.Separator) + "lang" + string(filepath.Separator) + "zh.json"
	dirPrefix     = "litemonitor_" // 版本隔离目录前缀（与 ccswitch_ / markeron_ 同构）
)

// plainVersionRe 纯版本号（如 1.3.6），用于目录名与 FileVersion 校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本目录名（litemonitor_1.3.6）；imported- 分支收纳
// 版本探测失败的导入（无 FileVersion 资源时）
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// Manager LiteMonitor 版本管理引擎：远程列表、下载完整性校验、
// 嵌套布局解压隔离、本地导入。上游 zip 是单层包装目录布局
// （LiteMonitor_v1.3.6-win-x64/ 包住全部内容），与 ccswitch 平铺布局不同，
// 解压采用 snipaste 的"唯一 exe 定位 installRoot"策略吸收布局漂移。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（长超时）
	fileVersion func(string) (string, error)
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 10 * time.Minute},
		fileVersion: versioninfo.FileVersion,
	}
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]LMRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录。
// exe 缺失/为空或语言包锚点缺失均视为损坏安装跳过（官方 zip 恒含二者）。
func (m *Manager) ListInstalled() ([]LMVersionInfo, error) {
	entries, err := os.ReadDir(m.versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []LMVersionInfo
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
		// 便携套件锚点：语言包缺失 = 目录被误删/半残，不列为可用版本
		if _, statErr := os.Stat(filepath.Join(dir, langAnchorRel)); statErr != nil {
			continue
		}

		name := strings.TrimPrefix(e.Name(), dirPrefix)
		info := LMVersionInfo{Dir: dir, ExePath: exe, Size: fi.Size()}
		if importedDirRe.MatchString(name) {
			info.Version = "v" + name // vimported-时间戳：探测失败兜底（导入安装）
		} else {
			info.Version = "v" + name
			// PE 版本与目录名一致才可信（上游 FileVersion 恒为 X.Y.Z.0 四段，
			// 比对前归一为三段）；不一致 = 目录内容与名不符的损坏安装。
			// imported- 目录名不含版本号，天然不走此分支。
			if runtime.GOOS == "windows" {
				if actual, err := m.fileVersion(exe); err != nil || normalizeFileVersion(actual) != name {
					continue
				}
			}
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
		return versioncmp.Compare(
			strings.TrimPrefix(list[i].Version, "v"),
			strings.TrimPrefix(list[j].Version, "v")) > 0
	})
	return list, nil
}

// Download 下载便携 zip 并解压安装到 versions/litemonitor_X.Y.Z/。
// 上游提供官方 sha256（GitHub API digest），完整性四层兜底：
//  1. 官方 sha256 校验（第一主依据）；
//  2. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改）；
//  3. archive/zip 读取每个 entry 时强制 CRC32 校验（extractAll 读满不提前返回）；
//  4. 提取后布局自检（exe 唯一且 ≤ 单层包装目录 + 语言包锚点）+ PE 版本核对，
//     失败清理暂存目录。
//
// 安装采用 staging 目录 + 原子 rename（snipaste 同构）：中途失败不污染最终目录。
// onProgress 可选：实时上报各阶段进度（下载字节、校验、解压）。
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
	var rel *LMRelease
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

	tmpZip, err := os.CreateTemp("", "hanxi-litemonitor-*.zip")
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

	// 5. 解压保布局安装到暂存目录（zip 内建 CRC32 在此阶段逐 entry 校验）
	emit("extract", 0, 0, "")
	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return err
	}
	stagingDir := filepath.Join(m.versionsDir,
		dirPrefix+strings.TrimPrefix(version, "v")+fmt.Sprintf(".installing-%d", time.Now().UnixNano()))
	defer os.RemoveAll(stagingDir)
	installRoot, err := extractAll(tmpZipPath, stagingDir)
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 6. PE 版本核对（上游 FileVersion 恒为 "X.Y.Z.0"，归一三段比对）
	if runtime.GOOS == "windows" {
		actualVer, verr := m.fileVersion(filepath.Join(installRoot, exeName))
		if verr != nil {
			emit("error", 0, 0, fmt.Sprintf("读取 %s 版本失败: %v", exeName, verr))
			return fmt.Errorf("读取 %s 版本失败: %w", exeName, verr)
		}
		if want := strings.TrimPrefix(version, "v"); normalizeFileVersion(actualVer) != want {
			err := fmt.Errorf("文件版本不匹配：期望 %s，实际 %s", want, actualVer)
			emit("error", 0, 0, err.Error())
			return err
		}
	}

	// 7. 落盘元信息 + 原子搬迁到最终目录
	meta := map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"source":       rel.AssetName,
		"zipSize":      rel.Size,
		"zipSHA256":    fileSHA256(tmpZipPath),
		"assetSHA256":  rel.SHA256,
		"verifiedHash": true,
	}
	if err := writeJSON(filepath.Join(installRoot, "meta.json"), meta); err != nil {
		return err
	}
	finalDir := filepath.Join(m.versionsDir, dirPrefix+strings.TrimPrefix(version, "v"))
	if _, err := os.Stat(finalDir); err == nil {
		return fmt.Errorf("版本 %s 已安装", version)
	}
	emit("install", 0, 0, "")
	if err := os.Rename(installRoot, finalDir); err != nil {
		return err
	}

	emit("done", 100, 100, "")
	return nil
}

// Remove 卸载指定版本（删除隔离目录）。
// 先 rename 到 .removing- 临时名再删：LiteMonitor 运行中会锁 exe，
// rename 失败即给出人性化指引（与 snipaste 同构）。
func (m *Manager) Remove(version string) error {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	removing := dir + fmt.Sprintf(".removing-%d", time.Now().UnixNano())
	if err := os.Rename(dir, removing); err != nil {
		return fmt.Errorf("无法卸载，相关文件可能正在被 LiteMonitor 使用；请先退出后重试: %w", err)
	}
	if err := os.RemoveAll(removing); err != nil {
		return fmt.Errorf("清理版本目录失败: %w", err)
	}
	return nil
}

// ResolveExe 返回指定版本的 LiteMonitor.exe 路径（不存在返回错误）
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

// resolveVersionDir 定位版本隔离目录（litemonitor_X.Y.Z 或 litemonitor_imported-时间戳）
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

// ImportLocal 导入本地已解压的 LiteMonitor 便携套件。
// 与 ccswitch 的单 exe 导入不同：LiteMonitor 的 settings.json/themes/plugins
// 全部随 exe 目录走（AppContext.BaseDirectory 便携语义，源码实证），
// 整套迁移才能保住用户的主题与监控项配置。
// 接受 srcDir 根目录或单层包装目录（官方 zip 解出常带 LiteMonitor_vX.Y.Z-win-x64/）内的套件。
// 调用方需先确保源实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (LMVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	root, err := resolveImportRoot(srcDir)
	if err != nil {
		return LMVersionInfo{}, err
	}
	srcExe := filepath.Join(root, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return LMVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}

	version, vErr := m.fileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(normalizeFileVersion(version)) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 ccswitch ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	} else {
		version = normalizeFileVersion(version)
	}
	finalDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(finalDir); err == nil {
		return LMVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(m.versionsDir, 0755); err != nil {
		return LMVersionInfo{}, err
	}
	stagingDir := finalDir + fmt.Sprintf(".installing-%d", time.Now().UnixNano())
	defer os.RemoveAll(stagingDir)
	if err := copyPortableDir(root, stagingDir); err != nil {
		return LMVersionInfo{}, err
	}
	// 导入自校验：布局锚点齐备（exe + 语言包）
	if _, err := os.Stat(filepath.Join(stagingDir, langAnchorRel)); err != nil {
		return LMVersionInfo{}, fmt.Errorf("导入目录布局无效：缺少 %s（疑似安装版残留或目录不完整）", filepath.ToSlash(langAnchorRel))
	}

	installedAt := time.Now().Format("2006-01-02 15:04:05")
	_ = writeJSON(filepath.Join(stagingDir, "meta.json"), map[string]any{
		"installedAt": installedAt,
		"isImport":    true,
		"source":      root,
	})
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return LMVersionInfo{}, err
	}

	return LMVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(finalDir, exeName),
		Dir:         finalDir,
		Size:        fi.Size(),
		InstalledAt: installedAt,
		IsImport:    true,
		Source:      root,
	}, nil
}

// resolveImportRoot 定位导入源中的套件根：srcDir 根命中优先，
// 其次唯一单层子目录命中（吸收"解压后带一层包装目录"的常见形态）。
func resolveImportRoot(srcDir string) (string, error) {
	if hasExe(srcDir) {
		return srcDir, nil
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return "", fmt.Errorf("源目录不可访问: %s", srcDir)
	}
	var hits []string
	for _, e := range entries {
		if e.IsDir() && hasExe(filepath.Join(srcDir, e.Name())) {
			hits = append(hits, filepath.Join(srcDir, e.Name()))
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	return "", fmt.Errorf("源目录（及其单层子目录）未找到唯一可用的 %s: %s", exeName, srcDir)
}

func hasExe(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// normalizeFileVersion 上游 FileVersion 恒为 "X.Y.Z.0" 四段（csproj 显式设置），
// 归一为与 tag 可比的三段；非规范输入原样返回交由正则校验拒绝。
func normalizeFileVersion(fv string) string {
	fv = strings.TrimSpace(fv)
	if parts := strings.Split(fv, "."); len(parts) == 4 && parts[3] == "0" {
		fv = strings.Join(parts[:3], ".")
	}
	return fv
}

// extractAll 安全解压 zip 到暂存目录，返回实际包含 LiteMonitor.exe 的安装根目录
// （官方 zip 带单层包装目录 LiteMonitor_vX.Y.Z-win-x64/，snipaste 同款吸收策略）。
// 每个 entry 必须读满——completion 路径中的 io.Copy 跑完触发 archive/zip 内建 CRC32 校验。
// 注意：官方 zip 含 GBK 编码中文文件名（"使用说明"类），Go 按原始字节落盘会得到
// 乱码名文件——布局自检只锚定 exe 与语言包，不受其干扰。
func extractAll(zipPath, stagingDir string) (string, error) {
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", err
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	var exeCandidates []string
	for _, entry := range zr.File {
		// ZipSlip 防护：拒绝绝对路径与逃逸出暂存目录的条目
		clean := filepath.Clean(filepath.FromSlash(entry.Name))
		if filepath.IsAbs(clean) || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("zip 含非法路径条目 %q", entry.Name)
		}
		target := filepath.Join(stagingDir, clean)
		rel, err := filepath.Rel(stagingDir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("zip 条目逃逸目标目录 %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("zip 含不支持的符号链接 %q", entry.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}
		rc, err := entry.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			rc.Close()
			return "", err
		}
		// 必须读满：提前返回会跳过 CRC32 校验
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return "", fmt.Errorf("读取 zip 条目 %q 失败: %w", entry.Name, copyErr)
		}
		if closeErr != nil {
			return "", closeErr
		}
		if strings.EqualFold(filepath.Base(target), exeName) {
			fi, err := os.Stat(target)
			if err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
				exeCandidates = append(exeCandidates, target)
			}
		}
	}

	if len(exeCandidates) != 1 {
		return "", fmt.Errorf("zip 布局无效：期望唯一的 %s，实际找到 %d 个", exeName, len(exeCandidates))
	}
	root := filepath.Dir(exeCandidates[0])
	relRoot, err := filepath.Rel(stagingDir, root)
	if err != nil {
		return "", err
	}
	if relRoot != "." && strings.Contains(relRoot, string(filepath.Separator)) {
		return "", fmt.Errorf("zip 布局过深：%s 必须位于根目录或单层包装目录", exeName)
	}
	// 语言包锚点自检（官方 zip 恒含 resources/lang/zh.json）
	if _, err := os.Stat(filepath.Join(root, langAnchorRel)); err != nil {
		return "", fmt.Errorf("zip 布局无效：缺少 %s", filepath.ToSlash(langAnchorRel))
	}
	return root, nil
}

// copyPortableDir 整套复制便携目录（保留相对结构），跳过 Hanxi 侧元信息与上游
// 运行期临时文件（settings.json.tmp/.bak 属上游自管状态，不带入新安装）。
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
		if name == "meta.json" || strings.HasPrefix(name, "settings.json.") || strings.HasPrefix(name, "~") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		return copyFileTo(path, dst)
	})
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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
