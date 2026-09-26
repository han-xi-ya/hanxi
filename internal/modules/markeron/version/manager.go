package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"
)

const (
	exeName          = "MarkerOn.exe"
	portableMarkName = "markeron.portable" // 0 字节标记：与 exe 同目录即激活便携模式

	// treeEntryName 版本树目录前缀（<root>/markeron_<version>，与历史布局 markeron_2.9.4 一致）。
	treeEntryName = "markeron"
	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 10 分钟口径）。
	fetchBudget = 10 * time.Minute
)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager MarkerOn 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 校验 → 解包 →
// 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch + UnpackZip + Tree）；
// 本包只保留 MarkerOn 领域知识：便携资产筛选、镜像 URL 模板、vX.Y.Z 版本形状、
// 便携锚点（exe + markeron.portable）布局自检与既有进度词表映射。
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
		mirrors:     githubMirrors,
	}
}

// OpenTree 打开 MarkerOn 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]MarkerRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 markeron_vX.Y.Z（历史遗留）或 markeron_X.Y.Z（当前落位格式）；
// 仅接受 vX.Y.Z 形状版本令牌（markeron_imported-x 等外来目录不列入）；
// exe 缺失/为空或便携标记缺失均视为损坏安装跳过。
func (m *Manager) ListInstalled() ([]MarkerVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []MarkerVersionInfo
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
		// 便携标记与 exe 同目录是便携模式激活的硬条件，缺失视为安装损坏
		if _, statErr := os.Stat(filepath.Join(v.Dir, portableMarkName)); statErr != nil {
			continue
		}

		info := MarkerVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    dirSize(v.Dir, fi.Size()),
			SHA256:  v.Meta.AssetSHA256,
		}
		// exe 诊断哈希：新安装读落位账本，历史安装（无账本摘要）回退现场计算
		if info.SHA256 == "" {
			info.SHA256, _ = fileSHA256(exe)
		}
		info.InstalledAt = formatInstalledAt(v.Meta.InstalledAt, v.Dir)
		list = append(list, info)
	}
	return list, nil
}

// Download 下载便携 zip 并安装到 versions/markeron_X.Y.Z/。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub API 官方资产摘要（digest）
// 为信任根做流式 + 落盘双 SHA-256 校验，Content-Length 与流式上限双核，
// 镜像只是同摘要的备用传输来源；解包经 artifact.UnpackZip（ZipSlip/炸弹/CRC32
// 全量闸门），落位经 Tree.Commit（staging + 原子 rename，同版本异摘要防漂移）。
// 进度回调沿用本模块既有词表（downloading/extract/done/error），不发明新词。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// onProgress 可选：实时上报各阶段进度（下载字节、解压落位）。
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
	var rel *MarkerRelease
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
	// 官方摘要信任根：GitHub release API 的 asset.digest（"sha256:<hex>"）。
	// 上游虽无独立 checksums 资产，API 摘要同样权威——缺失一律拒装，不做无校验安装。
	digest := remoteCache.digest(version)
	if digest == "" {
		err := fmt.Errorf("上游未提供 MarkerOn %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-markeron-*.zip")
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
	// 4. 便携锚点布局自检（模块策略，内核不感知）：exe 非空 + 便携标记存在
	if err := checkPortableLayout(staging); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	// exe 诊断摘要入账（ListInstalled 展示用，避免每次扫描现场哈希）
	assetSHA, _ := fileSHA256(filepath.Join(staging, exeName))

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

// ResolveExe 返回指定版本的 MarkerOn.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录。优先规范名 markeron_X.Y.Z（当前落位格式），
// 回退历史格式 markeron_vX.Y.Z；均不存在时返回与原实现一致的引导文案。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	for _, t := range []string{ver, "v" + ver} {
		if d, rerr := m.tree.Resolve(t); rerr == nil {
			return d, t, nil
		}
	}
	return "", "", fmt.Errorf("版本 v%s 未安装，请先在下方版本管理下载", ver)
}

// versionFromToken 把 Tree 扫出的版本令牌规范化为 v.X.Y.Z（2.9.4/v2.9.4 → v2.9.4）。
// 仅接受纯 x.y.z 形状，markeron_imported-x 等外来目录名被拒（与原目录名解析口径一致）。
func versionFromToken(token string) (string, bool) {
	rest := strings.TrimPrefix(strings.TrimSpace(token), "v")
	if rest == "" {
		return "", false
	}
	if plainVersionRe.MatchString(rest) {
		return "v" + rest, true // 规范化为 vX.Y.Z，与远程 ListReleases 保持一致
	}
	return "", false
}

// checkPortableLayout 便携锚点自检（模块策略）：staging 内 MarkerOn.exe 存在且非空、
// markeron.portable 标记存在（0 字节合法）。不符即判定安装无效，调用方丢弃 staging。
func checkPortableLayout(staging string) error {
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	}
	if _, err := os.Stat(filepath.Join(staging, portableMarkName)); err != nil {
		return fmt.Errorf("zip 布局无效：缺少 %s 便携标记", portableMarkName)
	}
	return nil
}

// formatInstalledAt 安装时间展示（yyyy-MM-dd HH:mm:ss）：优先账本 installedAt
// （含历史 meta.json 的本地时区字符串写法，artifact.Meta 统一解析），无账本回退 exe 修改时间。
func formatInstalledAt(fromMeta time.Time, dir string) string {
	if !fromMeta.IsZero() {
		return fromMeta.Local().Format("2006-01-02 15:04:05")
	}
	if legacy := readLegacyMetaInstalledAt(dir); !legacy.IsZero() {
		return legacy.Local().Format("2006-01-02 15:04:05")
	}
	if fi, err := os.Stat(filepath.Join(dir, exeName)); err == nil {
		return fi.ModTime().Format("2006-01-02 15:04:05")
	}
	return ""
}

// readLegacyMetaInstalledAt 读取无 schema 的历史 meta.json（迁移前安装）里的
// installedAt；仅诊断展示用途，读不到返回零值。
func readLegacyMetaInstalledAt(dir string) time.Time {
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

// plainVersionRe 纯版本号（如 2.9.4），用于识别版本隔离目录名
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// ---------- 领域知识（不进内核的部分） ----------

// githubMirrors 构造直连与镜像下载 URL 候选列表（首个为主址）。
// 路径模板对任意 owner/repo 泛化，与 frpc 模块共用同一组镜像前缀。
func githubMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// fileSHA256 计算文件全量 SHA-256（十六进制小写）。
// 注意：仅用于诊断展示链（exe 自哈希入账与历史安装回退显示），不参与下载校验主流程
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

// dirSize 版本目录整树字节和：主程序文件只是入口，多文件载荷才是体量的
// 主体，单报主 exe 尺寸与真实安装体量级失真。dirstats 度量恒跳过符号链接/
// 重解析点防环；2s 挂钟预算超限或度量失败回退旧口径（主程序文件大小）并
// Debug 报账，不谎报全量。
func dirSize(dir string, fallback int64) int64 {
	st := dirstats.MeasureBudgeted(dir, 2*time.Second)
	if st.Err != nil || st.Partial || st.Bytes <= 0 {
		slog.Debug("markeron 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
