package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
)

const (
	exeName = "LiteMonitor.exe"
	// langAnchorRel 布局自检锚点：官方 zip 恒含中文语言包
	// （settings.json 不在 zip 内——首启按上游默认生成，故不能作锚点）。
	langAnchorRel = "resources" + string(filepath.Separator) + "lang" + string(filepath.Separator) + "zh.json"

	// treeEntryName 版本树目录前缀（<root>/litemonitor_<version>，与历史布局
	// litemonitor_1.3.6 / litemonitor_imported-<时间戳> 一致；无 v 前缀）。
	treeEntryName = "litemonitor"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源）。
	dirPrefix = treeEntryName + "_"
	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 10 分钟口径）。
	fetchBudget = 10 * time.Minute
)

// plainVersionRe 纯版本号（如 1.3.6），用于目录名与 FileVersion 校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager LiteMonitor 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 校验 →
// 解包 → 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch +
// UnpackZip + Tree）；本包只保留 LiteMonitor 领域知识：便携资产筛选、镜像 URL
// 模板、版本目录形状（litemonitor_X.Y.Z / litemonitor_imported-时间戳，无 v
// 前缀）、嵌套布局吸收（官方 zip 带单层包装目录 LiteMonitor_vX.Y.Z-win-x64/，
// snipaste 同款"唯一 exe 定位 installRoot"策略）、双锚点完整性自检（exe +
// 语言包 zh.json）、PE FileVersion 与目录名一致才可信的核账、本地整套便携
// 套件导入（settings/themes/plugins 随 exe 目录走）。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	fetch       fetcher
	mirrors     func(version, assetName string) []string
	fileVersion func(string) (string, error)
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		fetch:       artifact.Fetch,
		mirrors:     assetMirrors,
		fileVersion: versioninfo.FileVersion,
	}
}

