package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/packages/go/artifact"
)

const (
	exeName       = "Bili23.exe"          // 入口（Python-Static 静态解释器改壳，GUI 子系统，进程常驻）
	bootstrapName = "_pystand_static.int" // 启动引导脚本（与 exe 同级，缺失即布局损坏）
	scriptMainRel = "script/main.py"      // 应用主模块相对路径（布局自检第三锚点）
	topDirName    = "Bili23-Downloader"   // 官方 zip 的顶层单目录名（收割展平的对象）

	// treeEntryName 版本树目录前缀（<root>/bili23_<version>，与历史布局
	// bili23_2.15.0 / bili23_imported-<时间戳> 一致）。
	treeEntryName = "bili23"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源）。
	dirPrefix = treeEntryName + "_"
	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 15 分钟口径，便携包 ~43MB，镜像网络给足余量）。
	fetchBudget = 15 * time.Minute
)

// plainVersionRe 纯版本号（如 2.15.0 / 2.00.7——允许上游的前导零变体），用于目录名与导入探测校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// appVersionRe 导入版本探测：上游把版本号硬编码在 script/util/common/config.py 的
// Config 类属性 app_version = "2.15.0"。Bili23.exe 是 pythonw 改名壳，PE FileVersion
// 恒为 Python 版本号，不可用作应用版本依据（与 ccswitch 单 exe 导入的关键差异）。
var appVersionRe = regexp.MustCompile(`app_version\s*=\s*["'](\d+\.\d+\.\d+)["']`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager Bili23 Downloader 版本管理引擎：远程列表（GitHub 元数据）与
// "下载 → 校验 → 解包 → 落位"主流程委托 Wave 4 共享内核 packages/go/artifact
// （Fetch + UnpackZip + Tree）——官方摘要事实核查（2026-09-19）：上游
// Bili23-Downloader 各版 windows_x64_portable.zip 资产全量携带 GitHub
// asset.digest（SHA-256），无需走 snipaste/guoheview 的弱摘要薄适配器路线，
// 下载校验段可整体委托内核。本包只保留 Bili23 领域知识：便携资产筛选、
// 镜像 URL 模板、版本目录形状（x.y.z 含前导零变体 / imported-时间戳）、
// 顶层 Bili23-Downloader/ 包装目录收割、三锚点（Bili23.exe +
// _pystand_static.int + script/main.py）布局自检、导入版本探测
// （PE 版本不可信、版本藏 config.py）与既有进度词表映射。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	fetch   fetcher
	mirrors func(version, assetName string) []string
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		fetch:       artifact.Fetch,
		mirrors:     assetMirrors,
	}
}

