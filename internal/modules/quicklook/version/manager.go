package version

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
)

const (
	exeName          = "QuickLook.exe"          // 便携包主程序（托盘 Manager，含全局键盘钩子与预览视图）
	portableMarkName = "portable.lock"          // 官方便携标记：exe 同目录即令配置随 exe 走（SettingHelper.IsPortableVersion）
	nativeMarkName   = "QuickLook.Native64.dll" // 原生 helper（查询焦点窗口选中项），布局自检哨兵

	// treeEntryName 版本树目录前缀（<root>/quicklook_<version>，与历史布局
	// quicklook_4.5.0 / quicklook_imported-<时间戳> 一致）。
	treeEntryName = "quicklook"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源）。
	dirPrefix = treeEntryName + "_"
	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 10 分钟口径，便携 zip 约 117MB）。
	fetchBudget = 10 * time.Minute
)

// plainVersionRe 纯版本号（如 4.5.0），用于目录名与 FileVersion 校验（上游 tag 无 v 前缀）
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager QuickLook 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 官方摘要
// 双核"委托 Wave 4 共享内核 packages/go/artifact（Fetch + Tree），落位/扫描/卸载
// 走 Tree 的 staging + 原子 rename 链；本包保留两类 QuickLook 领域知识与一处
// bespoke 段：
//   - 便携资产筛选（QuickLook-<ver>.zip 精确比对）、镜像 URL 模板、版本目录
//     形状（x.y.z / imported-时间戳）、便携双锚点（exe + portable.lock +
//     QuickLook.Native64.dll）布局自检与既有进度词表映射；
//   - 解包段留本包 extractAll（不委托 artifact.UnpackZip，ADR-0003 单家特例，
//     按 ADR-0002 §3 不入内核）：上游官方 zip 有两处内核 UnpackZip 不兼容的
//     实测事实——①目录条目以反斜杠 "\" 结尾（archive/zip 的 IsDir 判不出，
//     内核会把目录当文件落 0 字节同名件、随后其下文件因祖先被文件占位而整包
//     拒收）；②QuickLook.Plugin\...\runtimes\<rid>\lib\<tfm>\*.dll 深层树在
//     版本基目录下极易越过 Win32 MAX_PATH(260)，落盘必须走 "\\?\" 长路径前缀
//     （本包 longPath），内核解包无此通道且无第二家需求；
//   - 本地整套导入（ImportLocal）与下载链独立，仍为本包 bespoke。
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

// OpenTree 打开 QuickLook 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]QuickLookRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 quicklook_4.5.0（下载/导入的正规版本，上游 tag 无 v 前缀，展示
// 裸号与迁移前一致）或 quicklook_imported-YYYYMMDD-HHMMSS（版本探测失败的
// 导入兜底）；其余形状的版本令牌不列入；exe 缺失/为空或便携标记缺失均视为
// 损坏安装跳过。
func (m *Manager) ListInstalled() ([]QuickLookVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []QuickLookVersionInfo
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
		// 便携标记与 exe 同目录是"配置随 exe 走"的激活条件（SettingHelper.LocalDataPath 判据）：
		// 缺失则退回 %APPDATA%\pooi.moe\QuickLook 用户目录，破坏版本隔离与整体导入语义，视为损坏
		if _, statErr := os.Stat(filepath.Join(v.Dir, portableMarkName)); statErr != nil {
			continue
		}

		info := QuickLookVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    fi.Size(),
		}
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

