package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
)

const (
	exeName        = "Recordly.exe"
	asarRelPath    = "resources" + string(filepath.Separator) + "app.asar" // Electron 主包，布局自检依据
	installDirName = "recordly"                                            // 固定托管目录名（见下方单版本说明）
)

// 单版本安装目录设计（与 ccswitch 的 recordly_X.Y.Z 多版本隔离不同，系上游约束所致）：
// electron-builder NSIS oneClick 安装器启动时按 HKCU 卸载注册表 InstallLocation
// 静默卸载"上一个安装"——多版本共存会让每装一版就抹掉其他版本目录，形同虚设。
// 因此 Recordly 托管收敛为固定 versions/recordly 单目录，"版本管理"语义 =
// 在线安装/覆盖升级 + 导入本地整套目录。切换版本 = 重新安装目标版本。
//
// Manager Recordly 版本管理引擎：远程列表（双通道）、安装器下载与双源校验、
// NSIS 静默安装、本地导入、卸载。
type Manager struct {
	versionsDir string
	client      *http.Client  // 下载客户端（214MB 安装器，长超时）
	desktopDir  func() string // 桌面目录探测（service 注入 plat.DesktopDir；nil 时尽力而为）
}

func NewManager(versionsDir string) *Manager {
	return NewManagerWithDesktop(versionsDir, nil)
}

// NewManagerWithDesktop 允许注入桌面目录探测（快捷方式清理用）。
func NewManagerWithDesktop(versionsDir string, desktopDir func() string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 20 * time.Minute},
		desktopDir:  desktopDir,
	}
}

// InstallDir 托管安装根目录（versions/recordly）。
func (m *Manager) InstallDir() string {
	return filepath.Join(m.versionsDir, installDirName)
}

// ListRemote 获取远程可用版本（includePre=true 时含 beta 通道；10 分钟缓存）。
func (m *Manager) ListRemote(includePre bool) ([]RecordlyRelease, error) {
	return remoteCache.get(includePre)
}

