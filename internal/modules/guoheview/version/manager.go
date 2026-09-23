package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
)

const (
	exeName          = "GuoheView.exe" // 主程序（自研解码内核经 ghde.dll 等同目录 DLL 加载）
	portableMarkName = "portable.ini"  // 官方便携标记：存在则 config.ini 落 exe 同目录，程序只读不改

	// treeEntryName 版本树目录前缀（<root>/guoheview_<version>，与历史布局
	// guoheview_3.2.7.98 / guoheview_imported-<时间戳> 一致）。
	treeEntryName = "guoheview"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源）。
	dirPrefix = treeEntryName + "_"
)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// dirNameRe 版本令牌形状（guoheview_3.2.7.98 四段构建号；imported- 分支收纳
// 版本探测失败时间戳兜底的导入——无 FileVersion 资源时）
var dirNameRe = regexp.MustCompile(`^` + dirPrefix + `(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// Manager 果核看图版本管理引擎："解包 → 落位"主流程委托 Wave 4 共享内核
// packages/go/artifact（UnpackZip + Tree），"下载 + 官方哈希校验"段按
// ADR-0002 §5 薄适配器裁定留在本包 bespoke：上游发布接口（rj.lovestu.com，
// 非 GitHub）官方哈希只有 MD5，而内核 artifact.Fetch 以官方 SHA-256 为唯一
// 信任根不放宽（放宽=削弱分发专项 §10.2 的安全裁定）。官方摘要必检的精神
// 保留：MD5 也检——官方弱摘要仅作完整性、不作发布者信任（叠加 HTTPS 传输、
// 字节数、zip entry CRC32 与解压布局自检共同兜底）。本包另保留果核领域知识：
// 便携资产筛选、发布接口缓存、GuoheViewPortable/ 包装目录收割、portable.ini
// 补写与本地整套导入。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree
	client      *http.Client // 下载客户端（长超时，bespoke 下载链专用）
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		client:      &http.Client{Timeout: 10 * time.Minute},
	}
}

// OpenTree 打开果核看图版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（stable + 更新的 beta；10 分钟内命中缓存）。
// 上游接口只发布当前版本，列表至多两条——历史版本无法远程获取是上游特性。
func (m *Manager) ListRemote() ([]ViewRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 guoheview_X.Y.Z.W（下载/导入的正规版本）或
// guoheview_imported-YYYYMMDD-HHMMSS（版本探测失败的导入兜底）；
// 其余形状的版本令牌不列入；exe 缺失/为空视为损坏安装跳过；便携标记缺失
// 同样跳过（意味着实例会把配置写进 %APPDATA%，破坏托管隔离语义——下载链
// 已由 ensurePortableMark 保证补写，历史/外来目录缺标记按损坏处理）。
func (m *Manager) ListInstalled() ([]ViewVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []ViewVersionInfo
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
		if _, statErr := os.Stat(filepath.Join(v.Dir, portableMarkName)); statErr != nil {
			continue
		}

		info := ViewVersionInfo{
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

// Download 下载便携 zip 并安装到 versions/guoheview_X.Y.Z.W/。
// 完整性四层兜底（官方哈希只有 MD5，第一层强度弱于 GitHub digest，如实说明：
// 官方弱摘要仅作完整性、不作发布者信任）：
//  1. 官方 MD5 + HTTPS 传输（接口 md5 字段，bespoke 校验，防损坏与低级篡改）；
//  2. 下载落盘字节数 == 接口声明 size（防截断）；
//  3. 解包委托 artifact.UnpackZip——ZipSlip/链接语义/炸弹预算/Windows 文件名
//     纪律/每 entry 强制 CRC32 全量闸门（收口原 extractAll 手写副本）；
//  4. 布局自检（GuoheView.exe 唯一且非空）+ portable.ini 缺失补写（官方
//     开关语义，程序只读不改，补写仅恢复"配置留在隔离目录"的托管前提）。
//
// 落位经 Tree.Commit（staging + 原子 rename，同版本异摘要防漂移——原实现
// 直写最终目录，半件即污染安装）。进度回调沿用本模块既有词表
// （downloading/verify/extract/done/error），不造幻影步骤。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue——事务与
// 解包器无关，bespoke 下载段同样记在账上）。
//
// onProgress 可选：实时上报各阶段进度。
// Download 保留旧调用面，供版本包单测与非事务调用使用。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装，可由事务 context 取消（P0 批 2b 生命周期）。
// bespoke MD5 下载链已 ctx 化（重试间隙/候选源前/传输读循环均可即时中止）。
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

	// 1. 解析目标版本对应的远程资产（模块知识：官方发布接口与便携资产筛选）
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本失败: %v", err))
		return err
	}
	var rel *ViewRelease
	for i := range releases {
		if releases[i].Version == version {
			rel = &releases[i]
			break
		}
	}
	if rel == nil {
		err := fmt.Errorf("远程列表不存在版本 %s（上游只发布当前版本，旧版请用「导入本地」）", version)
		emit("error", 0, 0, err.Error())
		return err
	}
	// 官方摘要必检（弱摘要族口径同 ADR-0002 §5）：parseChannelBody 已挡缺
	// MD5 资产进列表，此处兜底直投缓存等旁路——无官方哈希一律拒装，不做
	// 无校验安装。
	if !md5HexRe.MatchString(strings.TrimSpace(rel.MD5)) {
		err := fmt.Errorf("上游未提供果核看图 %s 的官方 MD5 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-guoheview-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 下载 zip（官方单域 bespoke 链，同 URL 多轮重试；不委托内核 Fetch 的
	// 原因见包注释：信任根形状不兼容，纪律仍在模块保留）
	emit("downloading", 0, rel.Size, "")
	if err := downloadTo(ctx, m.client, []string{rel.AssetURL}, tmpZipPath, func(done int64) {
		emit("downloading", done, rel.Size, "")
	}); err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 3. 字节数校验
	actual, err := fileSize(tmpZipPath)
	if err != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if actual != rel.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, actual)
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 4. 官方 MD5 校验（bespoke verify 段，journal 以独立 verify 步如实记账）
	emit("verify", 0, 0, "")
	if err := verifyMD5(tmpZipPath, rel.MD5); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
	}
	// 包摘要落账（防漂移信任根：官方 MD5 是发布方声明，本地 SHA-256 供
	// Tree.Commit 的同版本幂等/异摘要拒装判定）
	zipSHA256 := fileSHA256(tmpZipPath)

	// 5. 解包进独占中转目录（staging 与最终目录同卷，供原子落位；
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
	// 便携根收割 + 便携标记补写（果核布局策略，内核不感知；Commit 前收口）
	if err := harvestPortableRoot(staging); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	if err := ensurePortableMark(staging); err != nil {
		emit("error", 0, 0, fmt.Sprintf("补写便携标记失败: %v", err))
		return err
	}
	// exe 诊断摘要入账（避免每次扫描现场哈希；ViewVersionInfo 不展示，纯账本诊断）
	assetSHA := fileSHA256(filepath.Join(staging, exeName))

	// 6. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本异摘要拒绝
	// 覆盖——防止同版本号内容漂移）
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   zipSHA256,
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

// ResolveExe 返回指定版本的 GuoheView.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（guoheview_3.2.7.98 或 guoheview_imported-时间戳）。
// 形状外令牌（含路径穿越）先于任何磁盘访问被拒，错误口径与原实现一致。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	token = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !dirNameRe.MatchString(dirPrefix + token) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	if d, rerr := m.tree.Resolve(token); rerr == nil {
		return d, token, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// versionFromToken 把 Tree 扫出的版本令牌规范化为 vX.Y.Z.W / vimported-<时间戳>
// （3.2.7.98 → v3.2.7.98，与原目录名剥离前缀的口径一致）。仅接受四段
// x.y.z.w 或 imported-时间戳形状，guoheview_v3.2.7.98 之类带 v 前缀的
// 历史/外来目录名不列入。
func versionFromToken(token string) (string, bool) {
	if dirNameRe.MatchString(dirPrefix + token) {
		return "v" + token, true
	}
	return "", false
}

// harvestPortableRoot 收割便携根目录（模块布局策略，内核不感知）：官方便携
// zip 顶层是 GuoheViewPortable/ 包装目录（实测 3.2.7），若原样保留会深一层、
// 破坏"版本目录即安装目录"的全家族布局。以 exe 所在目录为 payload 根，把根内
// 内容平铺进 staging 根、根外杂质一概不带入（口径同原 extractAll 的
// stripPayloadPrefix——安全闸门已收口 artifact.UnpackZip，此处只收口布局）。
func harvestPortableRoot(staging string) error {
	root, err := locatePayloadRoot(staging)
	if err != nil {
		return err
	}
	if root == staging {
		return nil // zip 已是平铺布局：根即目录本身，全收
	}

	// staging 下通往 payload 根的第一段祖先是"保留链"；其余顶层 entry 为根外杂质
	rel, err := filepath.Rel(staging, root)
	if err != nil {
		return err
	}
	top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	entries, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("读取中转目录失败: %w", err)
	}
	for _, e := range entries {
		if e.Name() == top {
			continue
		}
		if err := os.RemoveAll(filepath.Join(staging, e.Name())); err != nil {
			return fmt.Errorf("清除根外杂质失败: %w", err)
		}
	}

	// payload 根内容平铺到 staging 根，随后移除包装链（含链上残留杂质）
	items, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("读取便携根失败: %w", err)
	}
	for _, it := range items {
		if err := os.Rename(filepath.Join(root, it.Name()), filepath.Join(staging, it.Name())); err != nil {
			return fmt.Errorf("收割便携根目录失败: %w", err)
		}
	}
	if err := os.RemoveAll(filepath.Join(staging, top)); err != nil {
		return fmt.Errorf("清理包装目录失败: %w", err)
	}

	// 复验收割结果：exe 落回 staging 根且非空
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	}
	return nil
}

// locatePayloadRoot 在 staging 内定位唯一的 GuoheView.exe（常规文件且非空），
// 返回其所在目录作为便携根；零个或多个都无法确立"版本目录即安装目录"布局。
func locatePayloadRoot(staging string) (string, error) {
	var roots []string
	err := filepath.WalkDir(staging, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(d.Name(), exeName) {
			return nil
		}
		if fi, serr := d.Info(); serr == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			roots = append(roots, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("扫描解压布局失败: %w", err)
	}
	switch len(roots) {
	case 1:
		return roots[0], nil
	case 0:
		return "", fmt.Errorf("zip 布局无效：缺少可用的 %s", exeName)
	default:
		return "", fmt.Errorf("zip 布局无效：找到 %d 个 %s，无法判定便携根", len(roots), exeName)
	}
}

// portableMarkText 上游 portable.ini 原文即"仅作是否便携的开关，程序不会
// 修改它"——缺失时补写该文件语义安全（下载与导入链共用）。
const portableMarkText = "; Hanxi 托管安装补写的便携模式开关（官方语义，见上游 portable.ini 说明）\n"

// ensurePortableMark 便携标记存在性兜底：staging/版本目录缺 portable.ini 时
// 补写官方开关——保证托管实例配置永远留在隔离目录，不外溢 %APPDATA%。
func ensurePortableMark(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, portableMarkName)); err == nil {
		return nil
	}
	return os.WriteFile(filepath.Join(dir, portableMarkName), []byte(portableMarkText), 0644)
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

// ---------- 本地导入（果核看图领域流程，无内核对应物） ----------

// ImportLocal 导入本地已有的果核看图便携目录（自行解压/拷贝的整套目录）。
// 果核看图是"整套目录即程序"的便携形态（DLL + resources + plugins 缺一不可），
// 与 everything 同款整套搬运；config.ini 一并带走（便携模式下配置就在目录内）。
// 源目录缺 portable.ini 时经 ensurePortableMark 补写官方开关——保证托管实例
// 配置永远留在隔离目录，不外溢 %APPDATA%。
// 调用方需先确保源实例未运行（运行中的文件被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (ViewVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return ViewVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !fourSegVersion.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return ViewVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return ViewVersionInfo{}, err
	}

	// 整套搬运；跳过托管侧自有 meta.json（由本次导入重写）与临时残留
	if err := copyTree(srcDir, targetDir, "meta.json"); err != nil {
		_ = os.RemoveAll(targetDir)
		return ViewVersionInfo{}, err
	}
	if err := ensurePortableMark(targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return ViewVersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
	})

	return ViewVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        fi.Size(),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// copyTree 把 src 目录内容整体复制到 dst（保留相对结构）。
// skip 列表按文件基名匹配（导入场景排除托管侧 meta.json）。
func copyTree(src, dst string, skip ...string) error {
	skipSet := map[string]bool{"meta.json.tmp": false}
	for _, s := range skip {
		skipSet[s] = true
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.Name() != "." && skipSet[d.Name()] {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFileTo(path, target)
	})
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
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
