package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"
)

const (
	// exeNamePrefix 上游主 exe 的文件名前缀（大小写不敏感匹配）。
	// 实测上游文件名**随版本漂移**（HEU_KMS_Activator_v64.0.0.exe），且该前缀同时
	// 是 instance 层进程探测的判据，因此导入与运行都必须保住它。
	exeNamePrefix = "HEU_KMS_Activator"

	// productHint 主程序身份锚点（PE VERSION 资源的 FileDescription 前缀）。
	// 实测 v64.0.0 为 "HEU KMS Activator™"，InternalName/OriginalFilename 为
	// "HEU_KMS_Activator_v64.0.0"——用它把关"导入的确实是这个工具"，免得用户
	// 误传别的 exe 进托管目录后被 Hanxi 当成 HEU 拉起。
	productHint = "HEU KMS Activator"

	// toolName 账本 Tool 字段（Meta.Tool 缺省会取 Tree 的 entryName，这里给展示名）。
	toolName = "HEU KMS Activator"

	treeEntryName = "heukms"
	dirPrefix     = treeEntryName + "_"
)

// fetchBudget 单次下载的总超时预算（与 litemonitor/ccswitch 同口径的 10 分钟）。
const fetchBudget = 10 * time.Minute

// plainVersionRe 正规版本令牌形状（与上游 tag 同形：纯三段数字，无 v 前缀）。
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的导入兜底目录后缀：现行形态 imported-<sha256 前 12 hex>
// （同内容必同版本，查重与时钟无关）；imported-YYYYMMDD-HHMMSS 为历史时间戳形态，继续收纳以兼容存量目录。
var importedDirRe = regexp.MustCompile(`^imported-(?:[0-9a-f]{12}|\d{8}-\d{6})$`)

// Manager HEU 版本管理引擎：远程列表（GitHub 元数据）+ "下载 → 校验 → RAR5 解包
// → 落位"主流程（下载与原子落位委托 Wave 4 共享内核 packages/go/artifact）
// + 本地导入兜底通道。
//
// 与 zip 家族的形态差异（都在本包内消化，不动内核纪律）：
//   - 解包器不是 UnpackZip 而是本包的 unpackRar（RAR5，经 nwaples/rardecode）——
//     上游当前只发 iso+rar，而 iso 的真实内容在 UDF 侧、无从纯 Go 读取（论证见包注释）；
//   - 可安装集更窄：只有"有 rar 资产且 GitHub 已挂官方 digest"的版本进远程表
//     （`artifact.Fetch` 强制摘要校验，无摘要根本装不了，列进来只会造出点不动的行）；
//   - 包内主程序文件名随版本漂移，故解包锚点是"包内唯一 *.exe"，落位后仍按
//     "目录内唯一 *.exe"定位，两处都不拼死文件名。
//
// 领域知识留在本包：托管资产筛选、镜像 URL 模板、rar 条目审阅与解包闸门、
// 版本目录形状 heukms_64.0.0 / heukms_imported-兜底 tag、PE FileVersion 与
// FileDescription 核账、本地导入（挂载 iso 或解压后手取 exe 的收纳路径）。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	// fetch 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
	// 流式上限），单测注入假实现以模拟中断/坏摘要源。
	fetch   func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error
	mirrors func(version, assetName string) []string

	// unpack RAR5 解包接缝：默认 unpackRar。本仓没有纯 Go 的 RAR **写**入库，
	// CI 造不出 rar 夹具，故下载链单测经此注入假解包器；真解包的安全判据由
	// planRarUnpack / sanitizeRarName 纯函数用例锁死（见 version_test.go）。
	unpack func(ctx context.Context, rarPath, dstDir string) (string, error)

	// fileVersion / fileDescription 是 PE VERSION 资源读取接缝：生产装配指向
	// internal/platform/versioninfo（Win32 VerQueryValue），单测注入假实现以
	// 覆盖身份闸与核账分支（非 Windows 构建下真实现必失败，不可入测）。
	fileVersion     func(path string) (string, error)
	fileDescription func(path string) (string, error)
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无 IO
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/ImportLocal/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir:     versionsDir,
		tree:            OpenTree(versionsDir),
		fetch:           artifact.Fetch,
		mirrors:         assetMirrors,
		unpack:          unpackRar,
		fileVersion:     versioninfo.FileVersion,
		fileDescription: func(path string) (string, error) { return versioninfo.StringValue(path, "FileDescription") },
	}
}

