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

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"
)

const (
	exeName     = "Paseo.exe"                                           // electron-builder executableName 固定
	asarRelPath = "resources" + string(filepath.Separator) + "app.asar" // Electron 主包，布局自检依据
	// treeEntryName 版本树目录前缀（<root>/paseo_<version>，与历史布局
	// paseo_0.8.0 / paseo_imported-时间戳 一致）。
	treeEntryName = "paseo"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源）。
	dirPrefix = treeEntryName + "_"
	// fetchBudget 单次下载的总超时预算（win zip ~180MB，沿用原下载客户端 20 分钟口径）。
	fetchBudget = 20 * time.Minute
)

// canonicalVersionRe 规范 semver 版本令牌：0.8.0 / 0.8.0-beta.1（beta 通道
// 预发布带 -beta.N 后缀，预发布规则排序见 semver.go）。
var canonicalVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.\-]*)?$`)

// tokenVersionRe 版本令牌白名单（目录名形状判定，与原 versionDirRe 去前缀
// 等价）：规范 semver，或版本探测失败收纳的 imported-YYYYMMDD-HHMMSS 兜底
// （vscode 同规则）。
var tokenVersionRe = regexp.MustCompile(`^(?:\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.\-]*)?|imported-\d{8}-\d{6})$`)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager Paseo 版本管理引擎：远程列表（双通道 GitHub 元数据）与"下载 → 校验 →
// 解包 → 落位"主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch +
// UnpackZip + Tree）；本包只保留 Paseo 领域知识：electron-builder win zip 便携
// 资产筛选（含 mac zip 炸弹排除）、镜像 URL 模板、版本目录形状（semver 含预发布 /
// imported-时间戳）、Electron 双锚点（Paseo.exe + resources/app.asar）布局自检、
// 本地导入整套迁移与既有进度词表映射。
//
// 多版本隔离目录设计（vscode 便携同款，与 recordly 的 NSIS 单目录相反）：
// 上游 win zip 是"解压即运行"的 win-unpacked 归档，无注册表卸载语义，
// 因此每版本独占 versions/paseo_<ver>/ 目录，共存与切换零成本。
//
// 数据归属（集成决策）：Paseo 无 data\ 便携激活器，Electron 数据恒在
// %APPDATA%\Paseo、daemon 数据恒在 ~/.paseo——托管实例与用户自装实例共享
// 同一份用户目录数据（拍板方案，理由见模块 module.go 包注释），故解压/导入
// 均不做任何数据目录搬运或隔离注入。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	fetch   fetcher
	mirrors func(tag, assetName string) []string
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

// OpenTree 打开 Paseo 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（includePre=true 时含 beta 通道；10 分钟缓存）。
// N13 形态标注：返回前逐行回填 Form=hostedForm（本模块恒 win zip 便携形态；
// 缓存出口两分支均按通道产出新切片，就地回填不触碰缓存源）。
func (m *Manager) ListRemote(includePre bool) ([]PaseoRelease, error) {
	list, err := remoteCache.get(includePre)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Form = hostedForm
	}
	return list, nil
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描）。
// 目录命名 paseo_0.8.0 / paseo_0.8.0-beta.1（下载/导入的正规版本）或
// paseo_imported-YYYYMMDD-HHMMSS（版本探测失败的导入兜底）；
// 其余形状的版本令牌不列入；Paseo.exe 缺失/为空或 resources/app.asar
// 缺失均视为损坏安装跳过（双锚点自检）。
func (m *Manager) ListInstalled() ([]PaseoVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []PaseoVersionInfo
	for _, v := range vers {
		if !tokenVersionRe.MatchString(v.Version) {
			continue
		}
		exe := filepath.Join(v.Dir, exeName)
		if !validPortableLayout(v.Dir) {
			continue
		}
		fi, statErr := os.Stat(exe) // validPortableLayout 已保证存在
		if statErr != nil {
			continue
		}

		info := PaseoVersionInfo{
			Version: v.Version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    dirSize(v.Dir, fi.Size()),
		}
		// 新下载链走内核统一账本（artifact.Meta，含 schema）：官方摘要在场即
		// verifiedHash；导入链与迁移前的历史账本走模块自写 map 形态（无 schema，
		// Tree 解析为零值），installedAt 原样展示、isImport/source/verifiedHash
		// 从 legacy 字段读取。
		if v.Meta.ZipSHA256 != "" {
			info.VerifiedHash = true
		}
		if !v.Meta.InstalledAt.IsZero() {
			info.InstalledAt = v.Meta.InstalledAt.Local().Format("2006-01-02 15:04:05")
		}
		if info.InstalledAt == "" {
			legacyAt, isImport, src, verified := readLegacyMetaFields(v.Dir)
			info.InstalledAt = legacyAt
			info.IsImport = isImport
			info.Source = src
			info.VerifiedHash = info.VerifiedHash || verified
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// Download 下载官方 win zip 并安装到 versions/paseo_<ver>/。
// 完整性主流程收口至内核 artifact.Fetch：以 GitHub API 官方资产摘要（digest，
// parseReleasesBody 已剥前缀入 PaseoRelease.SHA256，无摘要的 release 根本不进
// 列表）为信任根做流式 + 落盘双 SHA-256 校验，Content-Length 与流式上限双核
// （对齐原"字节数 == release 声明 size"层），镜像只是同摘要的备用传输来源；
// 解包经 artifact.UnpackZip（ZipSlip/炸弹/CRC32 全量闸门，取代原 extractAll），
// 落位经 Tree.Commit（staging + 原子 rename，同版本异摘要防漂移——原实现直写
// 最终目录，半件即污染安装）。
// 进度回调沿用本模块既有词表（downloading/verify/extract/done/error）：
// verify 是内核 Fetch 的真实阶段迁移（官方摘要双核完成点），如实透出不造幻影。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// onProgress 可选：实时上报各阶段进度（下载字节、校验、解压落位）。
// Download 保留旧调用面，供版本包单测与非事务调用使用。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装，可由事务 context 取消。
func (m *Manager) DownloadContext(ctx context.Context, txnID, version string, onProgress func(p DownloadProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析目标版本对应的远程资产（缓存内按全量查找，不受当前通道显示过滤影响）
	rel, ok := remoteCache.findRelease(version)
	if !ok {
		// 冷缓存：先拉一轮列表再查（findRelease 以 Version 字段匹配，缓存已存裸版本）
		if _, err := remoteCache.get(true); err != nil {
			emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
			return err
		}
		rel, ok = remoteCache.findRelease(version)
	}
	if !ok {
		err := fmt.Errorf("远程列表不存在版本 %s（或该版本无本机可用的 Windows 便携资产/官方哈希）", version)
		emit("error", 0, 0, err.Error())
		return err
	}
	// 官方摘要信任根：GitHub release API 的 asset.digest。上游 win zip 实测
	// 全覆盖（v0.5.0-beta.1 起逐版核对），缺失一律拒装，不做无校验安装。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 Paseo %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-paseo-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 受控下载（主址 + 镜像逐个回退；下载/校验/字节数双核全部委托内核。
	// 注意下载路径用 Tag 原文拼写，大小写敏感不可归一）
	urls := m.mirrors(rel.Tag, rel.AssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   rel.SHA256,
		MaxBytes: rel.Size, // 与 release API 声明大小对齐：超限即断，杜绝异常放大
		FileName: rel.AssetName,
	}
	emit("downloading", 0, rel.Size, "")
	fetchErr := m.fetch(ctx, src, tmpZipPath, func(p artifact.Progress) {
		// 内核进度 → 既有词表：流式下载对应 downloading，摘要双核完成点
		// 对应 verify，其余阶段本模块不上报
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
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）
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
	// 4. Electron 双锚点布局自检（模块策略，内核不感知）：
	// Paseo.exe 非空 + resources/app.asar 存在（win zip = win-unpacked 打包，
	// 中央目录实测无根目录包裹，二者落在中转目录根——勿按"包内单根目录"直觉
	// 改造，vscode/recordly 点名过的同一陷阱）。
	if err := layoutCheck(staging); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	// exe 诊断摘要入账（避免每次扫描现场哈希；PaseoVersionInfo 不展示，纯账本诊断）
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

// Remove 卸载指定版本（委托 Tree：rename 隔离后删除，文件占用时留下可恢复状态）。
// 刻意不动 %APPDATA%\Paseo 与 ~/.paseo：那是用户目录共享数据，
// 归用户与其自装实例所有，删除托管版本不越权（recordly"卸载保数据"先例）。
func (m *Manager) Remove(version string) error {
	_, token, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return m.tree.Remove(token, nil)
}

// ResolveExe 返回指定版本 Paseo.exe 路径（不存在返回错误）。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（paseo_0.8.0 / paseo_0.8.0-beta.1 /
// paseo_imported-时间戳；接受带/不带 v 前缀输入）。
// 形状外令牌（含路径穿越）先于任何磁盘访问被拒，错误口径与原实现一致。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	token = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !tokenVersionRe.MatchString(token) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	if d, rerr := m.tree.Resolve(token); rerr == nil {
		return d, token, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// validPortableLayout 便携目录双锚点布局判定：Paseo.exe 非空 +
// resources/app.asar 存在（electron-builder 恒定成员；app.asar.unpacked
// 由 asarUnpack 派生、随版本配置漂移，不作判据）。
func validPortableLayout(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	if asar, err := os.Stat(filepath.Join(dir, asarRelPath)); err != nil || asar.IsDir() {
		return false
	}
	return true
}

// layoutCheck 解压/导入后的落盘布局自检（带中文诊断文案）：
// exe 非空 + Electron 主包存在。不符即判定安装无效，调用方丢弃 staging。
func layoutCheck(dir string) error {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("布局无效：缺少可用的 %s", exeName)
	}
	if asar, err := os.Stat(filepath.Join(dir, asarRelPath)); err != nil || asar.IsDir() {
		return fmt.Errorf("布局无效：缺少 %s", asarRelPath)
	}
	return nil
}

// readLegacyMetaFields 读取模块自写的导入账本与迁移前历史下载账本（无 schema
// 的 map 形态）：installedAt 原样字符串、isImport 布尔、source 导入来源目录
// （历史下载账本为资产名）、verifiedHash 布尔。新下载链的 artifact.Meta 账本
// （含 schema）不走本函数（verifiedHash 由 ZipSHA256 在场推定）。
func readLegacyMetaFields(dir string) (installedAt string, isImport bool, source string, verifiedHash bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return "", false, "", false
	}
	var mm map[string]any
	if json.Unmarshal(raw, &mm) != nil {
		return "", false, "", false
	}
	installedAt, _ = mm["installedAt"].(string)
	isImport, _ = mm["isImport"].(bool)
	source, _ = mm["source"].(string)
	verifiedHash, _ = mm["verifiedHash"].(bool)
	return installedAt, isImport, source, verifiedHash
}

// ---------- 本地导入（Paseo 领域流程，无内核对应物） ----------

// ImportLocal 导入本地 Paseo 程序目录（整套 Electron 目录拷贝迁移）。
// 数据恒在 %APPDATA%\Paseo 与 ~/.paseo（与 exe 位置无关），程序目录内没有
// 用户数据，整套拷贝即为完整迁移；与"便携 vs 自装"哪份在用无关。
// 调用方需先确保源实例未运行（Windows 下运行中的 exe 被独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (PaseoVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return PaseoVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}
	if asar, err := os.Stat(filepath.Join(srcDir, asarRelPath)); err != nil || asar.IsDir() {
		return PaseoVersionInfo{}, fmt.Errorf("源目录缺少 %s，不是 Paseo 程序目录: %s", asarRelPath, srcDir)
	}
	if isUnderDir(srcDir, filepath.Join(m.versionsDir)) {
		return PaseoVersionInfo{}, fmt.Errorf("源目录本身就在 Hanxi 托管目录内，无需导入")
	}

	version := ""
	if v, verr := versioninfo.FileVersion(srcExe); verr == nil && v != "" {
		cand := strings.TrimPrefix(v, "v")
		if canonicalVersionRe.MatchString(cand) {
			version = cand
		}
	}
	if version == "" {
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	if err := artifact.ValidateVersionToken(version); err != nil {
		return PaseoVersionInfo{}, err
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return PaseoVersionInfo{}, fmt.Errorf("版本 %s 已存在，请先卸载该版本再导入", version)
	}

	if err := copyTreeExcept(srcDir, targetDir, []string{"meta.json", "hanxi-meta.json"}); err != nil {
		_ = os.RemoveAll(targetDir)
		return PaseoVersionInfo{}, fmt.Errorf("迁移程序目录失败: %w", err)
	}
	if err := layoutCheck(targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return PaseoVersionInfo{}, fmt.Errorf("导入后%w", err)
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt":  now,
		"isImport":     true,
		"source":       srcDir,
		"verifiedHash": false,
	})

	return PaseoVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        dirSize(targetDir, fi.Size()),
		InstalledAt: now,
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// isUnderDir 判断 path 是否位于 parent 目录内（大小写不敏感，Windows 语义）。
func isUnderDir(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../") && rel != "."
}

// copyTreeExcept 整树复制目录（跳过 skip 名单中的顶层项，跳过符号链接防环）。
func copyTreeExcept(src, dst string, skip []string) error {
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		if skipSet[e.Name()] {
			continue
		}
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			continue // 程序目录理论无符号链接；保守跳过防环路与越权写
		case e.IsDir():
			if err := copyTreeExcept(s, d, nil); err != nil {
				return err
			}
		default:
			if err := copyFileTo(s, d); err != nil {
				return err
			}
		}
	}
	return nil
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

// dirSize 版本目录整树字节和：主程序文件只是入口，多文件载荷才是体量的
// 主体，单报主 exe 尺寸与真实安装体量级失真。dirstats 度量恒跳过符号链接/
// 重解析点防环；2s 挂钟预算超限或度量失败回退旧口径（主程序文件大小）并
// Debug 报账，不谎报全量。
func dirSize(dir string, fallback int64) int64 {
	st := dirstats.MeasureBudgeted(dir, 2*time.Second)
	if st.Err != nil || st.Partial || st.Bytes <= 0 {
		slog.Debug("paseo 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
