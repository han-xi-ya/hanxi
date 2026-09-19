package version

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"hanxi/internal/modules/rufus/instance"
	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
)

const (
	exeName = "rufus.exe" // 落盘定名（与版本无关）：ResolveExe 确定性寻径、进程名探测稳定

	// treeEntryName 版本树目录前缀（<root>/rufus_<version>，与历史布局 rufus_4.15 同构）。
	treeEntryName = "rufus"

	// iniFileName 上游便携模式开关：exe 同目录存在 rufus.ini（哪怕空文件）
	// 即全部设置落 ini 而非注册表（src/rufus.c 实证）。安装落位与首启兜底
	// 两处播种（种子内容与策略见 instance.SeedPortableSettings）。
	iniFileName = "rufus.ini"

	// moduleMetaFileName 模块侧账本补充：内核 meta.json 只承载通用字段，
	// RufusVersionInfo 的 Source（导入来源目录 / 远程资产名）与 IsImport 的
	// "来源明细"记在这里，与内核账本同目录共存、随事务 staging 原子落位。
	moduleMetaFileName = "meta.module.json"

	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 10 分钟口径）。
	fetchBudget = 10 * time.Minute
)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// plainVersionRe 纯版本号（如 4.15）：Rufus 上游自 1.x 起恒为两段式，用于目录校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// assetVerRe 官方资产文件名中的版本号段（rufus-4.15p.exe → 4.15）
var assetVerRe = regexp.MustCompile(`(?i)^rufus-?v?(\d+\.\d+)`)

// importCandidateRe 导入候选：官方形态单文件 exe（rufus.exe / rufus-4.15p.exe /
// rufus-4.15.exe）；.sig、_x86/_arm64 变体与自造命名（rufus-setup.exe 等）拒收。
var importCandidateRe = regexp.MustCompile(`(?i)^rufus(-\d[\w.]*)?\.exe$`)

// Manager Rufus 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 校验 →
// 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch + Tree）；
// 本包只保留 Rufus 领域知识：单文件便携 exe 形态筛选、镜像 URL 模板、
// vX.Y 版本形状、便携锚点（exe + rufus.ini 播种）、导入形态判别与本地导入，
// 以及既有进度词表（resolve/downloading/verify/install/done/error）映射。
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

