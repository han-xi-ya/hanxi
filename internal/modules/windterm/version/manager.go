// Package version 实现 WindTerm 版本管理：GitHub releases 远程列表、
// 便携 zip"下载 → 校验 → 解包 → 落位"主流程（委托 Wave 4 共享内核
// packages/go/artifact 的 UnpackZip/Tree 闸门；下载段按信任根分治）与
// 本地导入/卸载/解析。
//
// 完整性策略（vscode 历史版无官方摘要降级先例，ADR-0002 §5 同款分治）：
// 上游 releases 全部资产无 GitHub digest（2026-09-23 API 实测），无信任根
// 可用——下载委托模块 bespoke 链（多镜像回退 + 重试 + 流式字节上限），
// 随后字节数核对（层 2）+ artifact.UnpackZip 的 ZipSlip/炸弹/CRC32 全量
// 闸门（层 3）+ 便携布局自检（层 4，本包领域知识）；自算 zip/exe 摘要
// 落账本供重装比对（同版本异摘要防内容漂移由 Tree.Commit 收口）。
// meta 记 verifiedHash=false 供 UI 如实展示。若上游未来回填 digest，
// remoteCache.digest 在场即自动升格内核 artifact.Fetch 官方摘要双核主流程。
//
// zip 布局领域知识（实测 2.7.0）：包根为 WindTerm_X.Y.Z/ 目录包裹，主程序
// WindTerm.exe（asInvoker manifest，无提权要求）位于该目录内；托管保持
// 原布局不解套（ResolveExe 按不变式 `*/WindTerm.exe` 定位），版本隔离目录
// 仍为 windterm_X.Y.Z/，portable 数据随主程序目录自包含。
package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"

	"hanxi/packages/go/netx"
)

const (
	exeName = "WindTerm.exe"

	// treeEntryName 版本树目录前缀（<root>/windterm_<version>）。
	treeEntryName = "windterm"
	// fetchBudget 单次下载的总超时预算（33MB 便携包，10 分钟口径与家族一致）。
	fetchBudget = 10 * time.Minute
	// moduleMetaFileName 模块侧账本（verifiedHash/computedSHA 等前端契约与
	// 诊断字段，内核 meta.json 不收的部分）。
	moduleMetaFileName = "meta.module.json"
)

// Manager WindTerm 版本管理引擎。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	// digestFetcher 官方摘要主链接缝（默认内核 artifact.Fetch：摘要必检 +
	// 镜像回退 + 流式上限）；bespoke 降级链走 client。失败注入测试替换。
	digestFetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error
	mirrors       func(tag, assetName string) []string
	client        *http.Client // 降级链下载客户端（长超时：33MB 便携包）
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir:   versionsDir,
		tree:          OpenTree(versionsDir),
		digestFetcher: artifact.Fetch,
		mirrors:       githubMirrors,
		client:        netx.NewClient(15*time.Minute, nil),
	}
}

