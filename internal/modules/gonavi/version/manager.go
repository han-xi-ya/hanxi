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
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"
)

const (
	exeName = "GoNavi.exe" // zip 根平铺三条目：GoNavi.exe + LICENSE + NOTICE（实证布局）

	// treeEntryName 版本树目录前缀（<root>/gonavi_<version>）。
	treeEntryName = "gonavi"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源）。
	dirPrefix = treeEntryName + "_"
	// fetchBudget 单次下载的总超时预算（GoNavi 是 ~75MB 解压量的 Wails 单
	// exe，压缩包亦在数十 MB 级，沿用家族大包 10 分钟口径）。
	fetchBudget = 10 * time.Minute

	// moduleMetaFileName 模块侧账本（verifiedHash/导入来源等前端契约字段）。
	moduleMetaFileName = "meta.module.json"
)

// plainVersionRe 纯版本号（如 0.8.9），用于目录名与 FileVersion 校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// licenseNames Apache-2.0 合规伴生文件：解包保布局天然随版本目录保留；
// 导入链按白名单一并搬运（副本同样必须携带许可文本）。
var licenseNames = []string{"LICENSE", "NOTICE"}

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// sumsFetcher 备用摘要源（SHA256SUMS 清单）拉取接缝，默认 downloadSmall。
type sumsFetcher func(urls []string, maxBytes int64) ([]byte, error)

// Manager GoNavi 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 校验 →
// 解包 → 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch +
// UnpackZip + Tree）；本包只保留 GoNavi 领域知识：便携资产筛选、镜像 URL
// 模板、版本目录形状（x.y.z / imported-时间戳）、布局自检（GoNavi.exe 在
// 解压根）、本地导入白名单、账外漂移护栏与既有进度词表映射。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	fetch     fetcher
	mirrors   func(version, assetName string) []string
	fetchSums sumsFetcher
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		fetch:       artifact.Fetch,
		mirrors:     assetMirrors,
		fetchSums:   downloadSmall,
	}
}

// OpenTree 打开 GoNavi 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）。
// N13 形态标注：逐行回填 Form=hostedForm——缓存命中时 get 交回的是缓存
// 共享切片，先拷贝断开引用再回填（不污染缓存源，免与并发读互相踩踏）。
func (m *Manager) ListRemote() ([]GoNaviRelease, error) {
	list, err := remoteCache.get()
	if err != nil {
		return nil, err
	}
	out := make([]GoNaviRelease, len(list))
	copy(out, list)
	for i := range out {
		out[i].Form = hostedForm
	}
	return out, nil
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 gonavi_X.Y.Z（下载/导入的正规版本）或
// gonavi_imported-YYYYMMDD-HHMMSS（版本探测失败的导入兜底）；其余形状的
// 版本令牌不列入；exe 缺失或为空视为损坏安装跳过。
// 账外漂移护栏：对每个版本做 mtime 闸控的摘要复查（仅 exe 落位后有改动
// 迹象才复算全量哈希，成本纪律见 driftCheck 注释），漂移如实投影到
// HashDrifted/DriftNote——不自动处置，不撒谎。
func (m *Manager) ListInstalled() ([]GoNaviVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []GoNaviVersionInfo
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

		mm := readModuleMeta(v.Dir)
		info := GoNaviVersionInfo{
			Version:      version,
			ExePath:      exe,
			Dir:          v.Dir,
			Size:         dirSize(v.Dir, fi.Size()),
			SHA256:       v.Meta.AssetSHA256,
			VerifiedHash: mm.VerifiedHash,
			IsImport:     mm.IsImport,
			Source:       mm.Source,
		}
		if !v.Meta.InstalledAt.IsZero() {
			info.InstalledAt = v.Meta.InstalledAt.Local().Format("2006-01-02 15:04:05")
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		drifted, note := driftCheck(exe, fi, v.Meta)
		info.HashDrifted, info.DriftNote = drifted, note
		list = append(list, info)
	}
	return list, nil
}

// Download 便携 zip 下载并安装到 versions/gonavi_X.Y.Z/（旧调用面，
// 供版本包单测与非事务调用使用）。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装，可由事务 context 取消（P0 批 2b 生命周期）。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub release API 官方资产摘要
// （digest，备用官方 SHA256SUMS 清单）为信任根做流式 + 落盘双 SHA-256 校验，
// Content-Length 与流式上限双核（对齐"字节数 == release 声明 size"层），
// 镜像只是同摘要的备用传输来源；解包经 artifact.UnpackZip（ZipSlip/炸弹/
// CRC32 全量闸门），落位经 Tree.Commit（staging + 原子 rename，同版本异
// 摘要防漂移）。
// 进度回调沿用家族既有词表（downloading/extract/done/error）：verify
// （官方摘要双核）由内核折进 download 阶段，不造幻影步骤。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// onProgress 可选：实时上报各阶段进度（下载字节、解压落位）。
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
	var rel *GoNaviRelease
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
	// 官方摘要信任根：digest 主源（parse 层已剥前缀），缺失时经备用官方
	// SHA256SUMS 清单解析补齐；两路皆无摘要一律拒装，不做无校验安装。
	digest := rel.SHA256
	if digest == "" {
		if rel.SumsAsset == "" {
			err := fmt.Errorf("上游未提供 GoNavi %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
			emit("error", 0, 0, err.Error())
			return err
		}
		body, serr := m.fetchSums(m.mirrors(version, rel.SumsAsset), 256<<10)
		if serr != nil {
			err := fmt.Errorf("拉取官方摘要清单 %s 失败，拒绝无校验安装: %w", rel.SumsAsset, serr)
			emit("error", 0, 0, err.Error())
			return err
		}
		if digest, err = sumsDigest(body, rel.AssetName); err != nil {
			emit("error", 0, 0, err.Error())
			return err
		}
	}

	tmpZip, err := os.CreateTemp("", "hanxi-gonavi-*.zip")
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
		SHA256:   digest,
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
	// 4. 布局自检（模块策略，内核不感知）：GoNavi.exe 在解压根存在且非空
	// （实证 zip 根平铺 exe + LICENSE + NOTICE，Wails 单 exe 无二级目录）。
	if err := checkPortableLayout(staging); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	// exe 实测摘要入账（账外漂移护栏的比对锚点；避免每次扫描现场哈希）
	assetSHA, err := fileSHA256(filepath.Join(staging, exeName))
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("计算落位摘要失败: %v", err))
		return err
	}
	// 模块侧账本先写进 staging（随目录原子落位，杜绝"半件有内容无账"窗口）
	if err := writeModuleMeta(staging, moduleMeta{VerifiedHash: true}); err != nil {
		emit("error", 0, 0, fmt.Sprintf("写入校验账目失败: %v", err))
		return err
	}

	// 5. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移）
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   digest,
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

