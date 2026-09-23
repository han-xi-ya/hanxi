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
	exeName    = "TranslucentTB.exe"
	configName = "settings.json" // 上游便携版配置：恒在 exe 同目录（DetermineConfigPath 实证）

	// treeEntryName 版本树目录前缀（<root>/translucenttb_<version>，与历史布局
	// translucenttb_2026.2 / translucenttb_imported-<时间戳> 一致）。
	treeEntryName = "translucenttb"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源）。
	dirPrefix = treeEntryName + "_"
	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 10 分钟口径）。
	fetchBudget = 10 * time.Minute
)

// companionNames 便携 zip 根目录的必需伴生文件（官方 2022.1~2026.2 实测布局恒定）：
// exe 缺任何一件都无法完成"透明任务栏"核心功能（注入器/UI 线程库/日志/WinUI 页面）。
var companionNames = []string{
	"ExplorerHooks.dll",
	"ExplorerTAP.dll",
	"ProgramLog.dll",
	"Xaml.dll",
	"resources.pri",
}

// yearVersionRe 纯版本号（如 2026.2），用于目录名与 FileVersion 校验
var yearVersionRe = regexp.MustCompile(`^\d{4}\.\d+$`)

// fileVersionRe PE 版本资源归一化：上游 FileVersion 形如 2026.2.0.d4636e4
// （年份.序号.0.构建sha），取前两段即为与远程 tag 同域的规范版本。
var fileVersionRe = regexp.MustCompile(`^(\d{4}\.\d+)(?:\.\d+)?(?:\.[0-9a-fA-F.]+)?$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager TranslucentTB 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 校验 →
// 解包 → 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch +
// UnpackZip + Tree）；本包只保留 TranslucentTB 领域知识：便携资产筛选、镜像 URL
// 模板、版本目录形状（YYYY.N / imported-时间戳）、便携锚点（exe + 四 dll +
// resources.pri 全套伴生文件）布局自检、本地导入白名单与既有进度词表映射。
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

// OpenTree 打开 TranslucentTB 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]TBRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 translucenttb_2026.2（下载/导入的正规版本）或
// translucenttb_imported-YYYYMMDD-HHMMSS（版本探测失败的导入兜底）；
// 其余形状的版本令牌不列入；exe 缺失/为空或任一必需伴生文件缺失均视为损坏安装跳过。
func (m *Manager) ListInstalled() ([]TBVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []TBVersionInfo
	for _, v := range vers {
		version, ok := versionFromToken(v.Version)
		if !ok {
			continue
		}
		if !layoutValid(v.Dir) {
			continue
		}
		exe := filepath.Join(v.Dir, exeName)
		fi, _ := os.Stat(exe) // layoutValid 已保证存在

		info := TBVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    fi.Size(),
		}
		// 新下载链走内核统一账本（artifact.Meta，含 schema）；导入链与迁移前
		// 的历史账本走模块自写 map 形态，installedAt 原样展示、isImport/source
		// 仅导入账携带（source 在远程账的 map 形态里是资产名，非导入来源，
		// 故仅 isImport 时透出，展示面语义与原实现一致）。
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

// Download 下载便携 zip 并安装到 versions/translucenttb_YYYY.N/。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub API 官方资产摘要（digest，
// 已解析进 TBRelease.SHA256）为信任根做流式 + 落盘双 SHA-256 校验，
// Content-Length 与流式上限双核（对齐原"字节数 == release 声明 size"层），
// 镜像只是同摘要的备用传输来源；解包经 artifact.UnpackZip（ZipSlip/炸弹/CRC32
// 全量闸门，取代原 extractAll），落位经 Tree.Commit（staging + 原子 rename，
// 同版本异摘要防漂移——原实现直写最终目录，半件即污染安装）。
// 便携锚点自检（exe + 全套伴生文件）为模块策略，收在 Commit 落位之前。
// 进度回调沿用本模块既有词表（downloading/extract/done/error）：verify
// （官方摘要双核）由内核折进 download 阶段，不造幻影步骤。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// onProgress 可选：实时上报各阶段进度（下载字节、解压落位）。
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
	var rel *TBRelease
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
	// 已剥前缀入 TBRelease.SHA256，无摘要的 release 根本不进列表）。缺失一律
	// 拒装，不做无校验安装。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 TranslucentTB %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-translucenttb-*.zip")
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
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）。
	// 官方便携包除锚点文件外还含 Assets/ 等展示资源且随版本演进，无法预先
	// 给出完整文件清单，故不做 allowFiles 白名单限制（白名单语义是"包内文件
	// 必须全在清单"，资产布局一变即全线拒装），改由下一步锚点自检收口。
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
	if err := artifact.UnpackZipContext(ctx, tmpZipPath, staging, artifact.DefaultLimits, nil); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}
	// 4. 便携锚点布局自检（模块策略，内核不感知）：exe 非空 + 全套伴生文件就位
	if err := checkPortableLayout(staging); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	// exe 诊断摘要入账（TBVersionInfo 不展示，纯账本诊断）
	assetSHA, _ := fileSHA256(filepath.Join(staging, exeName))

	// 5. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移）
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   rel.SHA256,
		AssetSHA256: assetSHA,
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, version, meta); err != nil {
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

// ResolveExe 返回指定版本的 TranslucentTB.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（translucenttb_2026.2 或 translucenttb_imported-时间戳）。
// 形状外令牌（含路径穿越）先于任何磁盘访问被拒，错误口径与原实现一致。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	token = strings.TrimSpace(version)
	if !yearVersionRe.MatchString(token) && !importedDirRe.MatchString(token) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	if d, rerr := m.tree.Resolve(token); rerr == nil {
		return d, token, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// versionFromToken 把 Tree 扫出的版本令牌规范化（translucenttb_2026.2 → 2026.2，
// 与远程 tag 同域、无 v 前缀）。仅接受纯 YYYY.N 或 imported-时间戳形状，
// 其余外来目录名不列入（与原 dirNameRe 口径一致）。
func versionFromToken(token string) (string, bool) {
	if yearVersionRe.MatchString(token) || importedDirRe.MatchString(token) {
		return token, true
	}
	return "", false
}

// checkPortableLayout 便携锚点自检（模块策略）：staging 内 TranslucentTB.exe
// 存在且非空、全套必需伴生文件就位（官方便携 zip 恒有全套）。不符即判定安装
// 无效，调用方丢弃 staging。
func checkPortableLayout(staging string) error {
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	}
	var missing []string
	for _, c := range companionNames {
		if _, err := os.Stat(filepath.Join(staging, c)); err != nil {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("zip 布局无效：缺少必需伴生文件 %s", strings.Join(missing, "、"))
	}
	return nil
}

// layoutValid 安装布局完整性（ListInstalled 扫描用）：等价于锚点自检通过。
func layoutValid(dir string) bool { return checkPortableLayout(dir) == nil }

// readLegacyMetaFields 读取模块自写的导入账本与迁移前历史账本（无 schema 的
// map 形态）：installedAt 原样字符串、isImport 布尔、source 导入来源目录。
// 新下载链的 artifact.Meta 账本（含 schema）不走本函数。
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

// ---------- 本地导入（TranslucentTB 领域流程，无内核对应物） ----------

// ImportLocal 导入本地已安装的 TranslucentTB 便携版（整套迁移）。
// 与 ccswitch 的单 exe 导入不同：TranslucentTB 的 settings.json 恒在 exe 同目录
// （上游 DetermineConfigPath 便携分支实证），配置跟着安装位置走，
// 故 exe + 全部伴生 dll + resources.pri + Assets/ + settings.json（若有）整体搬入。
// 调用方需先确保源实例未运行（Windows 下运行中的 exe 被独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (TBVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return TBVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}
	// 导入源必须是完整便携安装：缺伴生文件的"半个目录"导进来也跑不起来
	var missing []string
	for _, c := range companionNames {
		if _, err := os.Stat(filepath.Join(srcDir, c)); err != nil {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return TBVersionInfo{}, fmt.Errorf("源目录缺少必需伴生文件：%s（请选择完整的便携版目录）", strings.Join(missing, "、"))
	}

	version := normalizeImportedVersion(srcExe)
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return TBVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return TBVersionInfo{}, err
	}

	// 白名单迁移：exe + 伴生文件 + 用户配置（存在则搬）。其余文件一概不搬。
	var copied []string
	whitelist := append([]string{exeName}, companionNames...)
	whitelist = append(whitelist, configName)
	for _, name := range whitelist {
		src := filepath.Join(srcDir, name)
		st, statErr := os.Stat(src)
		if statErr != nil || st.IsDir() {
			continue // settings.json 可能尚未生成（从未首启过），缺项跳过
		}
		if err := copyFileTo(src, filepath.Join(targetDir, name)); err != nil {
			_ = os.RemoveAll(targetDir)
			return TBVersionInfo{}, err
		}
		copied = append(copied, name)
	}
	// Assets/（启动画面资源）存在则整体搬入
	if err := copyDirIfAny(filepath.Join(srcDir, "Assets"), filepath.Join(targetDir, "Assets")); err != nil {
		_ = os.RemoveAll(targetDir)
		return TBVersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      strings.Join(copied, ", "),
	})

	return TBVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// normalizeImportedVersion 从 PE 版本资源提取规范版本号：
// 上游 FileVersion 形如 2026.2.0.d4636e4，归一化为与远程 tag 同域的 2026.2；
// 探测失败或非规范格式退化为时间戳兜底目录（与 frpc/ccswitch ImportLocal 同构）。
func normalizeImportedVersion(exePath string) string {
	if fv, err := versioninfo.FileVersion(exePath); err == nil {
		if g := fileVersionRe.FindStringSubmatch(strings.TrimSpace(fv)); g != nil {
			return g[1]
		}
	}
	return "imported-" + time.Now().Format("20060102-150405")
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

// copyDirIfAny 目录存在且含文件时递归搬入；源目录不存在直接成功（导入源可能精简）。
func copyDirIfAny(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(dstDir, e.Name())
		if e.IsDir() {
			if err := copyDirIfAny(src, dst); err != nil {
				return err
			}
			continue
		}
		if err := copyFileTo(src, dst); err != nil {
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