// OpenTree 打开 WindTerm 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）。
func (m *Manager) ListRemote() ([]WindTermRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已装版本（Tree 目录扫描 + payload exe 定位；
// 主程序缺失/为空视为损坏安装跳过）。
func (m *Manager) ListInstalled() ([]WindTermVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []WindTermVersionInfo
	for _, v := range vers {
		exe, ok := findPayloadExe(v.Dir)
		if !ok {
			continue
		}
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.Size() == 0 {
			continue
		}
		info := WindTermVersionInfo{
			Version: normalizeVersion(v.Version),
			ExePath: exe,
			Dir:     v.Dir,
			Size:    fi.Size(),
			SHA256:  v.Meta.AssetSHA256,
			Source:  v.Meta.Source,
		}
		if info.SHA256 == "" {
			info.SHA256 = fileSHA256(exe) // 历史/导入账无 exe 摘要：现场回算
		}
		mm := readModuleMeta(v.Dir)
		info.VerifiedHash = mm.VerifiedHash
		info.IsImport = mm.IsImport
		if mm.IsImport {
			info.Source = mm.Source
		}
		info.InstalledAt = formatInstalledAt(v.Meta.InstalledAt, v.Dir, exe)
		list = append(list, info)
	}
	return list, nil // Tree.Versions 契约：已按版本号降序（最新在前）
}

// Download 旧调用面（非事务/单测）：委托 DownloadContext。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装便携 zip，可由事务 context 取消（P0 批 2b）。
// 取消边界：传输与解包全程 ctx 感知即时中止；落位为 Tree.Commit 原子 rename，
// 不存在半途目录。进度词表与家族一致（downloading/verify/extract/done/error）。
func (m *Manager) DownloadContext(ctx context.Context, txnID, version string, onProgress func(p DownloadProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	version = normalizeVersion(version)
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *WindTermRelease
	for i := range releases {
		if normalizeVersion(releases[i].Version) == version {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		err := fmt.Errorf("远程列表不存在版本 %s", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmp, err := os.CreateTemp("", "hanxi-windterm-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	tmp.Close()

	token := strings.TrimPrefix(version, "v")
	urls := m.mirrors(token, rel.AssetName)
	digest := remoteCache.digest(rel.Version)

	verified := false
	if digest != "" {
		// 信任根在场（上游回填 digest 后自动启用）：内核 Fetch 流式+落盘双核。
		emit("downloading", 0, rel.Size, "")
		src := artifact.Source{
			URL:      urls[0],
			Mirrors:  urls[1:],
			SHA256:   digest,
			MaxBytes: rel.Size,
			FileName: rel.AssetName,
		}
		if ferr := m.digestFetcher(ctx, src, tmpPath, func(p artifact.Progress) {
			if p.Stage == artifact.StageDownload {
				emit("downloading", p.Done, p.Total, "")
			}
		}, fetchBudget); ferr != nil {
			emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", ferr))
			return ferr
		}
		verified = true
	} else {
		// 无官方摘要（上游现状）：bespoke 多镜像链 + 流式上限（防异常放大）。
		emit("downloading", 0, rel.Size, "")
		if derr := downloadWithMirrors(ctx, m.client, urls, tmpPath, rel.Size, func(done int64) {
			emit("downloading", done, rel.Size, "")
		}); derr != nil {
			emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", derr))
			return derr
		}
	}

	// 层 2：字节数核对（Fetch 路径内核已双核，此处覆盖降级链）
	actual, err := fileSize(tmpPath)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if rel.Size > 0 && actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// verify 阶段保留进度叙事（词表家族统一）：官方摘要已在内核复核，
	// 降级链在此落自算摘要账（computedSHA），供重装比对与如实展示。
	emit("verify", 0, 0, "")
	computedZipSHA := fileSHA256(tmpPath)

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
	if err := artifact.UnpackZipContext(ctx, tmpPath, staging, artifact.DefaultLimits, nil); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}
	// 层 4：便携布局自检（模块策略，内核不感知）：WindTerm.exe 按不变式
	// （根目录包裹或平铺）在场且非空。不符即丢弃 staging，最终目录不出现半件。
	exe, ok := findPayloadExe(staging)
	if !ok {
		err := fmt.Errorf("zip 布局无效：包内未找到 %s（预期根目录 WindTerm_*/%s）", exeName, exeName)
		emit("error", 0, 0, err.Error())
		return err
	}
	if fi, serr := os.Stat(exe); serr != nil || fi.Size() == 0 {
		err := fmt.Errorf("zip 布局无效：%s 缺失或为空", exeName)
		emit("error", 0, 0, err.Error())
		return err
	}

	if err := writeModuleMeta(staging, moduleMeta{
		Source:       rel.AssetName,
		VerifiedHash: verified,
		ZipSize:      actual,
		ComputedSHA:  computedZipSHA,
	}); err != nil {
		emit("error", 0, 0, fmt.Sprintf("写入来源账目失败: %v", err))
		return err
	}
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   digest, // 官方包摘要（降级链如实留空，computedSHA 见模块账本）
		AssetSHA256: fileSHA256(exe),
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("落位失败: %v", err))
		return err
	}

	emit("done", 100, 100, "")
	return nil
}

// ImportLocal 把本机已有 WindTerm 目录（官方便携解压目录或旧托管搬运件）
// 收纳为托管版本：整树复制保数据（会话/密钥索引随目录走，导入即"你的文件
// 还是你的"），剥离运行噪声（logs/）与旧账本文件。版本令牌取主程序 PE 文件
// 版本（形如 2.7.0），取不到按 imported-时间戳 落目录。复制进树根 staging
// 后走 Tree.Commit 原子落位（与远程安装同一账本通道，不留"有内容无账"黑户）。
func (m *Manager) ImportLocal(srcDir string) (WindTermVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	exe, ok := findPayloadExe(srcDir)
	if !ok {
		return WindTermVersionInfo{}, fmt.Errorf("导入失败：%s（含 WindTerm_* 子目录）内未找到可用的 %s", srcDir, exeName)
	}
	payloadRoot := filepath.Dir(exe) // 复制单元：含主程序的那一层目录整体
	fi, err := os.Stat(exe)
	if err != nil || fi.Size() == 0 {
		return WindTermVersionInfo{}, fmt.Errorf("%s 不可用（缺失或空文件）", exeName)
	}

	token := importToken(exe)
	if err := artifact.ValidateVersionToken(token); err != nil {
		return WindTermVersionInfo{}, err
	}
	stagingID := "import-" + token + "-" + time.Now().Format("150405")
	staging, discard, err := m.tree.StageDir(stagingID)
	if err != nil {
		return WindTermVersionInfo{}, err
	}
	defer discard() // Commit 成功后为 no-op
	if err := copyTreeExcept(payloadRoot, staging, []string{"logs", "meta.json", moduleMetaFileName}); err != nil {
		return WindTermVersionInfo{}, fmt.Errorf("迁移 WindTerm 目录失败: %w", err)
	}
	dstExe, ok := findPayloadExe(staging)
	if !ok {
		return WindTermVersionInfo{}, fmt.Errorf("导入收口异常：中转目录内找不到 %s", exeName)
	}
	if err := writeModuleMeta(staging, moduleMeta{Source: srcDir, IsImport: true}); err != nil {
		return WindTermVersionInfo{}, fmt.Errorf("写入导入账目失败: %v", err)
	}
	meta := artifact.Meta{
		Entry:       exeName,
		AssetSHA256: fileSHA256(dstExe),
		Source:      artifact.SourceImported,
	}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		return WindTermVersionInfo{}, err
	}
	targetDir := filepath.Join(m.versionsDir, treeEntryName+"_"+token)
	dstRelInStaging, _ := filepath.Rel(staging, dstExe) // payload 可能嵌一层 WindTerm_*/

	return WindTermVersionInfo{
		Version:     normalizeVersion(token),
		ExePath:     filepath.Join(targetDir, filepath.Dir(dstRelInStaging), exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		SHA256:      meta.AssetSHA256,
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// Remove 卸载指定版本（委托 Tree：rename 隔离后删除，文件占用时留下可恢复状态）。
func (m *Manager) Remove(version string) error {
	_, token, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return m.tree.Remove(token, nil)
}

// ResolveExe 返回指定版本的主程序绝对路径（版本目录或 payload 包裹层均可解析）。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	exe, ok := findPayloadExe(dir)
	if !ok {
		return "", fmt.Errorf("版本 %s 安装损坏：未找到 %s", normalizeVersion(version), exeName)
	}
	return exe, nil
}

// resolveVersionDir 定位版本隔离目录（令牌含/不含 v 前缀均可）。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	ver := strings.TrimPrefix(strings.TrimSpace(version), "v")
	for _, t := range []string{ver, "v" + ver} {
		if d, rerr := m.tree.Resolve(t); rerr == nil {
			return d, t, nil
		}
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载", normalizeVersion(version))
}

// ---------- 领域工具 ----------

// normalizeVersion 版本展示归一：补 v 前缀（v2.7.0），imported-* 原样。
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") || !strings.Contains(v, ".") {
		return v
	}
	return "v" + v
}

// findPayloadExe 在版本目录内按不变式定位 WindTerm.exe：平铺（dir 直系，
// 导入件常见）或根目录包裹（WindTerm_*/WindTerm.exe，官方 zip 实测形态）；
// 包裹目录名随版本漂移（上游 tag 与资产名错位先例）故按通配 + 字典序取首。
func findPayloadExe(dir string) (string, bool) {
	if flat := filepath.Join(dir, exeName); regular(flat) {
		return flat, true
	}
	matches, err := filepath.Glob(filepath.Join(dir, "WindTerm_*", exeName))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	return matches[0], true
}

func regular(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// importToken 导入版本令牌：PE 文件版本（2.7.0.0 归一 2.7.0）优先，
// 兜底 imported-时间戳（不猜版本，绝不与真实版本号撞令牌）。
func importToken(exe string) string {
	if fv, err := versioninfo.FileVersion(exe); err == nil {
		if ver, ok := normalizeFileVersion(fv); ok {
			return ver
		}
	}
	return "imported-" + time.Now().Format("20060102150405")
}

// normalizeFileVersion PE 版本串（可能带空白/尾点，段数 3~4）归一为
// x.y.z；形状不符返回 ok=false。
func normalizeFileVersion(fv string) (string, bool) {
	ver := strings.TrimRight(strings.TrimSpace(fv), ".")
	parts := strings.Split(ver, ".")
	if len(parts) < 3 || len(parts) > 4 {
		return "", false
	}
	for _, p := range parts {
		if !isDigits(p) {
			return "", false
		}
	}
	return strings.Join(parts[:3], "."), true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// formatInstalledAt 安装时间展示：优先账本 installedAt，无账回退 exe 修改时间。
func formatInstalledAt(fromMeta time.Time, dir, exe string) string {
	if !fromMeta.IsZero() {
		return fromMeta.Local().Format("2006-01-02 15:04:05")
	}
	if fi, err := os.Stat(exe); err == nil {
		return fi.ModTime().Format("2006-01-02 15:04:05")
	}
	return ""
}

// ---------- 账本 ----------

// moduleMeta 模块侧账本（meta.module.json）：verifiedHash 如实标注是否经
// 官方摘要校验（上游无 digest，远程安装恒 false——降级三层的诚实呈现）；
// zipSize/computedSHA 为降级链自算基线；isImport/source 承载导入来源明细。
type moduleMeta struct {
	Source       string `json:"source,omitempty"`
	IsImport     bool   `json:"isImport,omitempty"`
	VerifiedHash bool   `json:"verifiedHash,omitempty"`
	ZipSize      int64  `json:"zipSize,omitempty"`
	ComputedSHA  string `json:"computedSHA,omitempty"`
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

// copyTreeExcept 整树复制（跳过顶层 skip 名单条目），导入链专用。
func copyTreeExcept(src, dst string, skipTop []string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0755)
		}
		top := strings.Split(filepath.ToSlash(rel), "/")[0]
		for _, s := range skipTop {
			if strings.EqualFold(top, s) {
				return nil
			}
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0700)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