// OpenTree 打开 Rufus 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]RufusRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描）。
// 目录命名 rufus_X.Y（当前落位格式）兼容 rufus_vX.Y（v 前缀历史形状）；
// 版本令牌仅接受 x.y 与 imported-时间戳 两种形状（外来目录不列入）；
// exe 缺失/为空视为损坏安装跳过。排序：数值分段降序，imported 兜底目录沉底。
func (m *Manager) ListInstalled() ([]RufusVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []RufusVersionInfo
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

		info := RufusVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    fi.Size(),
		}
		if v.Meta.Schema != 0 {
			// 内核账本可信：来源词汇（remote|imported）入账本 Meta.Source，
			// 来源明细（导入目录 / 远程资产名）读模块侧账本
			info.IsImport = v.Meta.Source == artifact.SourceImported
			info.Source = readModuleMeta(v.Dir).Source
			info.InstalledAt = formatInstalledAt(v.Meta.InstalledAt, v.Dir, fi)
		} else {
			// 迁移前的历史安装：无 schema 的旧 meta.json 逐字段回读，
			// 安装时间回退 exe 修改时间
			info.IsImport, info.Source = readLegacyImportMeta(v.Dir)
			info.InstalledAt = formatInstalledAt(time.Time{}, v.Dir, fi)
		}
		list = append(list, info)
	}
	sort.SliceStable(list, func(i, j int) bool {
		// imported- 兜底目录恒沉底：versioncmp 对非数字段退化字典序，
		// "imported-…" 会被误判为比 "4.x" 更新——冷启动回退"最新已装"绝不能选中它
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

// Download 下载便携单文件 exe 安装到 versions/rufus_X.Y/rufus.exe。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub release API 官方资产摘要
// （digest）为信任根做流式 + 落盘双 SHA-256 校验，Content-Length 与流式上限
// 双核（取代旧"落盘后字节数核对"），镜像只是同摘要的备用传输来源；落位经
// Tree.Commit（staging 独占 + 原子 rename，同版本异摘要防漂移）。本包保留
// 领域动作：MZ 魔数断言（镜像错误页伪装 exe 的最低防线）与便携开关播种。
// txnID 为调用方事务 ID（staging 目录 .tmp-<txnID> 由它派生，供 journal
// 背书与崩溃恢复定位现场）。
//
// 进度回调沿用本模块既有词表（resolve/downloading/verify/install/done/error），
// 不发明新词。onProgress 可选：实时上报各阶段进度。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应的远程资产（模块知识：GitHub 元数据与资产筛选）
	emit("resolve", 0, 0, "")
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *RufusRelease
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
	digest := rel.SHA256
	if digest == "" {
		err := fmt.Errorf("上游未提供 Rufus %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	// 2. 独占中转目录（staging 与最终目录同卷，供原子落位）；exe 直接
	// 定名 rufus.exe 落进 staging，无需系统临时区中转
	token := strings.TrimPrefix(version, "v")
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	defer discard() // 成功 Commit 后为 no-op；任一步失败不留半件
	stagedExe := filepath.Join(staging, exeName)

	// 3. 受控下载（主址 + 镜像逐个回退；下载/摘要/字节数双核全部委托内核）
	urls := m.mirrors(rel.Version, rel.AssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   digest,
		MaxBytes: rel.Size, // 与 release API 声明大小对齐：超限即断，杜绝异常放大
		FileName: rel.AssetName,
	}
	emit("downloading", 0, rel.Size, "")
	fetchErr := m.fetch(context.Background(), src, stagedExe, func(p artifact.Progress) {
		// 内核进度 → 既有词表：只有流式下载阶段对应 downloading，其余阶段本模块不上报
		if p.Stage == artifact.StageDownload {
			emit("downloading", p.Done, p.Total, "")
		}
	}, fetchBudget)
	if fetchErr != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", fetchErr))
		return fetchErr
	}

	// 4. 落地断言（模块策略，内核不感知）：MZ 魔数（错误页/残片伪装防线）
	emit("verify", 0, 0, "")
	if err := verifyPEMagic(stagedExe); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return err
	}
	// 便携开关播种：安装即落 rufus.ini（首启 instance 侧播种保留作历史目录兜底）
	if err := instance.SeedPortableSettings(staging); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("播种便携开关失败: %v", err))
		return err
	}
	// 来源明细入账（远程安装记资产名，与历史 meta.json 的 source 字段口径一致）
	if err := writeModuleMeta(staging, rel.AssetName, false); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("写入来源账目失败: %v", err))
		return err
	}

	// 5. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移）
	emit("install", 0, 0, "")
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   digest, // 单文件形态：包即资产，官方摘要同值
		AssetSHA256: fileSHA256(stagedExe),
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("安装失败: %v", err))
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

// ResolveExe 返回指定版本的 rufus.exe 路径（不存在返回错误）
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

// resolveVersionDir 定位版本隔离目录。优先规范名 rufus_X.Y（当前落位格式），
// 回退历史形状 rufus_vX.Y；均不存在时返回与原实现一致的引导文案。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(ver) && !importedDirRe.MatchString(ver) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	for _, t := range []string{ver, "v" + ver} {
		if d, rerr := m.tree.Resolve(t); rerr == nil {
			return d, t, nil
		}
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// versionFromToken 把 Tree 扫出的版本令牌规范化为 vX.Y（4.15/v4.15 → v4.15，
// imported-… → vimported-…）。仅接受 x.y 两段式与 imported-时间戳 两种形状，
// 与历史目录名解析口径一致（外来目录名被拒）。
func versionFromToken(token string) (string, bool) {
	rest := strings.TrimPrefix(strings.TrimSpace(token), "v")
	if plainVersionRe.MatchString(rest) || importedDirRe.MatchString(rest) {
		return "v" + rest, true
	}
	return "", false
}