// ResolveExe 返回指定版本的 GoNavi.exe 绝对路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	exe := filepath.Join(dir, exeName)
	if fi, serr := os.Stat(exe); serr != nil || fi.Size() == 0 {
		return "", fmt.Errorf("版本 %s 安装损坏：未找到 %s", version, exeName)
	}
	return exe, nil
}

// PEVersion 返回版本 exe 的 PE 文件版本（供前端展示与漂移旁证；读不到返回空串）。
func (m *Manager) PEVersion(version string) string {
	exe, err := m.ResolveExe(version)
	if err != nil {
		return ""
	}
	fv, err := versioninfo.FileVersion(exe)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fv)
}

// VerifyLedger 账外漂移护栏只读复查（本模块特有硬要求）：将指定版本 exe 的
// 实测 sha256 与落位账本（meta.json assetSHA256）比对，返回是否漂移与如实
// 明细。ListInstalled 刷新内部走同一 driftCheck 路径；本方法供 service 层对
// active 版本做定向复查。成本控制：仅当 exe mtime 比落位记录新（有改动迹象）
// 才做 ~75MB 全量哈希，mtime 未变直接判"无漂移迹象"——旁证性护栏，不是
// 防篡改审计（touch 类掩盖不防，注释口径与返回值一致，不夸海口）。
// 漂移时不自动处置不撒谎：仅如实报告，处置决策留给用户。
func (m *Manager) VerifyLedger(version string) (drifted bool, note string) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return false, fmt.Sprintf("无法比对：%v", err)
	}
	exe := filepath.Join(dir, exeName)
	fi, serr := os.Stat(exe)
	if serr != nil {
		return false, fmt.Sprintf("无法比对：%s 不可读（安装疑似损坏）", exeName)
	}
	vers, verr := m.tree.Versions()
	if verr != nil {
		return false, fmt.Sprintf("无法比对：版本树扫描失败 %v", verr)
	}
	for _, v := range vers {
		if strings.EqualFold(v.Dir, dir) {
			return driftCheck(exe, fi, v.Meta)
		}
	}
	return false, "无法比对：账本不在版本树扫描结果内"
}

// driftCheck 漂移复查核心（ListInstalled 与 VerifyLedger 共用口径）：
// 账本无落位摘要（历史/异端安装）→ 如实报"无法比对"，不猜没猜；
// mtime 不晚于落位时刻 → 无改动迹象，不触发全量哈希；
// mtime 晚于落位时刻 → 复算比对（一致则报"复算一致"，不符即漂移）。
func driftCheck(exe string, fi os.FileInfo, meta artifact.Meta) (bool, string) {
	if meta.AssetSHA256 == "" {
		return false, "无法比对：账本缺落位摘要（历史或异端安装）"
	}
	landed := meta.InstalledAt
	if landed.IsZero() {
		return false, "无法比对：账本缺落位时间戳"
	}
	if !fi.ModTime().After(landed) {
		return false, "复算未触发：exe 自落位后无改动迹象"
	}
	if err := verifySHA256(exe, meta.AssetSHA256); err != nil {
		return true, fmt.Sprintf("账外漂移：%v", err)
	}
	return false, "复算一致：改动迹象经全量哈希排除"
}