// OpenTree 打开 HEU 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取上游版本台账（10 分钟内命中缓存；多镜像回退）。
func (m *Manager) ListRemote() ([]HeuRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本（委托 Tree 扫描，按版本号降序）。
//
// 目录命名 heukms_64.0.0（导入的正规版本）或 heukms_imported-兜底 tag
// （版本探测失败的导入兜底，现行哈希/历史时间戳）；形状外的目录不列入。
//
// 三重健康闸（任一不过即跳过该目录，按"未安装"处理，不在列表里放假版本）：
//  1. 目录内主 exe 可定位且非空（findHostedExe）；
//  2. 正规版本目录须 PE FileVersion 归一后与目录令牌一致——不一致即目录内容与
//     名不符的损坏/误放安装（imported- 目录名不含版本，天然豁免核账）；
//  3. FileDescription 身份闸（防目录被换成无关 exe 后仍被列入并可启动）。
func (m *Manager) ListInstalled() ([]HeuVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []HeuVersionInfo
	for _, v := range vers {
		exe, exeErr := findHostedExe(v.Dir)
		if exeErr != nil {
			slog.Debug("heukms 版本目录缺少可用主程序，跳过", "dir", v.Dir, "reason", exeErr)
			continue
		}
		switch {
		case plainVersionRe.MatchString(v.Version):
			// 正规目录：核账（实版 == 目录名）+ 身份闸。
			if fv, ferr := m.fileVersion(exe); ferr != nil || normalizeFileVersion(fv) != v.Version {
				slog.Debug("heukms 版本目录与文件实版不符，跳过", "dir", v.Dir, "token", v.Version, "fileVersion", fv, "err", ferr)
				continue
			}
		case importedDirRe.MatchString(v.Version):
			// 兜底目录名不含版本，无从核账，只过身份闸。
		default:
			continue
		}
		if !m.matchesProduct(exe) {
			slog.Debug("heukms 版本目录内主程序身份校验未通过，跳过", "dir", v.Dir)
			continue
		}
		list = append(list, m.infoFrom(v.Dir, v.Version, filepath.Base(exe), v.Meta))
	}
	return list, nil
}

// Download 下载并安装指定版本（阻塞至收口；进度经 onProgress 回流）。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载 → 官方摘要校验 → RAR5 解包 → 三重闸 → 原子落位，
// 全程可经 ctx 取消（P0 批 2b 生命周期，与 litemonitor 同构）。
//
// 事务 ID 派生的独占中转目录（`.tmp-<txnID>`）与最终目录同卷，落位经 Tree.Commit
// 的 rename 一步定终身；任一步失败由 discard 清理，不留半件版本。
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

	// 1. 解析目标版本的远程资产（模块知识：GitHub 元数据 + 托管资产筛选）。
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *HeuRelease
	token := strings.TrimPrefix(strings.TrimSpace(version), "v")
	for i := range releases {
		if releases[i].Version == token {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		err := fmt.Errorf("远程列表中不存在版本 %s（上游更早的版本无 rar 资产或未发布官方摘要，不入可安装表，请改用「导入本地」）", version)
		emit("error", 0, 0, err.Error())
		return err
	}
	// 官方摘要信任根：GitHub release API 的 asset.digest（parseReleasesBody 已
	// 剥前缀并入列，无摘要者根本不进列表）。此处再兜一道，缺摘要一律拒装——
	// 内核 artifact.Fetch 也会拒绝无校验安装，两层同口径。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 HEU %s 的官方 SHA-256 摘要，拒绝无校验安装", token)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpRar, err := os.CreateTemp("", "hanxi-heukms-*.rar")
	if err != nil {
		return err
	}
	tmpRarPath := tmpRar.Name()
	defer os.Remove(tmpRarPath)
	tmpRar.Close()

	// 2. 受控下载（主址 + 镜像逐个回退；摘要核对与流式字节上限全部收口内核）。
	urls := m.mirrors(token, rel.AssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   rel.SHA256,
		MaxBytes: rel.Size, // 与 release API 声明大小对齐：超限即断，杜绝异常放大
		FileName: rel.AssetName,
	}
	emit("downloading", 0, rel.Size, "")
	fetchErr := m.fetch(ctx, src, tmpRarPath, func(p artifact.Progress) {
		// 内核进度 → 既有词表：只有流式下载阶段对应 downloading，其余阶段不上报
		if p.Stage == artifact.StageDownload {
			emit("downloading", p.Done, p.Total, "")
		}
	}, fetchBudget)
	if fetchErr != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", fetchErr))
		return fetchErr
	}

	// 3. 解包进独占中转目录（本模块自有的 RAR5 通道，闸门与判据见 unpack_rar.go）。
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

	emit("extract", 0, 0, "")
	exePath, err := m.unpack(ctx, tmpRarPath, staging)
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("解包失败: %v", err))
		return err
	}

	// 4. 落位前三重闸：PE 魔数、FileDescription 身份、FileVersion 与目标版本一致。
	//    （包摘要已由内核核过；这三道核的是"解出来的确实是这个版本的主程序"。）
	if err := requireMZ(exePath); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	if !m.matchesProduct(exePath) {
		err := fmt.Errorf("解包得到的 exe 其 FileDescription 不是 %s，拒收（上游包结构可能已变）", productHint)
		emit("error", 0, 0, err.Error())
		return err
	}
	actualVer, verr := m.fileVersion(exePath)
	if verr != nil {
		emit("error", 0, 0, fmt.Sprintf("读取主程序版本失败: %v", verr))
		return fmt.Errorf("读取主程序版本失败: %w", verr)
	}
	if normalizeFileVersion(actualVer) != token {
		err := fmt.Errorf("文件版本不匹配：期望 %s，实际 %s", token, normalizeFileVersion(actualVer))
		emit("error", 0, 0, err.Error())
		return err
	}

	// 5. 双摘要入账：包摘要（内核幂等/防漂移比对的锚）+ exe 摘要（诊断"文件被动过"）。
	assetSHA, shaErr := fileSHA256(exePath)
	if shaErr != nil {
		emit("error", 0, 0, fmt.Sprintf("计算主程序摘要失败: %v", shaErr))
		return shaErr
	}
	meta := artifact.Meta{
		Tool:        toolName,
		Entry:       filepath.Base(exePath),
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

// infoFrom 组装版本信息视图（账本 + 实况大小）。
func (m *Manager) infoFrom(dir, token, exeName string, meta artifact.Meta) HeuVersionInfo {
	exe := filepath.Join(dir, exeName)
	var size int64
	if fi, err := os.Stat(exe); err == nil {
		size = fi.Size()
	}
	installedAt := ""
	if !meta.InstalledAt.IsZero() {
		installedAt = meta.InstalledAt.Local().Format("2006-01-02 15:04:05")
	}
	return HeuVersionInfo{
		Version:     token,
		ExePath:     exe,
		ExeName:     exeName,
		Dir:         dir,
		Size:        size,
		InstalledAt: installedAt,
		IsImport:    meta.Source != artifact.SourceRemote,
		Source:      meta.Source,
		SHA256:      meta.AssetSHA256,
	}
}

// ImportLocal 导入本地已取出的 HEU 主 exe。
//
// srcDir 接受三种形态（都实测可用）：exe 所在目录、含 exe 的单层包装目录、
// **挂载后的虚拟光驱根目录**（如 F:\，这是"不解析 iso"路线下最顺手的一径——
// 双击挂载 → 在导入框里选该盘符 → Hanxi 把主 exe 拷进托管目录并只留主程序）。
//
// 落位走 Tree 的独占 staging + 原子 rename（与 zip 家族同一条内核通道），
// 任一步失败不留半件。exe 保留上游原文件名（进程探测与身份核账都依赖它）。
//
// 调用方需先确保该版本实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (HeuVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe, err := resolveImportExe(srcDir)
	if err != nil {
		return HeuVersionInfo{}, err
	}
	fi, err := os.Stat(srcExe)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return HeuVersionInfo{}, fmt.Errorf("源目录未找到可用的主程序: %s", srcDir)
	}
	if err := requireMZ(srcExe); err != nil {
		return HeuVersionInfo{}, err
	}
	// 身份闸：不是 HEU 就别进托管（误传 exe 后"启动"会拉起无关程序，属安全边界）。
	if !m.matchesProduct(srcExe) {
		return HeuVersionInfo{}, fmt.Errorf("该文件的 FileDescription 不是 %s，拒绝导入（请确认选择的是 HEU KMS Activator 主程序）", productHint)
	}

	// 摘要先行：既作内容哈希兜底的版本源，也进落位账本（AssetSHA256），
	// 一次流式哈希两处复用，不再对源 exe 二次开句柄。
	assetSHA, shaErr := fileSHA256(srcExe)
	if shaErr != nil {
		return HeuVersionInfo{}, fmt.Errorf("计算主程序摘要失败: %w", shaErr)
	}

	rawVer, vErr := m.fileVersion(srcExe)
	version := normalizeFileVersion(rawVer)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：内容哈希兜底——取源主程序
		// sha256 前 12 hex，同内容必同版本，重复导入判定与时钟无关（历史秒级
		// 时间戳兜底仅同秒内互斥，跨秒会把同一份文件静默装成两份）。导入时刻
		// 由账本 installedAt 承载，不再进版本串。
		version = "imported-" + assetSHA[:12]
	}
	// 查重对正规版本与兜底版本一视同仁（原 else-if 只挡正规形状，兜底形态
	// 落到 Kernel Commit 的"内容漂移"话术，重复导入语义不统一）。
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, dirPrefix+version)); statErr == nil {
		// 先于人话自查：Kernel 的"已装"分支文案是防同版本内容漂移设计的，
		// 对"重复导入同一个 exe"场景太吓人，这里给出可执行的下一步。
		return HeuVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}

	// txnID 取 UnixNano 而非秒/毫秒时间串：StageDir 是独占语义（同名即拒），
	// 纳秒级唯一才不至于把"连续导入两个版本"这种正常操作顶成事务冲突假失败。
	staging, discard, err := m.tree.StageDir(fmt.Sprintf("import-%d", time.Now().UnixNano()))
	if err != nil {
		return HeuVersionInfo{}, err
	}
	defer discard() // 成功 Commit 后为 no-op；任一步失败不留半件

	exeName := filepath.Base(srcExe)
	if err := copyFileTo(srcExe, filepath.Join(staging, exeName)); err != nil {
		return HeuVersionInfo{}, err
	}
	// 导入自校验：落位前再确认主程序可在 staging 内定位（防复制半途失败）。
	if _, err := findHostedExe(staging); err != nil {
		return HeuVersionInfo{}, err
	}

	meta := artifact.Meta{
		Tool:        toolName,
		Entry:       exeName,
		AssetSHA256: assetSHA,
		Source:      artifact.SourceImported,
		InstalledAt: time.Now(), // 导入时刻入账（版本串不再携时）
	}
	if err := m.tree.Commit(staging, version, meta); err != nil {
		return HeuVersionInfo{}, fmt.Errorf("落位失败: %w", err)
	}

	finalDir := filepath.Join(m.versionsDir, dirPrefix+version)
	return HeuVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(finalDir, exeName),
		ExeName:     exeName,
		Dir:         finalDir,
		Size:        dirSize(finalDir, fi.Size()),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      artifact.SourceImported,
		SHA256:      assetSHA,
	}, nil
}