// Download 下载便携 zip 并安装到 versions/quicklook_X.Y.Z/。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub API 官方资产摘要（digest，
// 事实核查 2026-09-19：4.1.1–4.5.0 便携 zip 全量携带 digest，parseReleasesBody
// 已把无摘要 release 挡在列表外）为信任根做流式 + 落盘双 SHA-256 校验，
// Content-Length 与流式上限双核（对齐原"字节数 == release 声明 size"层），
// 镜像只是同摘要的备用传输来源；落位经 Tree.Commit（staging + 原子 rename，
// 同版本异摘要防漂移——原实现直写最终目录，半件即污染安装）。
// 解包段仍走本包 extractAll（longpath + 反斜杠目录条目双特例，见包注释）。
// 进度回调沿用本模块既有词表（downloading/verify/extract/done/error）：
// verify（官方摘要双核）由内核 Fetch 在流式+落盘复核完成后上报，映射进
// 既有词汇保持前端"哈希校验…"展示段零漂移。
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
// bespoke extractAll 已 ctx 化（entry 边界即时中止并自清半件）。
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
	var rel *QuickLookRelease
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
	// 已剥前缀入 QuickLookRelease.SHA256，无摘要的 release 根本不进列表）。
	// 缺失一律拒装，不做无校验安装。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 QuickLook %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-quicklook-*.zip")
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
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）。
	// 解包刻意走本包 bespoke extractAll 而非内核 UnpackZip：官方 zip 的反斜杠
	// 目录条目与深层插件树长路径两处特例内核不感知（ADR-0002 §3 单家需求
	// 不入内核，裁决全文见 ADR-0003），安全闸门（ZipSlip/CRC32 读满）在
	// extractAll 内自持，纪律同内核。
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
	if err := extractAll(ctx, tmpZipPath, staging); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}
	// extractAll 收尾已完成便携双锚点自检（exe 非空 + portable.lock + 原生 DLL）
	// 与便携标记兜底补写（模块策略，Commit 前收口）。
	// exe 诊断摘要入账（避免每次扫描现场哈希；QuickLookVersionInfo 不展示，纯账本诊断）
	assetSHA := fileSHA256(filepath.Join(staging, exeName))

	// 4. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
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

// ResolveExe 返回指定版本的 QuickLook.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（quicklook_X.Y.Z 或 quicklook_imported-时间戳）。
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

// versionFromToken 把 Tree 扫出的版本令牌规范化为展示版本号（4.5.0 → 4.5.0，
// 上游 tag 无 v 前缀，展示口径与迁移前"目录名剥前缀"一致）。仅接受纯 x.y.z 或
// imported-时间戳形状，quicklook_v4.5.0 之类带 v 前缀的历史/外来目录名不列入。
func versionFromToken(token string) (string, bool) {
	if plainVersionRe.MatchString(token) || importedDirRe.MatchString(token) {
		return token, true
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

// ---------- 本地导入（QuickLook 领域流程，无内核对应物） ----------

// ImportLocal 导入本地已解压/已安装的 QuickLook 目录（整套迁移）。
// 与 ccswitch 的单 exe 导入不同：QuickLook 是便携多文件程序（exe + 原生 DLL +
// QuickLook.Plugin 插件树 + portable.lock 全在根目录，配置随 portable.lock 落此目录），
// 只搬 exe 无法运行，故递归迁移整个目录（连用户已有的 .config 设置一并保留）。
// 调用方需先确保源实例未运行（运行中的 exe/DLL 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (QuickLookVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return QuickLookVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc/keyviz ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return QuickLookVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return QuickLookVersionInfo{}, err
	}

	if err := copyTree(srcDir, targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return QuickLookVersionInfo{}, err
	}
	// 强制便携语义：确保 portable.lock 在位（正常上游包已含；导入残缺包时补齐，
	// 保证配置随 exe 走、不越界写用户 %APPDATA%）
	_ = ensureFile(filepath.Join(targetDir, portableMarkName))

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      "<整个目录（含插件与原生 DLL）>",
	})

	return QuickLookVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// ---------- bespoke 解包（QuickLook 特例：longpath + 反斜杠目录条目） ----------