// resolveVersionDir 定位版本隔离目录（gonavi_X.Y.Z 或 gonavi_imported-时间戳）。
// 形状外令牌（含路径穿越）先于任何磁盘访问被拒，错误口径与家族一致。
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

// versionFromToken 把 Tree 扫出的版本令牌规范化为 vX.Y.Z / vimported-<时间戳>。
// 仅接受纯 x.y.z 或 imported-时间戳形状，gonavi_vX.Y.Z 之类带 v 前缀的
// 历史/外来目录名不列入。
func versionFromToken(token string) (string, bool) {
	if plainVersionRe.MatchString(token) || importedDirRe.MatchString(token) {
		return "v" + token, true
	}
	return "", false
}

// checkPortableLayout 便携布局自检（模块策略）：staging 根内 GoNavi.exe
// 存在且非空（实证 zip 根平铺，exe 相对路径恒 GoNavi.exe）。不符即判定
// 安装无效，调用方丢弃 staging。LICENSE/NOTICE 缺失不拒装（上游惯例可能
// 调整，核心不变式只有 exe）。
func checkPortableLayout(staging string) error {
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	}
	return nil
}

// ---------- 本地导入（GoNavi 领域流程，无内核对应物） ----------

// ImportLocal 导入本地已有的 GoNavi 便携安装（探测 GoNavi.exe，版本读 PE
// FileVersion，platform/versioninfo 复用）。白名单搬运 exe + LICENSE/NOTICE
// （Apache-2.0 副本必须随附许可文本），其余文件（配置/缓存一概在别处）
// 不搬。调用方需先确保源实例未运行（防御性约定）。
func (m *Manager) ImportLocal(srcDir string) (GoNaviVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return GoNaviVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return GoNaviVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	stagingID := "import-" + version + "-" + time.Now().Format("150405")
	staging, discard, err := m.tree.StageDir(stagingID)
	if err != nil {
		return GoNaviVersionInfo{}, err
	}
	defer discard()

	// 白名单迁移：exe 必搬 + 许可文本在则搬。其余文件一概不搬。
	copied := []string{exeName}
	if err := copyFileTo(srcExe, filepath.Join(staging, exeName)); err != nil {
		return GoNaviVersionInfo{}, err
	}
	for _, name := range licenseNames {
		src, serr := os.Stat(filepath.Join(srcDir, name))
		if serr != nil || src.IsDir() {
			continue
		}
		if err := copyFileTo(filepath.Join(srcDir, name), filepath.Join(staging, name)); err != nil {
			return GoNaviVersionInfo{}, err
		}
		copied = append(copied, name)
	}

	// exe 实测摘要入账（漂移护栏锚点）；导入链无官方摘要参照，
	// verifiedHash 如实记 false。
	assetSHA, err := fileSHA256(filepath.Join(staging, exeName))
	if err != nil {
		return GoNaviVersionInfo{}, fmt.Errorf("计算导入摘要失败: %w", err)
	}
	if err := writeModuleMeta(staging, moduleMeta{
		IsImport: true, Source: srcDir, Copied: strings.Join(copied, ", "),
	}); err != nil {
		return GoNaviVersionInfo{}, err
	}
	meta := artifact.Meta{Entry: exeName, AssetSHA256: assetSHA, Source: artifact.SourceImported}
	if err := m.tree.Commit(staging, version, meta); err != nil {
		return GoNaviVersionInfo{}, err
	}

	return GoNaviVersionInfo{
		Version:      "v" + version,
		ExePath:      filepath.Join(targetDir, exeName),
		Dir:          targetDir,
		Size:         dirSize(targetDir, fi.Size()),
		InstalledAt:  time.Now().Format("2006-01-02 15:04:05"),
		IsImport:     true,
		Source:       srcDir,
		SHA256:       assetSHA,
		VerifiedHash: false,
	}, nil
}

// ---------- 账本与文件工具 ----------

// moduleMeta 模块侧账本（内核 artifact.Meta 之外的 GoNavi 领域字段）。
type moduleMeta struct {
	IsImport     bool   `json:"isImport,omitempty"`
	Source       string `json:"source,omitempty"`
	Copied       string `json:"copied,omitempty"`
	VerifiedHash bool   `json:"verifiedHash,omitempty"`
}

func writeModuleMeta(dir string, mm moduleMeta) error {
	data, err := json.MarshalIndent(mm, "", "  ")
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

// dirSize 版本目录整树字节和：GoNavi 解压 ~75MB 几乎全在单 exe，但许可文本
// 等伴生条目一并计入才是真实安装体量。dirstats 度量恒跳过符号链接/重解析点
// 防环；2s 挂钟预算超限或度量失败回退旧口径（主程序文件大小）并 Debug 报账，
// 不谎报全量。
func dirSize(dir string, fallback int64) int64 {
	st := dirstats.MeasureBudgeted(dir, 2*time.Second)
	if st.Err != nil || st.Partial || st.Bytes <= 0 {
		slog.Debug("gonavi 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
