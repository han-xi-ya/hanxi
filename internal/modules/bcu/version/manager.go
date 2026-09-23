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
	exeName   = "BCUninstaller.exe"
	dirPrefix = treeEntryName + "_" // 版本隔离目录前缀（与 frp_v0.61.1 / everything_v1.5.0 同构）

	// treeEntryName 版本树目录前缀（<root>/bcu_<version>，与历史布局
	// bcu_6.2.0 / bcu_imported-<时间戳> 一致）。
	treeEntryName = "bcu"
	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 15 分钟口径：
	// 自包含便携包约 76MB，体量大于 markeron/ccswitch 的 10 分钟档）。
	fetchBudget = 15 * time.Minute
)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// listingTokenRe ListInstalled 收纳的版本令牌形状（原 dirNameRe 非导入分支：
// 数字起头的点/字母数字串，如 6.2.0 / 6.1.0.1）。
var listingTokenRe = regexp.MustCompile(`^[0-9][0-9a-zA-Z.]+$`)

// settingsName BCU 便携版的设置文件（与 exe 同目录），导入/整部迁移时一并携带。
const settingsName = "BCUninstaller.settings"

// innerExeRel 真身实例相对版本根的路径。**托管启动/唤窗必须直指内层**：
// 外层 BCUninstaller.exe（约 350KB）只是官方接力启动器（bootstrapper），
// 执行后拉起 win-x64\BCUninstaller.exe 并在约 20ms 内自退（2026-09-06 真机
// 实录：Hanxi 拉起的外层秒退 + 幸存内层持有单实例互斥体 → wait 退出分类按
// "我方进程已死+互斥体活着"规则把自家实例误判成 external，托管能力全失——
// TROUBLESHOOTING #24 rustdesk 接力家族同款上游怪癖，内核 supervisor.wait 的
// external-takeover 分支同样会踩中，锚点契约迁移后原样保持）。
// 直启内层不改变便携设置落点：工作目录钉 exe 所在目录（内核默认惯例），
// 真身退出将 settings 写回目录（实证 win-x64 内无分家文件）。老布局缺失
// 内层时回退外层兜底。
var innerExeRel = filepath.Join("win-x64", exeName)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager BCU 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 校验 →
// 解包 → 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch +
// UnpackZip + Tree）；本包只保留 BCU 领域知识：双变体（portable/fdd）资产
// 选择、镜像 URL 模板（tag 与版本不同形）、版本目录形状（3~4 段 /
// imported-时间戳）、win-x64 双层布局自检与内层真身账本锚点、本地导入
// 黑名单整搬与既有进度词表映射。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	fetch   fetcher
	mirrors func(tag, assetName string) []string
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