// OpenTree 打开 Bili23 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]Bili23Release, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 bili23_2.15.0 / bili23_2.00.7（下载/导入的正规版本）或
// bili23_imported-YYYYMMDD-HHMMSS（版本探测失败的导入兜底）；其余形状的
// 版本令牌不列入。布局三锚点（Bili23.exe 非空 + _pystand_static.int +
// script/main.py）任一缺失均视为损坏安装跳过——本应用是"运行时+源码"
// 目录形态，缺件即无法启动。
func (m *Manager) ListInstalled() ([]Bili23VersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []Bili23VersionInfo
	for _, v := range vers {
		version, ok := versionFromToken(v.Version)
		if !ok {
			continue
		}
		dir := v.Dir
		if !verifyLayout(dir) {
			continue
		}
		exe := filepath.Join(dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil {
			continue
		}

		info := Bili23VersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     dir,
			Size:    dirSize(dir),
		}
		// 新下载链走内核统一账本（artifact.Meta，含 schema）；导入链与迁移前
		// 的历史账本走模块自写 map 形态，installedAt 原样展示、isImport/source
		// 仅导入账携带。
		if !v.Meta.InstalledAt.IsZero() {
			info.InstalledAt = v.Meta.InstalledAt.Local().Format("2006-01-02 15:04:05")
		}
		if info.InstalledAt == "" {
			legacyAt, isImport, src := readLegacyMetaFields(dir)
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

// Download 下载便携 zip 并安装到 versions/bili23_X.Y.Z/。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub API 官方资产摘要（digest，
// 已解析进 Bili23Release.SHA256，无摘要的 release 根本不进列表）为信任根做
// 流式 + 落盘双 SHA-256 校验，Content-Length 与流式上限双核（对齐原
// "字节数 == release 声明 size"层），镜像只是同摘要的备用传输来源；解包经
// artifact.UnpackZip（ZipSlip/炸弹/CRC32 全量闸门，取代原 extractAll），落位
// 经 Tree.Commit（staging + 原子 rename，同版本异摘要防漂移——原实现直写
// 最终目录，半件即污染安装）。
// 进度回调沿用本模块既有词表（downloading/verify/extract/done/error）：
// verify（官方摘要双核）由内核 Fetch 在流式+落盘复核完成后上报，映射进
// 既有词汇保持前端展示段零漂移。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// onProgress 可选：实时上报各阶段进度（下载字节、校验、解压落位）。
// Download 保留旧调用面，供版本包单测与非事务调用使用。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装，可由事务 context 取消（P0 批 2b 生命周期）。
func (m *Manager) DownloadContext(ctx context.Context, txnID, version string, onProgress func(p DownloadProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
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
	var rel *Bili23Release
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
	// 官方摘要信任根缺失一律拒装，不做无校验安装。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 Bili23 Downloader %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-bili23-*.zip")
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
	fetchErr := m.fetch(ctx, src, tmpZipPath, func(p artifact.Progress) {
		// 内核进度 → 既有词表：流式下载对应 downloading，摘要双核通过对应 verify
		switch p.Stage {
		case artifact.StageDownload:
			emit("downloading", p.Done, p.Total, "")
		case artifact.StageVerify:
			emit("verify", 0, 0, "")
		}
	}, fetchBudget)
	if fetchErr != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", fetchErr))
		return fetchErr
	}

	// 3. 解包进独占中转目录（staging 与最终目录同卷，供原子落位；
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）
	if err := ctx.Err(); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	token := strings.TrimPrefix(version, "v")
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	defer discard() // 成功 Commit 后为 no-op；任一步失败不留半件

	emit("extract", 0, 0, "")
	if err := artifact.UnpackZipContext(ctx, tmpZipPath, staging, artifact.DefaultLimits, nil); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}
	// 4. 顶层包装目录收割 + 三锚点布局自检（Bili23 领域策略，内核不感知，
	// Commit 前收口）：官方 zip 顶层是 Bili23-Downloader/ 单目录，必须展平
	// 使三锚点落在隔离目录根；上游若改为扁平布局自然兼容（收割为 no-op）。
	if err := harvestPayloadRoot(staging); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	if !verifyLayout(staging) {
		err := fmt.Errorf("zip 布局无效：缺少 %s / %s / %s 三锚点", exeName, bootstrapName, scriptMainRel)
		emit("error", 0, 0, err.Error())
		return err
	}
	// exe 诊断摘要入账（避免每次扫描现场哈希；Bili23VersionInfo 不展示，纯账本诊断）
	assetSHA := fileSHA256(filepath.Join(staging, exeName))

	// 5. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移）
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   rel.SHA256,
		AssetSHA256: assetSHA,
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("落位失败: %v", err))
		return err
	}

	emit("done", 100, 100, "")
	return nil
}

// Remove 卸载指定版本（委托 Tree：rename 隔离后删除，文件占用时留下可恢复状态）
func (m *Manager) Remove(version string) error {
	_, token, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return m.tree.Remove(token, nil)
}

// ResolveExe 返回指定版本的 Bili23.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（bili23_X.Y.Z 或 bili23_imported-时间戳）。
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

// versionFromToken 把 Tree 扫出的版本令牌规范化为 vX.Y.Z 展示形式
// （2.15.0 → v2.15.0、2.00.7 → v2.00.7，与原目录名剥前缀加 v 的口径一致）。
// 仅接受纯 x.y.z（含前导零变体）或 imported-时间戳形状，
// bili23_v2.15.0 之类带 v 前缀的历史/外来目录名不列入。
func versionFromToken(token string) (string, bool) {
	if plainVersionRe.MatchString(token) || importedDirRe.MatchString(token) {
		return "v" + token, true
	}
	return "", false
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

// ---------- 布局收割与导入探测（Bili23 领域判定，内核不感知） ----------

// harvestPayloadRoot 收割顶层包装目录（Bili23 布局策略，Commit 前收口）：
// 官方便携 zip 顶层是 Bili23-Downloader/ 单目录（7z 在 PowerShell 下通配符
// 未展开所致，实测 v2.10.0–v2.15.0 稳定如此），原样保留会深一层、破坏
// "版本目录即安装目录"的全家族布局（ResolveExe / 三锚点约定都在根上）。
// 以唯一 Bili23.exe 所在目录为 payload 根，把根内内容平铺进 staging 根、
// 根外杂质一概不带入；zip 已是扁平布局（exe 就在 staging 根）时为 no-op，
// 上游某天改扁平天然兼容。
func harvestPayloadRoot(staging string) error {
	root, err := locatePayloadRoot(staging)
	if err != nil {
		return err
	}
	if root == staging {
		return nil // 扁平布局：根即目录本身
	}

	// staging 下通往 payload 根的第一段祖先是"保留链"；其余顶层 entry 为根外杂质
	rel, err := filepath.Rel(staging, root)
	if err != nil {
		return err
	}
	top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	entries, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("读取中转目录失败: %w", err)
	}
	for _, e := range entries {
		if e.Name() == top {
			continue
		}
		if err := os.RemoveAll(filepath.Join(staging, e.Name())); err != nil {
			return fmt.Errorf("清除根外杂质失败: %w", err)
		}
	}

	// payload 根内容平铺到 staging 根，随后移除包装链（含链上残留杂质）
	items, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("读取便携根失败: %w", err)
	}
	for _, it := range items {
		if err := os.Rename(filepath.Join(root, it.Name()), filepath.Join(staging, it.Name())); err != nil {
			return fmt.Errorf("收割顶层目录失败: %w", err)
		}
	}
	if err := os.RemoveAll(filepath.Join(staging, top)); err != nil {
		return fmt.Errorf("清理包装目录失败: %w", err)
	}

	// 复验收割结果：exe 落回 staging 根且非空
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	}
	return nil
}

