package version

import (
	"context"
	"debug/pe"
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
	// exeName 单便携 exe 的落盘定名（与版本无关）：ResolveExe 确定性寻径。
	// 上游资产名恒为 MangoDisk-<ver>-windows-portable.exe，剥去版本段即此名；
	// 迁移前的历史目录仍以随行 meta.json 的 assetName（含版本）为准，
	// inspect 双轨寻径不受影响。
	exeName = "MangoDisk-windows-portable.exe"

	// treeEntryName 版本树目录前缀（<root>/mangodisk_<version>，与历史布局
	// mangodisk_1.0.7 一致——落位令牌为剥 v 后的 x.y.z，目录形状零漂移）。
	treeEntryName = "mangodisk"

	// moduleMetaFileName 模块侧账本：完整性巡检所需的基线字段（官方/安装摘要、
	// 期望与装机大小、PE 身份、导入来源）超出内核 meta.json 的通用形状，原样
	// 记在这里（形状与迁移前 meta.json 逐字段一致），与内核账本同目录共存、
	// 随事务 staging 原子落位。
	moduleMetaFileName = "meta.module.json"

	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 10 分钟口径）。
	fetchBudget = 10 * time.Minute
)

var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

type installMeta struct {
	SchemaVersion    int    `json:"schemaVersion"`
	Version          string `json:"version"`
	InstalledAt      string `json:"installedAt"`
	IsImport         bool   `json:"isImport"`
	Source           string `json:"source"`
	AssetName        string `json:"assetName"`
	ExpectedSize     int64  `json:"expectedSize"`
	InstalledSize    int64  `json:"installedSize"`
	ExpectedSHA256   string `json:"expectedSHA256"`
	InstalledSHA256  string `json:"installedSHA256"`
	FileVersion      string `json:"fileVersion"`
	ProductName      string `json:"productName"`
	VerifiedOfficial bool   `json:"verifiedOfficial"`
}

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager MangoDisk 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 校验 →
// 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch + Tree，单文件
// exe 形态无解包段）；本包保留 MangoDisk 领域知识：便携资产筛选、镜像 URL
// 模板、x.y.z 目录形状、PE 身份校验（download/import/巡检三处共用）、
// 完整性基线巡检（installMeta 双轨账本）与既有进度词表映射。
//
// 无内部锁：同一时刻仅允许一个下载由 service 层的 downloadMu 保证，
// Manager 自身可并发读（Tree 内部按根目录持锁）。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	fetch   fetcher
	mirrors func(version, assetName string) []string
	// verifyExe PE 身份校验接缝（默认真实 validateExecutable）：生产链路语义
	// 不变；假资产测试注入轻量桩（无真 MangoDisk PE 可用）。
	verifyExe func(path, wantVersion string) (string, string, error)
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		fetch:       artifact.Fetch,
		mirrors:     assetMirrors,
		verifyExe:   validateExecutable,
	}
}

// OpenTree 打开 MangoDisk 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 返回远端发布列表（走 remoteCache，TTL 内不发网络请求）。
func (m *Manager) ListRemote() ([]MangoDiskRelease, error) { return remoteCache.get() }

// ListInstalled 扫描本地版本目录（委托 Tree 扫描，按版本号降序）并逐个 inspect。
// 目录命名 mangodisk_X.Y.Z（当前落位格式与历史布局一致）；版本令牌仅接受
// x.y.z 形状（外来目录名不列入）；完整性异常（drifted/invalid）不隐藏，
// 如实列出让用户看到并可重下/重导入。
func (m *Manager) ListInstalled() ([]MangoDiskVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []MangoDiskVersionInfo
	for _, v := range vers {
		version, ok := versionFromToken(v.Version)
		if !ok {
			continue
		}
		list = append(list, m.inspect(v.Dir, version))
	}
	return list, nil
}

// Inspect 对单个已装版本做完整性检查（meta 基线哈希 vs 当前文件哈希/PE 版本信息）。
func (m *Manager) Inspect(version string) (MangoDiskVersionInfo, error) {
	dir, err := m.resolveVersionDir(version)
	if err != nil {
		return MangoDiskVersionInfo{}, err
	}
	return m.inspect(dir, normalizeVersion(version)), nil
}

// VerifyBeforeLaunch 启动前完整性闸门：Verified/LocalBaseline 放行；
// Drifted（被 MangoDisk 内置更新器替换过）与 Invalid 拒绝并返回可操作错误。
func (m *Manager) VerifyBeforeLaunch(version string) (MangoDiskVersionInfo, error) {
	info, err := m.Inspect(version)
	if err != nil {
		return info, err
	}
	switch info.Integrity {
	case IntegrityVerified, IntegrityLocalBaseline:
		return info, nil
	case IntegrityDrifted:
		return info, fmt.Errorf("版本 %s 的程序文件已发生变化，可能由 MangoDisk 内置更新器替换；请重新下载或重新导入", info.Version)
	default:
		return info, fmt.Errorf("版本 %s 安装无效：%s", info.Version, info.IntegrityNote)
	}
}