// extractAll 全量解压 zip 到目标目录。每个 entry 必须读满——
// completion 路径中的 io.Copy 跑完触发 archive/zip 内建 CRC32 校验。
// 提取完成后自检布局（exe 非空 + 便携标记 + 原生 DLL），不符即清理目标目录报错。
//
// 注意：QuickLook 官方 zip 内条目名用反斜杠 "\" 作分隔（实测 v4.5.0），而标准
// zip 惯例为 "/"——统一先把 "\" 归一为 "/" 再按当前平台分隔符落地，避免 Windows 下
// filepath.Clean 对混合分隔符处理不一致导致的路径歧义。
//
// 本段不委托 artifact.UnpackZip 的两处领域特例（单家需求，按 ADR-0002 §3
// 不入内核，裁决全文见 ADR-0003）：①反斜杠结尾的目录条目 archive/zip 的
// IsDir 判不出，必须"归一后按尾斜杠判目录"；②深层插件树落盘必须加
// "\\?\" 长路径前缀（longPath），内核解包无此通道。ZipSlip 防护与 CRC32
// 读满纪律在本包自持。
// extractAll bespoke 解包（P0 批 2b：ctx 在每个 entry 边界与拷贝前后检查，
// 取消即删半件目录，纪律与闸门同构内核 UnpackZipContext）。
func extractAll(ctx context.Context, zipPath, targetDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		// 官方 zip 条目名用反斜杠 "\" 分隔（实测 v4.5.0），标准 zip 惯例为 "/"。
		// 关键坑：archive/zip 的 FileHeader.IsDir() 仅以"结尾是否为 /"判定，对
		// "\…\runtimes\" 这类反斜杠结尾的目录条目返回 false → 若沿用 IsDir 会把
		// 目录误当文件 os.Create 出一个同名 0 字节文件，随后在其下建目录即报
		// "找不到路径"。故先归一为 "/" 再按尾部斜杠判目录，不依赖 IsDir。
		norm := strings.ReplaceAll(f.Name, "\\", "/")
		isDir := strings.HasSuffix(norm, "/") || f.FileInfo().IsDir()
		// 用 path（纯斜杠语义、跨平台一致）清洗与越界判定，最后才 FromSlash 落地，
		// 避免 filepath.Clean 把 "/" 换成 "\" 令 "../" 前缀检查在 Windows 上落空。
		rel := path.Clean(strings.TrimSuffix(norm, "/"))
		if rel == "." || rel == "/" {
			continue // 指向目标根的冗余 "."/"" 条目
		}
		// ZipSlip 防护：拒绝绝对路径与逃逸出目标目录的条目
		if path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
			return fail(fmt.Errorf("zip 含非法路径条目 %q", f.Name))
		}
		target := filepath.Join(targetDir, filepath.FromSlash(rel))
		if isDir {
			if err := os.MkdirAll(longPath(target), 0755); err != nil {
				return fail(err)
			}
			continue
		}
		if err := os.MkdirAll(longPath(filepath.Dir(target)), 0755); err != nil {
			return fail(err)
		}
		rc, err := f.Open()
		if err != nil {
			return fail(err)
		}
		out, err := os.Create(longPath(target))
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

	// 布局自检：exe 存在且非空 + 便携标记 + 原生 DLL（官方 zip 根恒有）
	if err := checkLayout(targetDir); err != nil {
		return fail(err)
	}
	// 兜底保证便携标记在位（正常已在 zip 中；防御性再确认一次）
	_ = ensureFile(filepath.Join(targetDir, portableMarkName))
	return nil
}

// checkLayout 校验隔离目录是合法的 QuickLook 便携布局。
func checkLayout(dir string) error {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	}
	if _, err := os.Stat(filepath.Join(dir, portableMarkName)); err != nil {
		return fmt.Errorf("zip 布局无效：缺少 %s 便携标记", portableMarkName)
	}
	if _, err := os.Stat(filepath.Join(dir, nativeMarkName)); err != nil {
		return fmt.Errorf("zip 布局无效：缺少 %s 原生组件", nativeMarkName)
	}
	return nil
}

// copyTree 把 src 目录内容整体复制到 dst（保留相对子目录结构）。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(longPath(target), 0755)
		}
		if err := os.MkdirAll(longPath(filepath.Dir(target)), 0755); err != nil {
			return err
		}
		return copyFileTo(path, longPath(target))
	})
}

// ensureFile 若目标文件不存在则创建一个空文件（用作便携标记兜底）。
func ensureFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	f, err := os.OpenFile(longPath(path), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	return f.Close()
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