// locatePayloadRoot 在 staging 内定位唯一的 Bili23.exe（常规文件且非空），
// 返回其所在目录作为 payload 根；零个或多个都无法确立"版本目录即安装目录"布局。
func locatePayloadRoot(staging string) (string, error) {
	var roots []string
	err := filepath.WalkDir(staging, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(d.Name(), exeName) {
			return nil
		}
		if fi, serr := d.Info(); serr == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			roots = append(roots, filepath.Dir(p))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("扫描解压布局失败: %w", err)
	}
	switch len(roots) {
	case 1:
		return roots[0], nil
	case 0:
		return "", fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	default:
		return "", fmt.Errorf("zip 布局无效：找到 %d 个 %s，无法判定 payload 根", len(roots), exeName)
	}
}

// verifyLayout 安装布局三锚点校验（目录形态应用：缺任一件即无法启动）。
func verifyLayout(dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, exeName)); err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, bootstrapName)); err != nil || fi.IsDir() {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(scriptMainRel))); err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	return true
}

// ---------- 本地导入（Bili23 领域流程，无内核对应物） ----------

// ImportLocal 导入本地已有的 Bili23 Downloader（安装版目录 / 手动解压的便携目录均可）。
// 与 ccswitch 的单 exe 导入不同：本应用是"静态 Python 运行时 + 源码"整目录形态，
// 导入单元就是整个安装目录（复制 ~108MB）。用户配置恒在 %APPDATA%\Bili23 Downloader\，
// 与安装位置无关，不随导入迁移。
// 版本号从 script/util/common/config.py 的 app_version 常量解析（exe 是 pythonw 改名壳，
// PE FileVersion 不可信）。调用方需先确保源实例未运行。
func (m *Manager) ImportLocal(srcDir string) (Bili23VersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	// 容错：用户选中"解压出来的外层目录"时自动下钻一层官方顶层目录
	if !verifyLayout(srcDir) {
		nested := filepath.Join(srcDir, topDirName)
		if verifyLayout(nested) {
			srcDir = nested
		} else {
			return Bili23VersionInfo{}, fmt.Errorf("源目录不是有效的 Bili23 Downloader 安装（缺少 %s / %s / %s）: %s",
				exeName, bootstrapName, scriptMainRel, srcDir)
		}
	}
	// 防呆：不允许从 Hanxi 自己的托管目录导入（自我复制制造孤儿目录）
	if absVersions, err := filepath.Abs(m.versionsDir); err == nil {
		if absSrc, err := filepath.Abs(srcDir); err == nil &&
			strings.EqualFold(filepath.Dir(absSrc), absVersions) {
			return Bili23VersionInfo{}, fmt.Errorf("该版本已由 Hanxi 托管，无需导入")
		}
	}

	version := detectAppVersion(srcDir)
	if !plainVersionRe.MatchString(version) {
		// 版本探测失败（config.py 缺失/格式变更）：时间戳兜底，与 ccswitch ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return Bili23VersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}

	// 复制前最后一道竞态防御：锚点校验与复制之间 exe 可能被移走
	if _, err := os.Stat(filepath.Join(srcDir, exeName)); err != nil {
		return Bili23VersionInfo{}, err
	}

	if err := copyTree(srcDir, targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return Bili23VersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      "整个安装目录（运行时 + script + site-packages + bundle）",
	})

	return Bili23VersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        dirSize(targetDir),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// detectAppVersion 从 script/util/common/config.py 解析 app_version 常量（失败返回空串）。
func detectAppVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "script", "util", "common", "config.py"))
	if err != nil {
		return ""
	}
	if m := appVersionRe.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

// dirSize 目录总字节数（展示用；个别文件竞态缺失不计为错误）。
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // 遍历展示尺寸，竞态错误静默跳过
		}
		if fi, ierr := d.Info(); ierr == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// copyTree 递归复制目录（保相对布局）。拒绝非常规文件（符号链接等）防目录逃逸与死链复制。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("安装目录含非常规文件，无法导入: %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFileTo(path, target)
	})
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