// Download 下载单文件便携 exe 并安装到 versions/mangodisk_X.Y.Z/。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub release API 官方资产摘要
// （已解析进 MangoDiskRelease.SHA256）为信任根做流式 + 落盘双 SHA-256 校验，
// Content-Length 与流式上限双核，镜像只是同摘要的备用传输来源；落位经
// Tree.Commit（staging 独占 + 原子 rename，同版本异摘要防漂移）。本包保留
// 领域动作：装机字节数与 release 声明对齐断言、PE 身份校验（x64 /
// ProductName / FileVersion 对版本）与 installMeta 基线账本。
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// 进度回调沿用本模块既有词表（downloading/verify/install/done|error）：
// 官方摘要双核由内核折进 download 阶段，不发明新词。onProgress 可选。
// Download 保留旧调用面，供版本包单测与非事务调用使用。
func (m *Manager) Download(txnID, version string, onProgress func(DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装，可由事务 context 取消（P0 批 2b 生命周期）。
func (m *Manager) DownloadContext(ctx context.Context, txnID, version string, onProgress func(DownloadProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	version = normalizeVersion(version)
	emit := func(stage string, done, total int64, message string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: message})
		}
	}
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	var rel *MangoDiskRelease
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
	// 官方摘要信任根：parseReleasesBody 已把无/坏 digest 的 release 挡在列表外，
	// 此处再守一道（缓存被外部注入时同样拒绝无校验安装）。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 MangoDisk %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}
	if _, err := m.resolveVersionDir(version); err == nil {
		return fmt.Errorf("版本 %s 已安装", version)
	}

	// 独占中转目录（staging 与最终目录同卷，供原子落位；目录名 .tmp-<txnID>
	// 由事务 ID 派生，journal 背书恢复据此收口现场）。exe 直接定名落进
	// staging，无需系统临时区中转。
	token := strings.TrimPrefix(version, "v")
	if err := ctx.Err(); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	defer discard() // 成功 Commit 后为 no-op；任一步失败不留半件
	stagedExe := filepath.Join(staging, exeName)

	// 受控下载（主址 + 镜像逐个回退；下载/摘要/字节数双核全部委托内核）
	urls := m.mirrors(version, rel.AssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   rel.SHA256,
		MaxBytes: rel.Size, // 与 release API 声明大小对齐：超限即断，杜绝异常放大
		FileName: rel.AssetName,
	}
	emit("downloading", 0, rel.Size, "")
	fetchErr := m.fetch(ctx, src, stagedExe, func(p artifact.Progress) {
		// 内核进度 → 既有词表：只有流式下载阶段对应 downloading，其余阶段本模块不上报
		if p.Stage == artifact.StageDownload {
			emit("downloading", p.Done, p.Total, "")
		}
	}, fetchBudget)
	if fetchErr != nil {
		emit("error", 0, rel.Size, fetchErr.Error())
		return fetchErr
	}

	// 领域校验（模块策略，内核不感知）：装机字节数与 release 声明对齐 +
	// PE 身份三连（x64 机器 / ProductName=MangoDisk / FileVersion 对版本）
	emit("verify", 0, rel.Size, "")
	actualSize, err := fileSize(stagedExe)
	if err != nil {
		return err
	}
	if actualSize != rel.Size {
		return fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actualSize)
	}
	fileVersion, productName, err := m.verifyExe(stagedExe, token)
	if err != nil {
		return err
	}

	// 落位 + 写账本：基线账本（installMeta）随 staging 原子落位进
	// meta.module.json；内核 meta.json（防漂移摘要）由 Commit 统一落盘。
	emit("install", 0, rel.Size, "")
	assetSHA := fileSHA256(stagedExe)
	baseline := installMeta{
		SchemaVersion: 1, Version: version, InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		Source: rel.AssetName, AssetName: exeName, ExpectedSize: rel.Size, InstalledSize: actualSize,
		ExpectedSHA256: rel.SHA256, InstalledSHA256: assetSHA, FileVersion: fileVersion,
		ProductName: productName, VerifiedOfficial: true,
	}
	if err := writeJSON(filepath.Join(staging, moduleMetaFileName), baseline); err != nil {
		return err
	}
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   rel.SHA256, // 单文件形态：包即资产，官方摘要同值
		AssetSHA256: assetSHA,
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("安装版本目录失败: %v", err))
		return fmt.Errorf("安装版本目录失败: %w", err)
	}
	emit("done", rel.Size, rel.Size, "")
	return nil
}