// OpenTree 打开 LiteMonitor 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]LMRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 litemonitor_X.Y.Z（下载/导入的正规版本）或
// litemonitor_imported-YYYYMMDD-HHMMSS（版本探测失败的导入兜底）；
// 其余形状的版本令牌不列入。exe 缺失/为空或语言包锚点缺失均视为损坏安装跳过
// （官方 zip 恒含二者）；正规版本目录另须 PE FileVersion 与目录名一致才可信——
// 不一致 = 目录内容与名不符的损坏安装（imported- 目录名不含版本号，天然豁免）。
func (m *Manager) ListInstalled() ([]LMVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []LMVersionInfo
	for _, v := range vers {
		version, ok := versionFromToken(v.Version)
		if !ok {
			continue
		}
		token := strings.TrimPrefix(version, "v")
		exe := filepath.Join(v.Dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}
		// 便携套件锚点：语言包缺失 = 目录被误删/半残，不列为可用版本
		if _, statErr := os.Stat(filepath.Join(v.Dir, langAnchorRel)); statErr != nil {
			continue
		}
		if !importedDirRe.MatchString(token) {
			// PE 版本与目录名一致才可信（上游 FileVersion 恒为 X.Y.Z.0 四段，
			// 比对前归一为三段）。
			if actual, err := m.fileVersion(exe); err != nil || normalizeFileVersion(actual) != token {
				continue
			}
		}

		info := LMVersionInfo{Version: version, Dir: v.Dir, ExePath: exe, Size: fi.Size()}
		// 新下载链走内核统一账本（artifact.Meta，含 schema）；导入链与迁移前
		// 的历史账本走模块自写 map 形态，installedAt 原样展示、isImport/source
		// 仅导入账携带。
		if !v.Meta.InstalledAt.IsZero() {
			info.InstalledAt = v.Meta.InstalledAt.Local().Format("2006-01-02 15:04:05")
		}
		if info.InstalledAt == "" {
			legacyAt, isImport, src := readLegacyMetaFields(v.Dir)
			info.InstalledAt = legacyAt
			if isImport {
				info.IsImport = true
				info.Source = src
			}
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// Download 下载便携 zip 并安装到 versions/litemonitor_X.Y.Z/。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub API 官方资产摘要（digest，
// 已解析进 LMRelease.SHA256）为信任根做流式 + 落盘双 SHA-256 校验，
// Content-Length 与流式上限双核（对齐原"字节数 == release 声明 size"层），
// 镜像只是同摘要的备用传输来源；解包经 artifact.UnpackZip（ZipSlip/炸弹/CRC32
// 全量闸门，取代原 extractAll 的逐条目校验），落位经 Tree.Commit（staging +
// 原子 rename，同版本异摘要防漂移）。官方 zip 含 GBK 编码中文文件名（"使用
// 说明"类），落盘为乱码名文件不影响布局自检（只锚定 exe 与语言包）。
// 模块自留的两道闸在内核链路上如实保留：嵌套布局吸收 + 双锚点自检、
// PE FileVersion 与目录名核对（上游 FileVersion 恒为 "X.Y.Z.0"，归一三段比对）。
// 进度词表与内核链路对齐（ccswitch/markeron 同构）：verify（官方摘要双核）
// 由内核折进 download 阶段、install 折进 done，不造幻影步骤。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// onProgress 可选：实时上报各阶段进度（下载字节、解压落位）。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应的远程资产（模块知识：GitHub 元数据与资产筛选）
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
	// 官方摘要信任根：GitHub release API 的 asset.digest（parseReleasesBody
	// 已剥前缀入 LMRelease.SHA256，无摘要的 release 根本不进列表）。缺失一律
	// 拒装，不做无校验安装。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 LiteMonitor %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
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

	// 2. 受控下载（主址 + 镜像逐个回退；下载/校验/字节数双核全部委托内核）
	urls := m.mirrors(version, rel.AssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   rel.SHA256,
		MaxBytes: rel.Size, // 与 release API 声明大小对齐：超限即断，杜绝异常放大
		FileName: rel.AssetName,
	}
	emit("downloading", 0, rel.Size, "")
	fetchErr := m.fetch(context.Background(), src, tmpZipPath, func(p artifact.Progress) {
		// 内核进度 → 既有词表：只有流式下载阶段对应 downloading，其余阶段本模块不上报
		if p.Stage == artifact.StageDownload {
			emit("downloading", p.Done, p.Total, "")
		}
	}, fetchBudget)
	if fetchErr != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", fetchErr))
		return fetchErr
	}

	// 3. 解包进独占中转目录（staging 与最终目录同卷，供原子落位；
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）
	token := strings.TrimPrefix(version, "v")
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	defer discard() // 成功 Commit 后为 no-op；任一步失败不留半件

	emit("extract", 0, 0, "")
	if err := artifact.UnpackZip(tmpZipPath, staging, artifact.DefaultLimits, nil); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}

	// 4. 嵌套布局吸收 + 双锚点自检（模块策略，内核不感知）：定位套件根
	installRoot, err := resolveStagedRoot(staging)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}

	// 5. PE 版本核对（上游 FileVersion 恒为 "X.Y.Z.0"，归一三段比对）
	actualVer, verr := m.fileVersion(filepath.Join(installRoot, exeName))
	if verr != nil {
		emit("error", 0, 0, fmt.Sprintf("读取 %s 版本失败: %v", exeName, verr))
		return fmt.Errorf("读取 %s 版本失败: %w", exeName, verr)
	}
	if normalizeFileVersion(actualVer) != token {
		err := fmt.Errorf("文件版本不匹配：期望 %s，实际 %s", token, actualVer)
		emit("error", 0, 0, err.Error())
		return err
	}
	// exe 诊断摘要入账（避免每次扫描现场哈希；LMVersionInfo 不展示，纯账本诊断）
	assetSHA, _ := fileSHA256(filepath.Join(installRoot, exeName))

	// 6. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移）
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   rel.SHA256,
		AssetSHA256: assetSHA,
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(installRoot, token, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("落位失败: %v", err))
		return err
	}

	emit("done", 100, 100, "")
	return nil
}