// ResolveExe 返回指定版本的 HEU 主程序路径（不存在或损坏返回错误）。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	exe, err := findHostedExe(dir)
	if err != nil {
		return "", fmt.Errorf("版本 %s 安装损坏：%w", version, err)
	}
	return exe, nil
}

// Remove 卸载指定版本（委托 Tree：rename 隔离后删除，文件占用时留下可恢复状态）。
// HEU 运行中会锁 exe，rename 失败即由内核给出"先退出"人话指引。
func (m *Manager) Remove(version string) error {
	_, token, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return m.tree.Remove(token, nil)
}

// resolveVersionDir 定位版本隔离目录（heukms_64.0.0 或 heukms_imported-兜底 tag）。
// 形状外令牌（含路径穿越）先于任何磁盘访问被拒；容忍前端回传的 v 前缀形态。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	token = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(token) && !importedDirRe.MatchString(token) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	if d, rerr := m.tree.Resolve(token); rerr == nil {
		return d, token, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请在下方版本管理导入本地主程序", version)
}

// findHostedExe 在版本目录内定位主程序（文件名随版本漂移，不能拼死名）：
//   - 恰好一枚 *.exe → 采用；
//   - 多枚 → 按上游前缀 HEU_KMS_Activator* 筛，唯一命中才采用；
//   - 零枚或多枚命中 → 报错（布局不可信，宁拒不错）。
//
// 用 os.Stat 而非 DirEntry.Info()：虚拟光驱（UDF）等非常规介质上后者的
// 文件信息可能不可靠。
func findHostedExe(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("版本目录不可访问: %s", dir)
	}
	var all, prefixed []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".exe") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		fi, serr := os.Stat(full)
		if serr != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}
		all = append(all, full)
		if strings.HasPrefix(strings.ToLower(e.Name()), strings.ToLower(exeNamePrefix)) {
			prefixed = append(prefixed, full)
		}
	}
	switch {
	case len(all) == 1:
		return all[0], nil
	case len(prefixed) == 1:
		return prefixed[0], nil
	case len(all) == 0:
		return "", fmt.Errorf("目录内没有可用的 *.exe: %s", dir)
	default:
		return "", fmt.Errorf("目录内有 %d 枚可执行文件、无法判定主程序（请只保留上游的 %s*.exe）: %s", len(all), exeNamePrefix, dir)
	}
}