// Remove 卸载指定版本（委托 Tree：rename 隔离后删除，文件占用时留下可恢复
// 状态）；不检查是否正在运行——调用方（service）负责先做运行态拦截。
func (m *Manager) Remove(version string) error {
	if _, err := m.resolveVersionDir(version); err != nil {
		return err
	}
	return m.tree.Remove(strings.TrimPrefix(normalizeVersion(version), "v"), nil)
}

// ResolveExe 返回版本目录内的主程序绝对路径，缺文件视为版本损坏。
func (m *Manager) ResolveExe(version string) (string, error) {
	info, err := m.Inspect(version)
	if err != nil {
		return "", err
	}
	if info.ExePath == "" {
		return "", fmt.Errorf("版本 %s 缺少 MangoDisk 程序文件", version)
	}
	return info.ExePath, nil
}

// ImportLocal 把用户自备 EXE 纳管为本地版本：以 PE FileVersion 为版本号建目录
// 与基线 meta（IsImport=true、无官方 ExpectedSHA256，完整性校验退化为
// LocalBaseline 自检）。落盘按定名 exeName 归一（用户随意改名的源文件不再
// 参与寻径）。同版本号已存在时拒绝，不覆盖既有安装；落位经 Tree.Commit
// （staging + 原子 rename，Source=imported 入账）。
// 调用方需先确保实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcExe string) (MangoDiskVersionInfo, error) {
	srcExe = strings.TrimSpace(srcExe)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return MangoDiskVersionInfo{}, fmt.Errorf("未找到可用的 MangoDisk EXE: %s", srcExe)
	}
	fileVersion, productName, err := m.verifyExe(srcExe, "")
	if err != nil {
		return MangoDiskVersionInfo{}, err
	}
	if !plainVersionRe.MatchString(fileVersion) {
		return MangoDiskVersionInfo{}, fmt.Errorf("MangoDisk FileVersion 不是可识别的语义版本: %q", fileVersion)
	}
	version := "v" + fileVersion
	if _, err := m.resolveVersionDir(version); err == nil {
		return MangoDiskVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	staging, discard, err := m.tree.StageDir(fmt.Sprintf("imp-%s-%d", fileVersion, time.Now().UnixNano()))
	if err != nil {
		return MangoDiskVersionInfo{}, err
	}
	defer discard() // 成功 Commit 后为 no-op；半途失败不留半件
	if err := copyFileTo(srcExe, filepath.Join(staging, exeName)); err != nil {
		return MangoDiskVersionInfo{}, err
	}
	assetSHA := fileSHA256(filepath.Join(staging, exeName))
	baseline := installMeta{
		SchemaVersion: 1, Version: version, InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport: true, Source: srcExe, AssetName: exeName, InstalledSize: fi.Size(),
		InstalledSHA256: assetSHA, FileVersion: fileVersion, ProductName: productName,
	}
	if err := writeJSON(filepath.Join(staging, moduleMetaFileName), baseline); err != nil {
		return MangoDiskVersionInfo{}, err
	}
	meta := artifact.Meta{
		Entry:       exeName,
		AssetSHA256: assetSHA,
		Source:      artifact.SourceImported,
	}
	if err := m.tree.Commit(staging, fileVersion, meta); err != nil {
		return MangoDiskVersionInfo{}, err
	}
	return m.Inspect(version)
}

// inspect 完整性巡检：基线账本（新链 meta.module.json / 迁移前 meta.json 双轨）
// vs 当前文件哈希、大小与 PE 身份。判定阶梯与全部话术与迁移前逐字一致。
func (m *Manager) inspect(dir, version string) MangoDiskVersionInfo {
	info := MangoDiskVersionInfo{Version: version, Dir: dir, Integrity: IntegrityInvalid}
	meta, metaErr := readInstallMeta(dir)
	if metaErr == nil {
		info.InstalledAt = meta.InstalledAt
		info.IsImport = meta.IsImport
		info.Source = meta.Source
		info.ExpectedSHA256 = meta.ExpectedSHA256
	}
	assetName := meta.AssetName
	if assetName == "" {
		assetName = expectedAssetName(version)
	}
	exe := filepath.Join(dir, assetName)
	fi, err := os.Stat(exe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		info.IntegrityNote = "程序文件缺失或为空"
		return info
	}
	info.ExePath = exe
	info.Size = fi.Size()
	if info.InstalledAt == "" {
		info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
	}
	fileVersion, productName, identityErr := m.verifyExe(exe, "")
	info.FileVersion = fileVersion
	info.ProductName = productName
	if identityErr != nil {
		info.IntegrityNote = identityErr.Error()
		return info
	}
	info.CurrentSHA256 = fileSHA256(exe)
	if metaErr != nil || meta.InstalledSHA256 == "" {
		info.IntegrityNote = "安装元信息缺失或损坏"
		return info
	}
	if !strings.EqualFold(info.CurrentSHA256, meta.InstalledSHA256) || fi.Size() != meta.InstalledSize || fileVersion != meta.FileVersion {
		info.Integrity = IntegrityDrifted
		info.IntegrityNote = "程序文件与安装时基线不一致，可能已被上游更新器替换"
		return info
	}
	if meta.VerifiedOfficial {
		if meta.ExpectedSHA256 == "" || !strings.EqualFold(info.CurrentSHA256, meta.ExpectedSHA256) || fi.Size() != meta.ExpectedSize {
			info.Integrity = IntegrityDrifted
			info.IntegrityNote = "程序文件不再匹配官方发布资产"
			return info
		}
		info.Integrity = IntegrityVerified
		info.IntegrityNote = "GitHub 官方 SHA-256 与 PE 身份校验通过"
		return info
	}
	info.Integrity = IntegrityLocalBaseline
	info.IntegrityNote = "本地导入文件与导入时哈希基线一致"
	return info
}