// Remove 卸载指定版本（委托 Tree：rename 隔离后删除，文件占用时留下可恢复状态）。
// LiteMonitor 运行中会锁 exe，rename 失败即由内核给出"先退出"人话指引。
func (m *Manager) Remove(version string) error {
	_, token, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return m.tree.Remove(token, nil)
}

// ResolveExe 返回指定版本的 LiteMonitor.exe 路径（不存在或损坏返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
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

// resolveVersionDir 定位版本隔离目录（litemonitor_X.Y.Z 或 litemonitor_imported-时间戳）。
// 形状外令牌（含路径穿越）先于任何磁盘访问被拒，错误口径与原实现一致。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	token = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(token) && !importedDirRe.MatchString(token) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	if d, rerr := m.tree.Resolve(token); rerr == nil {
		return d, token, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// versionFromToken 把 Tree 扫出的版本令牌规范化为 vX.Y.Z / vimported-<时间戳>
// （1.3.6 → v1.3.6，与原目录名剥离前缀的口径一致）。仅接受纯 x.y.z 或
// imported-时间戳形状，litemonitor_vX.Y.Z 之类带 v 前缀的历史/外来目录名不列入。
func versionFromToken(token string) (string, bool) {
	if plainVersionRe.MatchString(token) || importedDirRe.MatchString(token) {
		return "v" + token, true
	}
	return "", false
}

// resolveStagedRoot 在解包后的中转目录内定位套件根（吸收官方 zip 的布局漂移）：
// staging 根命中优先，其次唯一单层子目录命中（官方 zip 恒带包装目录
// LiteMonitor_vX.Y.Z-win-x64/，snipaste 同款策略）；双层以下视为布局过深拒收。
func resolveStagedRoot(staging string) (string, error) {
	var candidates []string
	if hasExe(staging) {
		candidates = append(candidates, staging)
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", fmt.Errorf("读取中转目录失败: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(staging, e.Name())
		if hasExe(dir) {
			candidates = append(candidates, dir)
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("zip 布局过深或缺失：%s 必须位于根目录或单层包装目录", exeName)
	case 1:
	default:
		return "", fmt.Errorf("zip 布局无效：期望唯一的 %s，实际找到 %d 个", exeName, len(candidates))
	}
	// 语言包锚点自检（官方 zip 恒含 resources/lang/zh.json）
	if _, err := os.Stat(filepath.Join(candidates[0], langAnchorRel)); err != nil {
		return "", fmt.Errorf("zip 布局无效：缺少 %s", filepath.ToSlash(langAnchorRel))
	}
	return candidates[0], nil
}

// readLegacyMetaFields 读取模块自写的导入账本与迁移前历史账本（无 schema 的
// map 形态）：installedAt 原样字符串、isImport 布尔、source 导入来源目录。
// 新下载链的 artifact.Meta 账本（含 schema）不走本函数（InstalledAt 由内核
// 解析、source 为 "remote" 展示语义已由 isImport 标志承载）。
func readLegacyMetaFields(dir string) (installedAt string, isImport bool, source string) {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return "", false, ""
	}
	var mm map[string]any
	if json.Unmarshal(raw, &mm) != nil {
		return "", false, ""
	}
	installedAt, _ = mm["installedAt"].(string)
	isImport, _ = mm["isImport"].(bool)
	source, _ = mm["source"].(string)
	return installedAt, isImport, source
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

// ---------- 本地导入（LiteMonitor 领域流程，无内核对应物） ----------

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

// fileSHA256 计算文件全量 SHA-256（十六进制小写）。
// 注意：仅用于诊断展示链（exe 自哈希入账），不参与下载校验主流程
// （下载完整性已由内核 artifact.Fetch 的官方摘要双核收口）。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
