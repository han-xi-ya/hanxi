// Package version 实现 Termora 版本管理：GitHub releases 远程列表、
// 便携 zip"下载 → 校验 → 解包 → 落位"主流程（全链委托 Wave 4 共享内核
// packages/go/artifact）与本地导入/卸载/解析。
//
// 完整性策略（markeron/ccswitch 官方摘要主链，非降级链）：上游全部资产
// 带 GitHub digest（2026-09-23 API 实测），信任根在场——artifact.Fetch
// 流式 + 落盘双 SHA-256 必检，缺摘要一律拒装（不做无校验安装）；解包过
// artifact.UnpackZip 的 ZipSlip/炸弹/CRC32 全量闸门，落位过 Tree.Commit
// 原子 rename（同版本同摘要幂等、异摘要防漂移）。
//
// zip 布局领域知识（实测 2.0.0-beta.16，jpackage app-image）：包根固定
// 名 `Termora/`（无版本后缀！多版本共存由托管隔离目录名区分），内含
// Termora.exe（启动器，asInvoker）+ app/（jar 与配置）+ runtime/（自带
// JRE，零系统 Java 依赖）。托管保持原布局不解套；portable 数据激活器为
// 与 exe 同级的 data\ 目录（上游 Application.getBaseDataDir 实证：
// Windows 绿色版检测 appPath 同级 data 存在即自包含），缺失时上游回落
// ~/.termora——托管下载/导入/启动前统一补齐（见 instance 与 service）。
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
	exeName       = "Termora.exe"
	payloadDir    = "Termora" // jpackage 包根固定名（zip 内与落位后一致）
	dataDirName   = "data"    // 官方 portable 激活器：与 exe 同级存在即数据自包含
	treeEntryName = "termora" // 版本树目录前缀（<root>/termora_<version>）

	// fetchBudget 单次下载的总超时预算（75MB 便携包含 JRE，15 分钟口径）
	fetchBudget = 15 * time.Minute

	// moduleMetaFileName 模块侧账本（导入来源明细，内核 meta.json 不收）
	moduleMetaFileName = "meta.module.json"

	// dirSizeBudget 版本目录整树度量的挂钟预算：托管版本目录常规数百 MB/数千
	// 文件，正常毫秒级完成；超限视为异常现场（巨日志/磁盘卡死），截断即回退
	// 主程序文件大小旧口径并 Debug 报账，不拖慢 ListInstalled 同步链路。
	dirSizeBudget = 2 * time.Second
)

// Manager Termora 版本管理引擎。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	fetch   func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error
	mirrors func(tag, assetName string) []string
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		fetch:       artifact.Fetch,
		mirrors:     githubMirrors,
	}
}

// OpenTree 打开 Termora 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存；stable+beta 全量，
// IsPre 如实透出）。
// N13 形态标注：逐行回填 Form=hostedForm——缓存命中时 get 交回的是缓存
// 共享切片，先拷贝断开引用再回填（不污染缓存源，免与并发读互相踩踏）。
func (m *Manager) ListRemote() ([]TermoraRelease, error) {
	list, err := remoteCache.get()
	if err != nil {
		return nil, err
	}
	out := make([]TermoraRelease, len(list))
	copy(out, list)
	for i := range out {
		out[i].Form = hostedForm
	}
	return out, nil
}

// ListInstalled 扫描本地已装版本（Tree 目录扫描 + payload exe 定位；
// 主程序缺失/为空视为损坏安装跳过）。
//
// 排序注记：Tree.Versions 的版本降序对 x.y.z-beta.N 形状会退化为字典序
// （versioncmp 非规范段兜底），"beta.9 > beta.16" 类罕见错序仅作展示/
// 回退启发用——activeVersion 显式设定是主路径，正确性不依赖此排序。
func (m *Manager) ListInstalled() ([]TermoraVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []TermoraVersionInfo
	for _, v := range vers {
		exe, ok := findPayloadExe(v.Dir)
		if !ok {
			continue
		}
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.Size() == 0 {
			continue
		}
		info := TermoraVersionInfo{
			Version: normalizeVersion(v.Version),
			ExePath: exe,
			Dir:     v.Dir,
			Size:    dirSize(v.Dir, fi.Size()),
			SHA256:  v.Meta.AssetSHA256,
			Source:  v.Meta.Source,
		}
		if info.SHA256 == "" {
			info.SHA256 = fileSHA256(exe) // 账无 exe 摘要：现场回算
		}
		mm := readModuleMeta(v.Dir)
		info.IsImport = mm.IsImport
		if mm.IsImport {
			info.Source = mm.Source
			info.VerifiedHash = false // 导入件无官方摘要可核，如实 false
		} else {
			info.VerifiedHash = true // 远程链内核摘要必检（缺摘要根本装不进）
		}
		info.InstalledAt = formatInstalledAt(v.Meta.InstalledAt, exe)
		list = append(list, info)
	}
	return list, nil
}

