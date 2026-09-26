package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"
)

const (
	exeName = "ddns-go.exe"

	// treeEntryName 版本树目录前缀（<root>/ddnsgo_<version>，与历史布局
	// ddnsgo_6.17.6 同构，与 ccswitch_ / frp_ / markeron_ 同族）。
	treeEntryName = "ddnsgo"

	// moduleMetaFileName 模块侧账本补充：内核 meta.json 只承载通用字段，
	// DdnsVersionInfo 的 Source（导入来源目录 / 远程资产名）与 IsImport 的
	// "来源明细"记在这里，与内核账本同目录共存、随事务 staging 原子落位。
	moduleMetaFileName = "meta.module.json"

	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 10 分钟口径）。
	fetchBudget = 10 * time.Minute
)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// plainVersionRe 纯版本号（如 6.17.6），用于目录名与 FileVersion 校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// wideTokenRe 历史目录名宽口径（与迁移前 dirNameRe 的宽松分支同构：数字起头 +
// [0-9a-zA-Z.] 续）——"ddnsgo_6.17.6.bak" 这类手工杂物目录会被收纳扫描，
// exe 缺失即跳过，不构成损坏误判。
var wideTokenRe = regexp.MustCompile(`^[0-9][0-9a-zA-Z.]+$`)

// Manager ddns-go 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 校验 →
// 解包 → 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch +
// UnpackZip + Tree）；本包只保留 ddns-go 领域知识：Windows x64 zip 资产筛选、
// 镜像 URL 模板、vX.Y.Z 版本形状、单 exe 布局自检、导入语义与既有进度词表映射。
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