// OpenTree 打开 BCU 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]BCURelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 bcu_X.Y.Z(.W)（下载/导入的正规版本）或
// bcu_imported-YYYYMMDD-HHMMSS（版本探测失败的导入兜底）；其余形状的版本
// 令牌不列入；外层 exe 缺失/为空视为损坏安装跳过（settings 缺失属首启未
// 配置，正常）。展示 ExePath 与启动共用同一解析结果：win-x64 真身优先
// （见 innerExeRel），Size 保持外层启动器口径（迁移前后展示值不变）。
func (m *Manager) ListInstalled() ([]BCUVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []BCUVersionInfo
	for _, v := range vers {
		version, ok := versionFromToken(v.Version)
		if !ok {
			continue
		}
		exe := filepath.Join(v.Dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() {
			continue
		}
		if !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}

		info := BCUVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    fi.Size(),
		}
		if inner := filepath.Join(v.Dir, innerExeRel); isRegularFile(inner) {
			info.ExePath = inner // 真身优先（见 innerExeRel）
		}
		// 新下载链走内核统一账本（artifact.Meta，含 schema）；导入链与迁移前
		// 的历史账本走模块自写 map 形态，installedAt 原样展示、isImport/source
		// 携带导入来源与历史下载资产名（原实现即无条件读 source）。
		if !v.Meta.InstalledAt.IsZero() {
			info.InstalledAt = v.Meta.InstalledAt.Local().Format("2006-01-02 15:04:05")
		}
		if info.InstalledAt == "" {
			legacyAt, isImport, src := readLegacyMetaFields(v.Dir)
			info.InstalledAt = legacyAt
			info.IsImport = isImport
			info.Source = src
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// Download 下载指定变体的 zip 并安装到 versions/bcu_X.Y.Z/。
// variant 取值 VariantPortable（自包含）/ VariantFdd（框架依赖精简版）。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub API 官方资产摘要（digest，
// 已解析进 BCURelease.SHA256 / FddSHA256）为信任根做流式 + 落盘双 SHA-256
// 校验，Content-Length 与流式上限双核（取代原"字节数 == release 声明 size"
// 层），镜像只是同摘要的备用传输来源；解包经 artifact.UnpackZip（ZipSlip/
// 炸弹/CRC32 全量闸门，取代原 extractAll），落位经 Tree.Commit（staging +
// 原子 rename，同版本异摘要防漂移——原实现直写最终目录，半件即污染安装）。
// 进度回调沿用本模块既有词表（downloading/extract/done/error）：verify
// （官方摘要双核）由内核折进 download 阶段，不造幻影步骤。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// onProgress 可选：实时上报各阶段进度（下载字节、解压落位）。
// Download 保留旧调用面，供版本包单测与非事务调用使用。
func (m *Manager) Download(txnID, version, variant string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, variant, onProgress)
}

// DownloadContext 下载并安装，可由事务 context 取消（P0 批 2b 生命周期）。
func (m *Manager) DownloadContext(ctx context.Context, txnID, version, variant string, onProgress func(p DownloadProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if variant == "" {
		variant = VariantPortable
	}
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Variant: variant, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应变体的远程资产（模块知识：GitHub 元数据与双变体筛选）
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *BCURelease
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
	name, size, sha := rel.AssetName, rel.Size, rel.SHA256
	if variant == VariantFdd {
		if rel.FddName == "" {
			err := fmt.Errorf("版本 %s 无框架依赖变体（可能未附带或缺失官方哈希）", version)
			emit("error", 0, 0, err.Error())
			return err
		}
		name, size, sha = rel.FddName, rel.FddSize, rel.FddSHA256
	} else if variant != VariantPortable {
		emit("error", 0, 0, fmt.Sprintf("未知变体: %s", variant))
		return fmt.Errorf("未知变体: %s", variant)
	}
	// 官方摘要信任根：GitHub release API 的 asset.digest（parseReleasesBody
	// 已剥前缀入账，无摘要的资产根本不进列表/变体）。缺失一律拒装，不做
	// 无校验安装。
	if sha == "" {
		err := fmt.Errorf("上游未提供 BCU %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-bcu-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 受控下载（主址 + 镜像逐个回退；下载/校验/字节数双核全部委托内核；
	// tag 与资产版本不同形，用 release 自带 tag 拼路径）
	urls := m.mirrors(rel.Tag, name)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   sha,
		MaxBytes: size, // 与 release API 声明大小对齐：超限即断，杜绝异常放大
		FileName: name,
	}
	emit("downloading", 0, size, "")
	fetchErr := m.fetch(ctx, src, tmpZipPath, func(p artifact.Progress) {
		// 内核进度 → 既有词表：只有流式下载阶段对应 downloading，其余阶段本模块不上报
		if p.Stage == artifact.StageDownload {
			emit("downloading", p.Done, p.Total, "")
		}
	}, fetchBudget)
	if fetchErr != nil {
		emit("error", 0, size, fmt.Sprintf("下载失败: %v", fetchErr))
		return fetchErr
	}

	// 3. 解包进独占中转目录（staging 与最终目录同卷，供原子落位；
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）
	if err := ctx.Err(); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	token := strings.TrimSpace(version)
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
	// 4. 布局自检（模块策略，内核不感知）：外层启动器存在且非空
	// （win-x64 子层布局的账本锚点由 entryRel 收口，见 innerExeRel）
	if err := checkLayout(staging); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	// 真身诊断摘要入账（避免每次扫描现场哈希；BCUVersionInfo 不展示，纯账本诊断）
	entry := entryRel(staging)
	assetSHA, _ := fileSHA256(filepath.Join(staging, entry))

	// 5. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移）。meta.Entry 记录**内层真身相对路径**
	// （锚点契约与 ResolveExe 同口径：账本与启动都指向 win-x64 真身，外层
	// bootstrapper 只作布局存在性自检，绝不进托管生命周期视野）。
	meta := artifact.Meta{
		Entry:       filepath.ToSlash(entry),
		ZipSHA256:   sha,
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

// ResolveExe 返回指定版本用于启动/唤窗的可执行路径：优先 win-x64 真身、
// 回退外层启动器（见 innerExeRel 注释——外层次次接力自退，不可当生命周期
// 锚点）；两者皆缺返回错误。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	if inner := filepath.Join(dir, innerExeRel); isRegularFile(inner) {
		return inner, nil
	}
	if outer := filepath.Join(dir, exeName); isRegularFile(outer) {
		return outer, nil
	}
	return "", fmt.Errorf("版本 %s 未找到可用可执行文件（%s / %s 均缺失）", version, innerExeRel, exeName)
}

// resolveVersionDir 定位版本隔离目录（bcu_X.Y.Z(.W) 或 bcu_imported-时间戳）。
// 形状外令牌（含路径穿越）先于任何磁盘访问被拒，错误口径与原实现一致。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	token = strings.TrimSpace(version)
	if !plainVersionRe.MatchString(token) && !importedDirRe.MatchString(token) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	if d, rerr := m.tree.Resolve(token); rerr == nil {
		return d, token, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// versionFromToken 把 Tree 扫出的版本令牌原样规范化为展示版本号（6.2.0 →
// 6.2.0，BCU 无 v 前缀惯例，与远程列表口径一致）。仅收纳原 dirNameRe 的
// 两种形状（数字起头 x.y.z(.w) / imported-时间戳），其余目录名不列入。
func versionFromToken(token string) (string, bool) {
	if listingTokenRe.MatchString(token) || importedDirRe.MatchString(token) {
		return token, true
	}
	return "", false
}

// checkLayout zip 布局自检（模块策略）：staging 内 BCUninstaller.exe 存在且
// 非空。不符即判定安装无效，调用方丢弃 staging。（win-x64 子层可选：
// fdd 精简包等无子层布局回退外层启动器即真身，见 entryRel。）
func checkLayout(staging string) error {
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	}
	return nil
}

// entryRel 定位 staging 目录内的真身相对路径（相对版本根）：win-x64 真身
// 优先、缺失回退外层启动器。与 ResolveExe 同口径——账本锚点（meta.Entry）
// 与实际被托管启动的文件必须一致，否则诊断摘要对不上被启动的实体。
func entryRel(dir string) string {
	if isRegularFile(filepath.Join(dir, innerExeRel)) {
		return innerExeRel
	}
	return exeName
}

// isRegularFile 存在且为常规文件（非目录、可读实体）
func isRegularFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// readLegacyMetaFields 读取模块自写的导入账本与迁移前历史账本（无 schema 的
// map 形态）：installedAt 原样字符串、isImport 布尔、source（导入来源目录或
// 历史下载账本里的资产名——原实现即无条件展示）。新下载链的 artifact.Meta
// 账本（含 schema）不走本函数（InstalledAt 由内核解析、展示语义已由
// isImport 标志承载）。
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

// ---------- 本地导入（BCU 领域流程，无内核对应物） ----------

// ImportLocal 导入本地已有的 BCU 便携安装（黑名单整搬）：
// BCU 便携目录 = 完整应用（自含运行时）+ BCUninstaller.settings，全部与 exe 同目录；
// 与 everything 白名单相反——这里"整套搬"才是用户预期语义。
// 调用方需先确保源实例未运行（exe 被写锁时拷贝不可信）。
func (m *Manager) ImportLocal(srcDir string) (BCUVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return BCUVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与其他模块 ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return BCUVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return BCUVersionInfo{}, err
	}

	// 黑名单整搬：临时/系统垃圾文件不搬，其余（exe/dll/settings/cache 等）全量保留
	var copied int
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		_ = os.RemoveAll(targetDir)
		return BCUVersionInfo{}, err
	}
	for _, e := range entries {
		if e.Name() == "meta.json" { // 源目录若已存在我们的元信息则跳过，导入会重写
			continue
		}
		if isTempLike(e.Name()) {
			continue
		}
		src, dst := filepath.Join(srcDir, e.Name()), filepath.Join(targetDir, e.Name())
		if e.IsDir() {
			if copyDirErr := copyDirTo(src, dst); copyDirErr != nil {
				_ = os.RemoveAll(targetDir)
				return BCUVersionInfo{}, copyDirErr
			}
		} else {
			if copyFileErr := copyFileTo(src, dst); copyFileErr != nil {
				_ = os.RemoveAll(targetDir)
				return BCUVersionInfo{}, copyFileErr
			}
		}
		copied++
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      copied,
	})

	launchExe := filepath.Join(targetDir, exeName)
	if inner := filepath.Join(targetDir, innerExeRel); isRegularFile(inner) {
		launchExe = inner // 与 ResolveExe 同口径：真身优先
	}
	return BCUVersionInfo{
		Version:     version,
		ExePath:     launchExe,
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// isTempLike 识别导入时不应搬运的临时/锁/系统垃圾文件。
func isTempLike(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".tmp") || strings.HasPrefix(lower, "~") {
		return true
	}
	if strings.Contains(lower, "-wal") || strings.Contains(lower, "-shm") {
		return true
	}
	if lower == "desktop.ini" || lower == "thumbs.db" {
		return true
	}
	return false
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

func copyDirTo(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirTo(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFileTo(s, d); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// fileSHA256 计算文件全量 SHA-256（十六进制小写）。
// 注意：仅用于诊断展示链（真身自哈希入账），不参与下载校验主流程
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