// Download 旧调用面（非事务/单测）：委托 DownloadContext。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装便携 zip，可由事务 context 取消（P0 批 2b）。
// 取消边界：内核 Fetch 与解包全程 ctx 感知即时中止；落位为 Tree.Commit
// 原子 rename，不存在半途目录。进度词表 downloading/extract/done/error
// （verify 由内核 Fetch 折进 download，不造幻影步骤——markeron 同口径）。
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
	var rel *TermoraRelease
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
	digest := remoteCache.digest(rel.Version)
	if digest == "" {
		err := fmt.Errorf("上游未提供 Termora %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmp, err := os.CreateTemp("", "hanxi-termora-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	tmp.Close()

	tag := strings.TrimPrefix(version, "v")
	urls := m.mirrors(tag, rel.AssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:], // 镜像只是同摘要的备用传输来源，不信任其本身
		SHA256:   digest,
		MaxBytes: rel.Size,
		FileName: rel.AssetName,
	}
	emit("downloading", 0, rel.Size, "")
	if ferr := m.fetch(ctx, src, tmpPath, func(p artifact.Progress) {
		if p.Stage == artifact.StageDownload {
			emit("downloading", p.Done, p.Total, "")
		}
	}, fetchBudget); ferr != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", ferr))
		return ferr
	}

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
	// 布局自检（模块策略）：Termora/Termora.exe 在场非空 + app/ 目录存在
	// （jpackage 三件套之两件套锚点；runtime/ 缺失装上也起不动，但那是
	// 上游发布事故，字节数+摘要层已兜传输完整性，此处不重复设卡）。
	exe, ok := findPayloadExe(staging)
	if !ok {
		err := fmt.Errorf("zip 布局无效：缺少 %s/%s（jpackage app-image 形态）", payloadDir, exeName)
		emit("error", 0, 0, err.Error())
		return err
	}
	if fi, serr := os.Stat(exe); serr != nil || fi.Size() == 0 {
		err := fmt.Errorf("zip 布局无效：%s 缺失或为空", exeName)
		emit("error", 0, 0, err.Error())
		return err
	}
	if appDir := filepath.Join(filepath.Dir(exe), "app"); !isDir(appDir) {
		err := fmt.Errorf("zip 布局无效：缺少 %s/app（应用载荷目录）", payloadDir)
		emit("error", 0, 0, err.Error())
		return err
	}
	// portable 激活器：与 exe 同级补建 data\，首启即数据自包含在托管目录
	// （上游绿色版实证语义；缺失时应用会回落写 ~/.termora）。
	if err := os.MkdirAll(filepath.Join(filepath.Dir(exe), dataDirName), 0755); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}

	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   digest,
		AssetSHA256: fileSHA256(exe),
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, tag, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("落位失败: %v", err))
		return err
	}

	emit("done", 100, 100, "")
	return nil
}