// ImportLocal 导入本机已有的 Rufus 便携 exe（文件路径或所在目录均可）。
// 便携模式用户的个性化设置在同目录 rufus.ini 中（源码实证），源旁存在时
// 一并搬运保住配置；运行配置由托管侧 instance.SeedPortableSettings 兜底播种。
// 落位走 Tree.Commit（staging + 原子 rename，Source=imported 入账）。
// 调用方需先确保源实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(path string) (RufusVersionInfo, error) {
	srcExe, srcDir, err := resolveImportExe(path)
	if err != nil {
		return RufusVersionInfo{}, err
	}
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return RufusVersionInfo{}, fmt.Errorf("源文件不可用: %s", srcExe)
	}
	if err := verifyPEMagic(srcExe); err != nil {
		return RufusVersionInfo{}, fmt.Errorf("源文件不是有效的 Windows 可执行体: %w", err)
	}

	version := versionFromImportName(fi.Name())
	if version == "" {
		// 文件名无版本段（用户改名）：读 PE 版本资源；再失败时间戳兜底（与 rustdesk 同构）
		if fv, vErr := versioninfo.FileVersion(srcExe); vErr == nil {
			version = firstTwoSegments(fv)
		}
		if !plainVersionRe.MatchString(version) {
			version = "imported-" + time.Now().Format("20060102-150405")
		}
	}
	if _, rerr := m.tree.Resolve(version); rerr == nil {
		return RufusVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}

	staging, discard, err := m.tree.StageDir(fmt.Sprintf("imp-%s-%d", version, time.Now().UnixNano()))
	if err != nil {
		return RufusVersionInfo{}, err
	}
	defer discard() // 成功 Commit 后为 no-op；半途失败不留半件
	if err := copyFileTo(srcExe, filepath.Join(staging, exeName)); err != nil {
		return RufusVersionInfo{}, err
	}
	// 源旁便携配置一并搬运（存在即搬，尊重用户已有设置不覆盖不播种）
	if _, err := os.Stat(filepath.Join(srcDir, iniFileName)); err == nil {
		if err := copyFileTo(filepath.Join(srcDir, iniFileName), filepath.Join(staging, iniFileName)); err != nil {
			return RufusVersionInfo{}, err
		}
	}
	// 来源明细入账（导入装记录来源目录，与历史 meta.json 的 source 字段口径一致）
	if err := writeModuleMeta(staging, srcDir, true); err != nil {
		return RufusVersionInfo{}, err
	}

	meta := artifact.Meta{
		Entry:       exeName,
		AssetSHA256: fileSHA256(filepath.Join(staging, exeName)),
		Source:      artifact.SourceImported,
	}
	if err := m.tree.Commit(staging, version, meta); err != nil {
		return RufusVersionInfo{}, err
	}

	dir, _, err := m.resolveVersionDir("v" + version)
	if err != nil {
		return RufusVersionInfo{}, err
	}
	exePath := filepath.Join(dir, exeName)
	info := RufusVersionInfo{
		Version:  "v" + version,
		ExePath:  exePath,
		Dir:      dir,
		Size:     fi.Size(),
		Source:   srcDir,
		IsImport: true,
	}
	if mf, ferr := os.Stat(exePath); ferr == nil {
		info.InstalledAt = mf.ModTime().Format("2006-01-02 15:04:05")
	}
	if at := readMetaInstalledAt(dir); !at.IsZero() { // 内核账本 installedAt 为准（落位时刻）
		info.InstalledAt = at.Local().Format("2006-01-02 15:04:05")
	}
	return info, nil
}