// resolveVersionDir 定位版本隔离目录（mangodisk_X.Y.Z，与历史目录形状一致——
// 落位/扫描令牌均为剥 v 后的 x.y.z）。形状外令牌先于任何磁盘访问被拒，
// 错误口径与原实现逐字一致。
func (m *Manager) resolveVersionDir(version string) (string, error) {
	ver := strings.TrimPrefix(normalizeVersion(version), "v")
	if !plainVersionRe.MatchString(ver) {
		return "", fmt.Errorf("非法版本号: %q", version)
	}
	dir, err := m.tree.Resolve(ver)
	if err != nil {
		return "", fmt.Errorf("版本 v%s 未安装，请先下载或导入", ver)
	}
	return dir, nil
}

// versionFromToken 把 Tree 扫出的版本令牌规范化为 vX.Y.Z（1.0.7 → v1.0.7）。
// 仅接受纯 x.y.z 形状，与历史目录名解析口径一致（外来目录名被拒）。
func versionFromToken(token string) (string, bool) {
	if plainVersionRe.MatchString(token) {
		return "v" + token, true
	}
	return "", false
}

func normalizeVersion(version string) string {
	return "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func expectedAssetName(version string) string {
	return "MangoDisk-" + strings.TrimPrefix(version, "v") + "-windows-portable.exe"
}

// ---------- 完整性巡检账本（installMeta 双轨回读） ----------

// readInstallMeta 读取版本目录的基线账本：新链写 meta.module.json（内核
// meta.json 不承载完整性字段）；迁移前的历史安装只有 meta.json（同形状
// 旧账本），按其回读保持巡检语义零漂移。
func readInstallMeta(dir string) (installMeta, error) {
	if meta, err := readMeta(filepath.Join(dir, moduleMetaFileName)); err == nil {
		return meta, nil
	}
	return readMeta(filepath.Join(dir, "meta.json"))
}

func readMeta(path string) (installMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return installMeta{}, err
	}
	var meta installMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return installMeta{}, err
	}
	return meta, nil
}

// ---------- PE 身份领域校验（模块策略，内核不感知） ----------

func validateExecutable(path, wantVersion string) (string, string, error) {
	f, err := pe.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("不是有效的 Windows PE 文件: %w", err)
	}
	machine := f.FileHeader.Machine
	f.Close()
	if machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return "", "", fmt.Errorf("仅支持 Windows x64 MangoDisk，PE machine=0x%04x", machine)
	}
	productName, err := versioninfo.ProductName(path)
	if err != nil || !strings.EqualFold(strings.TrimSpace(productName), "MangoDisk") {
		return "", productName, fmt.Errorf("PE ProductName 不是 MangoDisk")
	}
	fileVersion, err := versioninfo.FileVersion(path)
	if err != nil {
		return "", productName, fmt.Errorf("读取 MangoDisk FileVersion 失败: %w", err)
	}
	fileVersion = normalizeFileVersion(fileVersion)
	if wantVersion != "" && fileVersion != wantVersion {
		return fileVersion, productName, fmt.Errorf("FileVersion 不匹配：期望 %s，实际 %s", wantVersion, fileVersion)
	}
	return fileVersion, strings.TrimSpace(productName), nil
}

func normalizeFileVersion(version string) string {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if idx := strings.IndexAny(version, " ,"); idx >= 0 {
		version = version[:idx]
	}
	parts := strings.Split(version, ".")
	if len(parts) == 4 && parts[3] == "0" {
		return strings.Join(parts[:3], ".")
	}
	return version
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

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