// resolveImportExe 在导入源内定位主 exe：srcDir 根命中优先，其次唯一单层子目录
// 命中（吸收"解压后带一层包装目录"的常见形态；挂载光驱根目录属前者）。
func resolveImportExe(srcDir string) (string, error) {
	if exe, err := findHostedExe(srcDir); err == nil {
		return exe, nil
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return "", fmt.Errorf("源目录不可访问: %s", srcDir)
	}
	var hits []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if exe, serr := findHostedExe(filepath.Join(srcDir, e.Name())); serr == nil {
			hits = append(hits, exe)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	return "", fmt.Errorf("源目录（及其单层子目录）未找到唯一可用的主程序 exe: %s", srcDir)
}

// matchesProduct PE FileDescription 身份闸。接缝失败按"不匹配"处理——
// 读不到版本资源就不是可信安装，不做"读不到就放行"的宽松降级。
func (m *Manager) matchesProduct(exe string) bool {
	if m.fileDescription == nil {
		return false
	}
	desc, err := m.fileDescription(exe)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(desc), productHint)
}

// requireMZ 校验 PE 头魔数 "MZ"（单文件导入通道的字节闸，
// 参照 rustdesk/subnetdesk 单 exe 家族的三重校验口径）。
func requireMZ(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf("读取文件头失败: %w", err)
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return fmt.Errorf("目标文件不是可执行的 PE 程序（文件头非 MZ）: %s", filepath.Base(path))
	}
	return nil
}

// normalizeFileVersion PE FileVersion 归一（上游实测 "64.0.0"；兼容 "x.y.z.0" 四段式）。
func normalizeFileVersion(fv string) string {
	fv = strings.TrimSpace(fv)
	if parts := strings.Split(fv, "."); len(parts) == 4 && parts[3] == "0" {
		fv = strings.Join(parts[:3], ".")
	}
	return fv
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

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

// dirSize 版本目录占用（度量失败/超时回退主程序文件大小，与 litemonitor 同口径）。
func dirSize(dir string, fallback int64) int64 {
	st := dirstats.MeasureBudgeted(dir, 2*time.Second)
	if st.Err != nil || st.Partial || st.Bytes <= 0 {
		slog.Debug("heukms 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