// resolveImportExe 归一化导入入参：文件路径直接用（须为官方便携形态名）；
// 目录则在官方形态中挑选版本最大的 x64 exe。
// 返回 (exe 路径, exe 所在目录)——后者用于查找随行的 rufus.ini。
func resolveImportExe(path string) (string, string, error) {
	path = strings.TrimSpace(path)
	fi, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("路径不存在或不可访问: %s", path)
	}
	if !fi.IsDir() {
		if !importCandidateRe.MatchString(fi.Name()) {
			return "", "", fmt.Errorf("文件名不符合官方便携版形态（rufus.exe / rufus-版本p.exe）: %s", fi.Name())
		}
		lower := strings.ToLower(fi.Name())
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "_x86") {
			return "", "", fmt.Errorf("不支持非 x64 架构变体: %s", fi.Name())
		}
		return path, filepath.Dir(path), nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", "", err
	}
	var best, bestVer string
	for _, e := range entries {
		if e.IsDir() || !importCandidateRe.MatchString(e.Name()) {
			continue
		}
		lower := strings.ToLower(e.Name())
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "_x86") {
			continue
		}
		ver := versionFromImportName(e.Name())
		if best == "" || (ver != "" && (bestVer == "" || versioncmp.Compare(ver, bestVer) > 0)) ||
			(ver == "" && bestVer == "" && strings.ToLower(e.Name()) > strings.ToLower(best)) {
			best, bestVer = e.Name(), ver
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("目录中未找到官方形态的便携 exe（rufus[-版本].exe）: %s", path)
	}
	return filepath.Join(path, best), path, nil
}

// versionFromImportName 从官方资产文件名提取版本号（无则空串）
func versionFromImportName(name string) string {
	if mm := assetVerRe.FindStringSubmatch(name); len(mm) == 2 && plainVersionRe.MatchString(mm[1]) {
		return mm[1]
	}
	return ""
}

// firstTwoSegments 截取版本号前两段（Rufus FileVersion 为 "X.Y.BUILD.0" 四段，
// 含构建号——与两段式 tag 比对前只取 X.Y）；段非数字时原样返回交由正则校验拒绝。
func firstTwoSegments(fv string) string {
	parts := strings.Split(strings.TrimSpace(fv), ".")
	if len(parts) >= 2 {
		return strings.Join(parts[:2], ".")
	}
	return fv
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

// readLegacyImportMeta 回读迁移前旧账本（map 形态）里的 isImport/source 字段。
func readLegacyImportMeta(dir string) (bool, string) {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return false, ""
	}
	var mm map[string]any
	if json.Unmarshal(raw, &mm) != nil {
		return false, ""
	}
	isImport, _ := mm["isImport"].(bool)
	source, _ := mm["source"].(string)
	return isImport, source
}

// formatInstalledAt 安装时间展示（yyyy-MM-dd HH:mm:ss）：优先账本 installedAt
// （含历史 meta.json 的本地时区字符串写法，artifact.Meta 统一解析），无账本回退 exe 修改时间。
func formatInstalledAt(fromMeta time.Time, dir string, exeInfo os.FileInfo) string {
	if !fromMeta.IsZero() {
		return fromMeta.Local().Format("2006-01-02 15:04:05")
	}
	if legacy := readMetaInstalledAt(dir); !legacy.IsZero() {
		return legacy.Local().Format("2006-01-02 15:04:05")
	}
	if exeInfo != nil {
		return exeInfo.ModTime().Format("2006-01-02 15:04:05")
	}
	return ""
}

// readMetaInstalledAt 读取版本目录 meta.json 里的 installedAt（新内核账本与
// 无 schema 的历史账本共用：artifact.Meta 兼容两种时间写法）；读不到返回零值。
func readMetaInstalledAt(dir string) time.Time {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return time.Time{}
	}
	var mm artifact.Meta // UnmarshalJSON 兼容 RFC3339 与 "2006-01-02 15:04:05" 两种历史写法
	if json.Unmarshal(raw, &mm) != nil {
		return time.Time{}
	}
	return mm.InstalledAt
}