// ImportLocal 把本机已有 Termora 便携目录（jpackage app-image 解压件）
// 收纳为托管版本：整树复制保数据（data\ 内的会话/密钥索引"随目录走"，
// 导入即继承——用户预期是"接着用"，不是"重新配"）。data\ 缺失不伪造
// 用户数据，但落位后由启动前 ensure（service）补齐自包含。
func (m *Manager) ImportLocal(srcDir string) (TermoraVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe, ok := findPayloadExe(srcDir)
	if !ok {
		return TermoraVersionInfo{}, fmt.Errorf("导入失败：%s（含 %s 子目录）内未找到可用的 %s", srcDir, payloadDir, exeName)
	}
	payloadRoot := filepath.Dir(srcExe) // 复制单元：含主程序的 Termora/ 层

	fi, err := os.Stat(srcExe)
	if err != nil || fi.Size() == 0 {
		return TermoraVersionInfo{}, fmt.Errorf("%s 不可用（缺失或空文件）", exeName)
	}

	token := importToken(srcExe)
	if err := artifact.ValidateVersionToken(token); err != nil {
		return TermoraVersionInfo{}, err
	}
	stagingID := "import-" + token + "-" + time.Now().Format("150405")
	staging, discard, err := m.tree.StageDir(stagingID)
	if err != nil {
		return TermoraVersionInfo{}, err
	}
	defer discard()
	if err := copyTreeExcept(payloadRoot, staging, []string{"meta.json", moduleMetaFileName}); err != nil {
		return TermoraVersionInfo{}, fmt.Errorf("迁移 Termora 目录失败: %w", err)
	}
	dstExe, ok := findPayloadExe(staging)
	if !ok {
		return TermoraVersionInfo{}, fmt.Errorf("导入收口异常：中转目录内找不到 %s", exeName)
	}
	if err := writeModuleMeta(staging, moduleMeta{Source: srcDir, IsImport: true}); err != nil {
		return TermoraVersionInfo{}, fmt.Errorf("写入导入账目失败: %v", err)
	}
	meta := artifact.Meta{
		Entry:       exeName,
		AssetSHA256: fileSHA256(dstExe),
		Source:      artifact.SourceImported,
	}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		return TermoraVersionInfo{}, err
	}

	installedDir := filepath.Join(m.versionsDir, treeEntryName+"_"+token)
	return TermoraVersionInfo{
		Version:     normalizeVersion(token),
		ExePath:     filepath.Join(installedDir, relFromStaging(staging, dstExe)),
		Dir:         installedDir,
		Size:        dirSize(installedDir, fi.Size()),
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

// ResolveExe 返回指定版本的主程序绝对路径（嵌套与平铺两形均可解析）。
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

// dirSize 版本目录整树字节和（jpackage 布局下主 exe 只是启动器，app/ 与
// runtime/ 才是体积主体——单报 exe 尺寸与真实包体量级失真）。dirstats 度量
// 天然跳过符号链接/重解析点防环；预算超限或度量失败回退主程序文件大小旧
// 口径并 Debug 报账，不谎报。
func dirSize(dir string, fallback int64) int64 {
	st := dirstats.MeasureBudgeted(dir, dirSizeBudget)
	if st.Err != nil || st.Partial || st.Bytes <= 0 {
		slog.Debug("termora 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}

// normalizeVersion 版本展示归一：补 v 前缀；imported-*/裸段原样。
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") || !strings.Contains(v, ".") {
		return v
	}
	return "v" + v
}

// findPayloadExe 在版本目录内定位 Termora.exe：官方 jpackage 嵌套
// （Termora/Termora.exe）优先，平铺（手工整理/导入件）兜底。
func findPayloadExe(dir string) (string, bool) {
	nested := filepath.Join(dir, payloadDir, exeName)
	if regular(nested) {
		return nested, true
	}
	flat := filepath.Join(dir, exeName)
	if regular(flat) {
		return flat, true
	}
	return "", false
}

func regular(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// importToken 导入版本令牌：jpackage 启动器 PE 文件版本优先（形如
// 2.0.0.16——段数 3~4，仅修剪尾部 .0 冗余段，绝不把 beta 构建号截掉；
// 与远程 tag 的对应关系不假装精确，账目如实记其来源路径）；取不到按
// imported-时间戳 兜底（不猜版本，绝不与真实版本号撞令牌）。
func importToken(exe string) string {
	if fv, err := versioninfo.FileVersion(exe); err == nil {
		if ver, ok := normalizeFileVersion(fv); ok {
			return ver
		}
	}
	return "imported-" + time.Now().Format("20060102150405")
}

var digitsRe = regexp.MustCompile(`^\d+$`)

// normalizeFileVersion PE 版本串（可能带空白/尾点，段数 3~4 纯数字）归一。
func normalizeFileVersion(fv string) (string, bool) {
	ver := strings.TrimRight(strings.TrimSpace(fv), ".")
	parts := strings.Split(ver, ".")
	if len(parts) < 3 || len(parts) > 4 {
		return "", false
	}
	for _, p := range parts {
		if !digitsRe.MatchString(p) {
			return "", false
		}
	}
	for len(parts) > 3 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "."), true
}

// formatInstalledAt 安装时间展示：优先账本 installedAt，无账回退 exe 修改时间。
func formatInstalledAt(fromMeta time.Time, exe string) string {
	if !fromMeta.IsZero() {
		return fromMeta.Local().Format("2006-01-02 15:04:05")
	}
	if fi, err := os.Stat(exe); err == nil {
		return fi.ModTime().Format("2006-01-02 15:04:05")
	}
	return ""
}

// relFromStaging 由 staging 内命中路径推 payload 相对形状（嵌套或平铺），
// 拼接到最终目录后得到 Commit 后的真实 exe 路径。
func relFromStaging(staging, exe string) string {
	rel, err := filepath.Rel(staging, exe)
	if err != nil {
		return filepath.Join(payloadDir, exeName) // 布局自检后理论不可达：兜官方形
	}
	return rel
}

// ---------- 账本 ----------

// moduleMeta 模块侧账本（meta.module.json）：isImport/source 承载导入来源
// 明细；远程安装的 VerifiedHash=true 由内核摘要必检事实推定，不需入账。
type moduleMeta struct {
	Source   string `json:"source,omitempty"`
	IsImport bool   `json:"isImport,omitempty"`
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