// ListInstalled 扫描托管安装目录（至多一条）。
// Recordly.exe 缺失/为空或 resources/app.asar 缺失均视为损坏安装跳过。
func (m *Manager) ListInstalled() ([]RecordlyVersionInfo, error) {
	dir := m.InstallDir()
	exe := filepath.Join(dir, exeName)
	fi, err := os.Stat(exe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return nil, nil
	}
	if asar, err := os.Stat(filepath.Join(dir, asarRelPath)); err != nil || asar.IsDir() {
		return nil, nil
	}

	info := RecordlyVersionInfo{
		Version: m.resolveInstalledVersion(dir, exe),
		ExePath: exe,
		Dir:     dir,
		Size:    fi.Size(),
	}
	if meta, err := os.ReadFile(filepath.Join(dir, "hanxi-meta.json")); err == nil {
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
	return []RecordlyVersionInfo{info}, nil
}

// resolveInstalledVersion 版本号解析优先级：hanxi-meta.json 记录的远程 tag
// （beta 版本唯一可靠来源——Windows PE FileVersion 无法承载 -beta.N 预发布后缀，
// electron-builder 会将其抹平为 1.3.5）→ PE FileVersion → unknown 时间戳兜底。
func (m *Manager) resolveInstalledVersion(dir, exe string) string {
	if meta, err := os.ReadFile(filepath.Join(dir, "hanxi-meta.json")); err == nil {
		var mm map[string]any
		if json.Unmarshal(meta, &mm) == nil {
			if tag, ok := mm["tag"].(string); ok && tag != "" {
				return tag
			}
		}
	}
	if v, err := versioninfo.FileVersion(exe); err == nil && v != "" {
		return "v" + v
	}
	return "vunknown-" + time.Now().Format("20060102")
}

// Download 下载 NSIS 安装器并静默安装到 versions/recordly/。
// 完整性四层兜底（与 ccswitch 同构，第二/三层因资产是裸 exe 而调整）：
//  1. 官方 sha256 校验（GitHub API digest，第一主依据）；
//  2. 官方 SHA256SUMS.txt 交叉比对（第二只眼，清单缺失时降级为单一官方源）；
//  3. 下载落盘字节数 == release API 声明的 size（防截断/代理篡改）；
//  4. 安装后布局自检（Recordly.exe 非空 + resources/app.asar），失败清理目录。
//
// NSIS oneClick 会顺带静默卸载注册表指向的旧安装：安装前经 foreignInstallLocation
// 卫兵拦截"注册表指向用户自装副本"的场，绝不让托管动作抹掉用户自己的安装。
//
// onProgress 可选：实时上报各阶段进度。
func (m *Manager) Download(version string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析远程资产（缓存内按全量查找，不受当前通道显示过滤影响）
	rel, ok := remoteCache.findRelease(version)
	if !ok {
		// 冷缓存：先拉一轮列表再查
		if _, err := remoteCache.get(true); err != nil {
			emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
			return err
		}
		rel, ok = remoteCache.findRelease(version)
	}
	if !ok {
		err := fmt.Errorf("远程列表不存在版本 %s", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	// 2. 外部安装卫兵：注册表指向托管目录之外的 Recordly 安装时拒绝
	//（继续会让 oneClick 静默卸载用户自装副本，红线）
	if loc, foreign := foreignInstallLocation(m.versionsDir); foreign {
		err := fmt.Errorf("检测到独立安装的 Recordly（%s）。在线安装会覆盖其安装位置，请先在系统「设置-应用」中卸载该版本（配置与录像不受影响），或用「导入本地」将其收编", loc)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpDir, err := os.MkdirTemp("", "hanxi-recordly-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmpInstaller := filepath.Join(tmpDir, rel.AssetName)

	// 3. 下载安装器（直连 + 镜像逐个回退）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(m.client, assetMirrors(version, rel.AssetName), tmpInstaller, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 4. 字节数校验
	actual, err := fileSize(tmpInstaller)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 5. 官方 digest sha256 + SHA256SUMS.txt 交叉比对
	emit("verify", 0, 0, "")
	localSHA := fileSHA256(tmpInstaller)
	if localSHA == "" {
		err := fmt.Errorf("无法读取下载文件")
		emit("error", 0, 0, err.Error())
		return err
	}
	if !strings.EqualFold(localSHA, rel.SHA256) {
		err := fmt.Errorf("sha256 不匹配：期望 %s，实际 %s", rel.SHA256, localSHA)
		emit("error", 0, rel.Size, err.Error())
		return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
	}
	if err := crossCheckSums(m.client, version, rel.AssetName, localSHA); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 6. NSIS 静默安装进托管目录
	emit("install", 0, 0, "NSIS 静默安装中，请勿操作")
	if err := runInstallerSilent(tmpInstaller, m.InstallDir()); err != nil {
		emit("error", 0, 0, fmt.Sprintf("静默安装失败: %v", err))
		return err
	}

	// 7. 布局自检；安装器顺手创建的快捷方式清理
	if err := m.layoutCheck(); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	var desktop string
	if m.desktopDir != nil {
		desktop = m.desktopDir()
	}
	cleanupShortcuts(m.versionsDir, desktop)

	// 8. 落盘元信息（tag 记录 beta 后缀等 PE 版本承载不了的信息）
	meta := map[string]any{
		"installedAt":     time.Now().Format("2006-01-02 15:04:05"),
		"tag":             rel.Version,
		"source":          rel.AssetName,
		"installerSize":   rel.Size,
		"installerSHA256": rel.SHA256,
		"verifiedHash":    true,
	}
	_ = writeJSON(filepath.Join(m.InstallDir(), "hanxi-meta.json"), meta)

	emit("done", 100, 100, "")
	return nil
}

// layoutCheck 安装/导入后的落盘布局自检：exe 非空 + Electron 主包存在。
func (m *Manager) layoutCheck() error {
	dir := m.InstallDir()
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("安装布局无效：缺少可用的 %s", exeName)
	}
	if asar, err := os.Stat(filepath.Join(dir, asarRelPath)); err != nil || asar.IsDir() {
		return fmt.Errorf("安装布局无效：缺少 %s", asarRelPath)
	}
	return nil
}

// Remove 卸载托管版本（删除安装目录）。
// 刻意不调用 Uninstall.exe：卸载注册表键（GUID 派生自 appId）在多次静默安装后
// 指向哪套安装不可靠，跑卸载器存在误伤用户自装副本的风险；RemoveAll 精确
// 作用于自家托管目录，卸载表残留条目与失效快捷方式是无害陈旧数据。
// %APPDATA%\Recordly（配置与录像）不动——与 everything/ccswitch 卸载保数据的先例一致。
func (m *Manager) Remove(version string) error {
	dir := m.InstallDir()
	if _, err := os.Stat(filepath.Join(dir, exeName)); err != nil {
		return fmt.Errorf("版本 %s 未安装或安装目录已缺失", version)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	var desktop string
	if m.desktopDir != nil {
		desktop = m.desktopDir()
	}
	cleanupShortcuts(m.versionsDir, desktop)
	return nil
}

// ResolveExe 返回当前托管安装的 Recordly.exe 路径。
// version 传空 = 接受任何已装版本；非空时与安装目录实际版本不符则报错。
func (m *Manager) ResolveExe(version string) (string, error) {
	installed, err := m.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装 Recordly，请先在下方版本管理在线安装或导入本地副本")
	}
	if version == "" || strings.EqualFold(version, installed[0].Version) {
		return installed[0].ExePath, nil
	}
	// beta tag 与 PE 版本互认（v1.3.5 vs v1.3.5-beta.2）：数值核心一致即接受
	if CompareCore(version, installed[0].Version) == 0 {
		return installed[0].ExePath, nil
	}
	return "", fmt.Errorf("当前托管版本为 %s，请求的 %s 未安装（切换版本请重新在线安装或导入）", installed[0].Version, version)
}

// CompareCore 只比 semver 数值核心（忽略预发布后缀）。
func CompareCore(a, b string) int {
	na, _, okA := splitSemver(a)
	nb, _, okB := splitSemver(b)
	if !okA || !okB {
		return strings.Compare(a, b)
	}
	for i := 0; i < 3; i++ {
		if na[i] != nb[i] {
			if na[i] > nb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// ImportLocal 导入本地已安装的 Recordly（整套 Electron 目录搬迁）。
// Recordly 的数据恒在 %APPDATA%\Recordly（userData），与 exe 位置无关；
// Electron 程序目录内没有用户数据，故整套拷贝即为完整迁移。
// 不支持导入"裸 NSIS 安装器"：oneClick 静默安装会按注册表连带卸载其他安装位置，
// 导入语义下不可控——请用系统安装器正常安装后导入其安装目录。
// 调用方需先确保源实例未运行（Windows 下运行中的 exe 被独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (RecordlyVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return RecordlyVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}
	if asar, err := os.Stat(filepath.Join(srcDir, asarRelPath)); err != nil || asar.IsDir() {
		return RecordlyVersionInfo{}, fmt.Errorf("源目录缺少 %s，不是 Recordly 安装目录: %s", asarRelPath, srcDir)
	}

	target := m.InstallDir()
	if _, err := os.Stat(target); err == nil {
		return RecordlyVersionInfo{}, fmt.Errorf("托管目录已有 Recordly（%s），请先在版本管理卸载再导入",
			m.resolveInstalledVersion(target, filepath.Join(target, exeName)))
	}
	if isUnderDir(target, srcDir) {
		return RecordlyVersionInfo{}, fmt.Errorf("源目录包含 Hanxi 数据目录，拒绝自嵌套导入")
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return RecordlyVersionInfo{}, err
	}
	if err := copyTree(srcDir, target); err != nil {
		_ = os.RemoveAll(target)
		return RecordlyVersionInfo{}, err
	}
	// 排除性自检：拷贝完整性以最关键的布局文件为准
	if err := m.layoutCheck(); err != nil {
		_ = os.RemoveAll(target)
		return RecordlyVersionInfo{}, fmt.Errorf("导入后 %w", err)
	}

	version := "vunknown-" + time.Now().Format("20060102")
	if v, verr := versioninfo.FileVersion(srcExe); verr == nil && v != "" {
		version = "v" + v
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	_ = writeJSON(filepath.Join(target, "hanxi-meta.json"), map[string]any{
		"installedAt": now,
		"isImport":    true,
		"source":      srcDir,
	})

	return RecordlyVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(target, exeName),
		Dir:         target,
		Size:        fi.Size(),
		InstalledAt: now,
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// isUnderDir 判断 path 是否位于 parent 目录内（大小写不敏感，Windows 语义）。
func isUnderDir(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../") && rel != "."
}

// copyTree 递归拷贝目录树（Electron 安装目录数百文件，跳过符号链接防环）。
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			// 安装目录理论无符号链接；保守跳过防环路与越权写
			continue
		case e.IsDir():
			if err := copyTree(s, d); err != nil {
				return err
			}
		default:
			if err := copyFileTo(s, d); err != nil {
				return err
			}
		}
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