// OpenTree 打开 ddns-go 版本树（目录前缀等领域知识只在本包定义，
// 调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]DdnsRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描）。
// exe 缺失/为空视为损坏安装跳过。与 ccswitch 不同：ddns-go 官方便携包内
// 除 exe 外仅有 LICENSE/README 文档件，无功能性标记文件（配置恒在用户主目录），
// 故布局自检只看 exe 本体，README 内容漂移不构成损坏。
// 账目双轨：内核新账本（meta.json 带 schema）来源词汇走 Meta.Source、
// 明细读 meta.module.json；迁移前的历史安装逐字段回读旧 meta.json。
// 排序：数值分段降序、imported 兜底目录沉底（冷启动回退"最新已装"绝不误选）。
func (m *Manager) ListInstalled() ([]DdnsVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []DdnsVersionInfo
	for _, v := range vers {
		version, ok := versionFromToken(v.Version)
		if !ok {
			continue
		}
		exe := filepath.Join(v.Dir, exeName)
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}

		info := DdnsVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    dirSize(v.Dir, fi.Size()),
		}
		if v.Meta.Schema != 0 {
			info.IsImport = v.Meta.Source == artifact.SourceImported
			info.Source = readModuleMeta(v.Dir).Source
			info.InstalledAt = formatInstalledAt(v.Meta.InstalledAt, v.Dir, fi)
		} else {
			// 迁移前的历史安装：无 schema 的旧 meta.json 逐字段回读
			info.IsImport, info.Source, info.InstalledAt = readLegacyMeta(v.Dir, fi)
		}
		list = append(list, info)
	}
	sort.SliceStable(list, func(i, j int) bool {
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

// Download 下载官方 zip 并解压安装到 versions/ddnsgo_X.Y.Z/。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub release API 官方资产摘要
// （digest）为信任根做流式 + 落盘双 SHA-256 校验，Content-Length 与流式上限
// 双核（取代旧"落盘后字节数核对 + 手工 sha256"两层），镜像只是同摘要的备用
// 传输来源；解包经 artifact.UnpackZip（ZipSlip/炸弹/CRC32 全量闸门，取代旧
// extractAll），落位经 Tree.Commit（staging 独占 + 原子 rename，同版本异摘要
// 防漂移）。本包保留领域动作：exe 非空布局自检与既有进度词表映射
// （downloading/verify/extract/done/error，不发明新词）。
//
// txnID 由 service 层事务通道生成并透传（staging 目录 .tmp-<txnID>）：崩溃
// 残件可被 journal 背书法启动恢复认领（ADR-0002/Wave 4-B），正常失败路径
// defer discard 收口；Manager 为内部面，RPC 面零漂移。
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
	var rel *DdnsRelease
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
	// 官方摘要信任根：parseReleasesBody 已把无 digest 的 release 挡在列表外，
	// 此处再守一道（缓存被外部注入时同样拒绝无校验安装）。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 ddns-go %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-ddnsgo-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 受控下载（主址 + 镜像逐个回退；下载/摘要/字节数双核全部委托内核）
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

	// 3. 摘要核验阶段（既有词表）：内核 Fetch 已在传输与落盘两道完成官方
	// SHA-256 全核，失败根本走不到这里——本阶段作为进度叙事保留。
	emit("verify", 0, 0, "")

	// 4. 解包进独占中转目录（staging 与最终目录同卷，供原子落位）
	if err := ctx.Err(); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	token := strings.TrimPrefix(version, "v")
	if err := artifact.ValidateVersionToken(token); err != nil {
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
	// 5. 布局自检（模块策略，内核不感知）：exe 非空（官方便携 zip 恒含）
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		err := fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
		emit("error", 0, 0, err.Error())
		return err
	}
	// 来源明细入账（远程安装记资产名，与历史 meta.json 的 source 字段口径一致）
	if err := writeModuleMeta(staging, rel.AssetName, false); err != nil {
		emit("error", 0, 0, fmt.Sprintf("写入来源账目失败: %v", err))
		return err
	}

	// 6. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移）
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   rel.SHA256, // 官方包摘要（内核 Fetch 已全核复核）
		AssetSHA256: fileSHA256(filepath.Join(staging, exeName)),
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

// ResolveExe 返回指定版本的 ddns-go.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（ddnsgo_X.Y.Z 或 ddnsgo_imported-时间戳）。
// 白名单形状校验保持迁移前口径（非法版本号与未安装的引导文案逐字不变）。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(ver) && !importedDirRe.MatchString(ver) && !wideTokenRe.MatchString(ver) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	d, rerr := m.tree.Resolve(ver)
	if rerr != nil {
		return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
	}
	return d, ver, nil
}

// versionFromToken 把 Tree 扫出的版本令牌规范化为 vX.Y.Z（6.17.6 → v6.17.6，
// imported-… → vimported-…）。仅接受历史 dirNameRe 的两支形状
// （纯语义/宽数字起头 与 imported-时间戳），markeron_ 等外来目录不列入。
func versionFromToken(token string) (string, bool) {
	if plainVersionRe.MatchString(token) || importedDirRe.MatchString(token) || wideTokenRe.MatchString(token) {
		return "v" + token, true
	}
	return "", false
}

// ImportLocal 导入本地已有的 ddns-go.exe（任意位置下载的官方原版）。
// 与 ccswitch 的导入语义一致：配置恒在 ~/.ddns_go_config.yaml（上游固定用户
// 主目录约定），与 exe 位置无关，故只迁移单 exe，其余文件一概不搬。
// 落位走 Tree.Commit（staging + 原子 rename，Source=imported 入账，
// 来源明细记 meta.module.json）。
// 调用方需先确保源实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (DdnsVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return DdnsVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（资源缺失或非 Windows）：时间戳兜底，与 frpc ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	if _, rerr := m.tree.Resolve(version); rerr == nil {
		return DdnsVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	staging, discard, err := m.tree.StageDir(fmt.Sprintf("imp-%s-%d", version, time.Now().UnixNano()))
	if err != nil {
		return DdnsVersionInfo{}, err
	}
	defer discard() // 成功 Commit 后为 no-op；半途失败不留半件

	if err := copyFileTo(srcExe, filepath.Join(staging, exeName)); err != nil {
		return DdnsVersionInfo{}, err
	}
	if err := writeModuleMeta(staging, srcDir, true); err != nil {
		return DdnsVersionInfo{}, err
	}
	meta := artifact.Meta{
		Entry:       exeName,
		AssetSHA256: fileSHA256(filepath.Join(staging, exeName)),
		Source:      artifact.SourceImported,
	}
	if err := m.tree.Commit(staging, version, meta); err != nil {
		return DdnsVersionInfo{}, err
	}

	dir, _, err := m.resolveVersionDir("v" + version)
	if err != nil {
		return DdnsVersionInfo{}, err
	}
	return DdnsVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(dir, exeName),
		Dir:         dir,
		Size:        dirSize(dir, fi.Size()),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// ---------- 来源账目（内核 meta.json 之外的模块侧补充） ----------

// moduleMeta 模块侧账本内容（meta.module.json）：source 为导入来源目录
// （IsImport=true）或远程资产名（IsImport=false）。历史安装的旧 meta.json
// 里的 source/isImport 字段与之语义等价，ListInstalled 双轨回读。
type moduleMeta struct {
	Source   string `json:"source"`
	IsImport bool   `json:"isImport"`
}

func writeModuleMeta(dir, source string, isImport bool) error {
	data, err := json.MarshalIndent(moduleMeta{Source: source, IsImport: isImport}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, moduleMetaFileName), data, 0644)
}

func readModuleMeta(dir string) moduleMeta {
	raw, err := os.ReadFile(filepath.Join(dir, moduleMetaFileName))
	if err != nil {
		return moduleMeta{}
	}
	var mm moduleMeta
	if json.Unmarshal(raw, &mm) != nil {
		return moduleMeta{}
	}
	return mm
}

// readLegacyMeta 回读迁移前旧账本（map 形态）里的 isImport/source/installedAt
// 三字段；安装时间读不到回退 exe 修改时间（与旧口径一致）。
func readLegacyMeta(dir string, exeInfo os.FileInfo) (bool, string, string) {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return false, "", legacyInstalledAt(exeInfo)
	}
	var mm map[string]any
	if json.Unmarshal(raw, &mm) != nil {
		return false, "", legacyInstalledAt(exeInfo)
	}
	isImport, _ := mm["isImport"].(bool)
	source, _ := mm["source"].(string)
	at, _ := mm["installedAt"].(string)
	if at == "" {
		at = legacyInstalledAt(exeInfo)
	}
	return isImport, source, at
}

func legacyInstalledAt(exeInfo os.FileInfo) string {
	if exeInfo != nil {
		return exeInfo.ModTime().Format("2006-01-02 15:04:05")
	}
	return ""
}

// formatInstalledAt 安装时间展示（yyyy-MM-dd HH:mm:ss）：优先内核账本
// installedAt，无账本回退 exe 修改时间。
func formatInstalledAt(fromMeta time.Time, dir string, exeInfo os.FileInfo) string {
	if !fromMeta.IsZero() {
		return fromMeta.Local().Format("2006-01-02 15:04:05")
	}
	if legacy := readMetaInstalledAt(dir); !legacy.IsZero() {
		return legacy.Local().Format("2006-01-02 15:04:05")
	}
	return legacyInstalledAt(exeInfo)
}

// readMetaInstalledAt 读取版本目录 meta.json 里的 installedAt（历史无 schema
// 账本与内核账本共用：artifact.Meta 兼容 RFC3339 与 "2006-01-02 15:04:05"
// 两种写法）；读不到返回零值。
func readMetaInstalledAt(dir string) time.Time {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return time.Time{}
	}
	var mm artifact.Meta
	if json.Unmarshal(raw, &mm) != nil {
		return time.Time{}
	}
	return mm.InstalledAt
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

// dirSize 版本目录整树字节和：主程序文件只是入口，多文件载荷才是体量的
// 主体，单报主 exe 尺寸与真实安装体量级失真。dirstats 度量恒跳过符号链接/
// 重解析点防环；2s 挂钟预算超限或度量失败回退旧口径（主程序文件大小）并
// Debug 报账，不谎报全量。
func dirSize(dir string, fallback int64) int64 {
	st := dirstats.MeasureBudgeted(dir, 2*time.Second)
	if st.Err != nil || st.Partial || st.Bytes <= 0 {
		slog.Debug("ddnsgo 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
