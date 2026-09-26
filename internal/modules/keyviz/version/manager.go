package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"
)

const (
	// exeName Tauri 有效载荷主程序（MSI 内单文件，WebView2 走系统运行时）
	exeName = "keyviz.exe"
	// treeEntryName 版本树目录前缀（<root>/keyviz_<version>，与历史布局
	// keyviz_2.1.1 / keyviz_imported-<时间戳> 一致）。
	treeEntryName = "keyviz"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源）。
	dirPrefix = treeEntryName + "_"
	// msiExtractInfoName 模块侧 MSI 提取来源账本（落在版本目录内，随 staging
	// 一并原子落位）。内核 artifact.Meta 是定形结构、无自由字段，Meta.Source
	// 家族约定只有 remote|imported 两值（不强行加第三枚举），"内容经
	// msiexec /a 管理提取而来"的如实细节（资产名、MSI 尺寸/摘要、提取入口）
	// 记录在这份额外账里；下载完整性主账（包摘要/exe 摘要/安装时间）仍由
	// 内核 meta.json 承载。
	msiExtractInfoName = "msi-extract.json"
	// fetchBudget 单次 MSI 下载的总超时预算（沿用原下载客户端 10 分钟口径）。
	fetchBudget = 10 * time.Minute
)

// plainVersionRe 纯版本号（如 2.1.1），用于目录令牌与 FileVersion 校验
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的兜底目录后缀（imported-YYYYMMDD-HHMMSS）
var importedDirRe = regexp.MustCompile(`^imported-\d{8}-\d{6}$`)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 镜像回退 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// msiPayloadWaitBudget / msiPayloadPollInterval 管理提取落盘防御轮询窗口
// （msiexec 客户端返回与安装服务落盘理论上有间隙）。包级变量仅为单测压缩等待。
var (
	msiPayloadWaitBudget   = 10 * time.Second
	msiPayloadPollInterval = 200 * time.Millisecond
)

// msiExtract 管理提取执行接缝：默认真实 msiexec（runMSIAdminInstall）。
// MSI 提取链刻意不进 artifact 内核——内核无 MSI 策略，策略族/声明化是 Wave 5
// 签名批次的事，不在模块侧造第二套；`msiexec /a` 行政安装 + 布局收割属
// Keyviz 领域知识（piclite 同机制已真机验证），留在本包经接缝收口。
// 测试注入假实现：直接在管理映像目录落盘布局或返回错误，不依赖本机
// Installer 服务；真实成功路径已在侦查阶段真机验证（v2.1.1，
// PFiles\keyviz\keyviz.exe，官方 digest 一致）。
var msiExtract = runMSIAdminInstall

// Manager Keyviz 版本管理引擎：远程列表（GitHub 元数据）与"下载 → 官方摘要
// 校验 → 缓存归档"段委托 Wave 4 共享内核 packages/go/artifact（Fetch + Tree）；
// MSI 管理提取与提取后布局校验/搬运留在本包；最终落位统一走
// Tree.StageDir → Commit（staging + 原子 rename，同版本异摘要防漂移——原
// 实现直写最终目录，半件即污染安装）。本包另保留领域知识：MSI 资产筛选、
// 镜像 URL 模板、版本目录形状（x.y.z / imported-时间戳）、本地导入白名单
// 与既有进度词表（downloading/verify/extract/done/error）映射。
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

// OpenTree 打开 Keyviz 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]KeyvizRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描，按版本号降序）。
// 目录命名 keyviz_X.Y.Z（下载/导入的正规版本）或
// keyviz_imported-YYYYMMDD-HHMMSS（版本探测失败的导入兜底）；
// 其余形状的版本令牌不列入；exe 缺失/为空视为损坏安装跳过。
// Keyviz 配置恒在 %APPDATA%\org.keyviz\store.json（tauri-plugin-store 用户
// 目录，与 exe 位置无关），目录内除有效载荷外只有账本文件。
func (m *Manager) ListInstalled() ([]KeyvizVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []KeyvizVersionInfo
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

		info := KeyvizVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    dirSize(v.Dir, fi.Size()),
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

// Download 下载 Windows MSI 并管理提取安装到 versions/keyviz_X.Y.Z/。
// 上游不提供便携 zip，完整性防线与家族模板对齐、按 MSI 形态特化：
//  1. 受控下载收口内核 artifact.Fetch：官方 sha256（GitHub API digest）为
//     信任根做流式 + 落盘双核，Content-Length 与流式上限双核（对齐原
//     "字节数 == release 声明 size"层），镜像只是同摘要的备用传输来源——
//     MSI 缓存归档为临时 .msi 文件，提取后即删；
//  2. msiexec 管理提取（模块领域段，见 extractMSI）由 Windows Installer 对
//     cabinet 流做内建 CRC 校验（替代 zip 路线的 archive/zip 逐 entry CRC32）；
//  3. 提取后布局自检（keyviz.exe 存在且非空）失败丢弃 staging，最终落位经
//     Tree.Commit 原子 rename（半件永不以目录形态出现在版本树）。
//
// 进度词表保持模块既有口径（downloading/verify/extract/done/error）：内核
// Fetch 的 verify 阶段（官方摘要双核）如实映射为既有 verify，不发明新词。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
//
// onProgress 可选：实时上报各阶段进度（下载字节、校验、提取）。
// Download 保留旧调用面，供版本包单测与非事务调用使用。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装，可由事务 context 取消（P0 批 2b 生命周期）。
// 取消边界：下载流即时中止；msiexec 管理提取为外部 Installer 调用，
// 不可中途强杀——在其返回后的轮询/收割/落位边界收口（staging 随 discard 丢弃）。
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

	// 1. 解析目标版本对应的远程资产（模块知识：GitHub 元数据与 MSI 资产筛选）
	releases, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *KeyvizRelease
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
	// 已剥前缀入 KeyvizRelease.SHA256，无摘要的 release 根本不进列表）。
	// 缺失一律拒装，不做无校验安装。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 Keyviz %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpMSI, err := os.CreateTemp("", "hanxi-keyviz-*.msi")
	if err != nil {
		return err
	}
	tmpMSIPath := tmpMSI.Name()
	defer os.Remove(tmpMSIPath)
	tmpMSI.Close()

	// 2. 受控下载 MSI（主址 + 镜像逐个回退；下载/校验/字节数双核全部委托内核）
	urls := m.mirrors(version, rel.AssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   rel.SHA256,
		MaxBytes: rel.Size, // 与 release API 声明大小对齐：超限即断，杜绝异常放大
		FileName: rel.AssetName,
	}
	emit("downloading", 0, rel.Size, "")
	fetchErr := m.fetch(ctx, src, tmpMSIPath, func(p artifact.Progress) {
		// 内核进度 → 既有词表：流式下载映射 downloading，摘要双核映射 verify
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

	// 3. 独占中转目录（staging 与最终目录同卷，供原子落位；
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）
	token := strings.TrimPrefix(version, "v")
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

	// 4. msiexec 管理提取 + 布局收割（模块领域段，Installer 内建 CRC 校验
	// 在此阶段完成；失败连同 staging 一并丢弃）
	emit("extract", 0, 0, "")
	if err := extractMSI(ctx, tmpMSIPath, staging); err != nil {
		emit("error", 0, 0, fmt.Sprintf("提取失败: %v", err))
		return err
	}
	// 提取来源如实入账（模块侧账，随 rename 原子落位；见 msiExtractInfoName 注释）
	if err := writeJSON(filepath.Join(staging, msiExtractInfoName), map[string]any{
		"method":         "msiexec /a administrative install",
		"asset":          rel.AssetName,
		"msiSize":        rel.Size,
		"msiSHA256":      rel.SHA256,
		"extractedEntry": exeName,
		"verifiedHash":   true,
	}); err != nil {
		emit("error", 0, 0, fmt.Sprintf("写入提取账本失败: %v", err))
		return err
	}
	// exe 诊断摘要入账（避免每次扫描现场哈希；KeyvizVersionInfo 不展示，纯账本诊断）
	assetSHA, _ := fileSHA256(filepath.Join(staging, exeName))

	// 5. 原子落位 + 写统一账本（meta.json 由内核形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移。ZipSHA256 承载 MSI 包官方摘要，
	// 提取链的完整性主账与 zip 路线同构等价）
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

// ResolveExe 返回指定版本的 keyviz.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// resolveVersionDir 定位版本隔离目录（keyviz_X.Y.Z 或 keyviz_imported-时间戳）。
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

// versionFromToken 把 Tree 扫出的版本令牌规范化为 vX.Y.Z / vimported-<时间戳>
// （2.1.1 → v2.1.1，与原目录名剥离前缀的口径一致）。仅接受纯 x.y.z 或
// imported-时间戳形状，keyviz_vX.Y.Z 之类带 v 前缀的历史/外来目录名不列入。
func versionFromToken(token string) (string, bool) {
	if plainVersionRe.MatchString(token) || importedDirRe.MatchString(token) {
		return "v" + token, true
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

// ---------- MSI 管理提取（模块领域段，不进 artifact 内核） ----------

// extractMSI 把 MSI 经 msiexec 管理提取收割进已存在的独占中转目录 staging。
//
//  1. 独占临时管理映像目录（os.MkdirTemp 系统生成）作 TARGETDIR——用户输入
//     永远不进入提取命令行（注入防护审查结论见 buildMSIExtractCmd 注释）；
//  2. msiExtract 接缝执行管理安装（默认真实 Installer，测试注入假实现）；
//  3. 递归定位 exe 所在 payload 目录并 copyTree 平铺收割进 staging：
//     不硬编码 <stage>\PFiles\keyviz 层级，天然吸收管理映像布局漂移；
//     映像根部自动复制的源 keyviz.msi 副本随 stage 丢弃，不进入安装目录；
//  4. 布局自检：staging 内 keyviz.exe 存在且非空，失败由调用方 discard。
func extractMSI(ctx context.Context, msiPath, staging string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "hanxi-keyviz-msi-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	if err := msiExtract(msiPath, stage); err != nil {
		return err
	}
	// msiexec 客户端不可中途强杀（Installer 服务侧事务），其返回后先审取消
	if err := ctx.Err(); err != nil {
		return err
	}

	// 防御：客户端返回与安装服务落盘理论上有间隙，轮询等 payload 出现
	var payload string
	deadline := time.Now().Add(msiPayloadWaitBudget)
	for {
		if payload = findPayloadDir(stage, exeName); payload != "" {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("管理提取无效：映像中未找到 %s", exeName)
		}
		select {
		case <-time.After(msiPayloadPollInterval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := copyTree(payload, staging); err != nil {
		return err
	}

	// 布局自检：exe 存在且非空
	fi, err := os.Stat(filepath.Join(staging, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return fmt.Errorf("MSI 布局无效：缺少可用的 %s", exeName)
	}
	return nil
}

// runMSIAdminInstall 真实管理提取执行器：构造受控 argv 并同步等待。
// /qn 全程静默；msiexec.exe 作为 Installer 客户端在装完前不返回，
// CombinedOutput 同步等待。
func runMSIAdminInstall(msiPath, stage string) error {
	cmd, err := buildMSIExtractCmd(msiPath, stage)
	if err != nil {
		return err
	}
	if out, cerr := cmd.CombinedOutput(); cerr != nil {
		return fmt.Errorf("msiexec 管理提取失败: %w（输出: %s）", cerr, strings.TrimSpace(string(out)))
	}
	return nil
}

// buildMSIExtractCmd 组装 msiexec 管理提取命令——受控常量拼接，注入防护核心。
// 审查结论与加固说明：
//   - argv 恒为定形五元组 [msiexec.exe, /a, <msiPath>, /qn, TARGETDIR=<stage>]，
//     exec.Command 直启不经 shell（Windows 为 CreateProcess argv 语义），不存在
//     命令分隔符/重定向解释面；参数含空格时由 Go 侧对"整个 argv 元素"加引号，
//     TARGETDIR= 属性值始终保持单一 token，不会被拆参注入；
//   - 两个路径参数均为系统生成（os.CreateTemp 的 .msi 缓存、os.MkdirTemp 的
//     管理映像 stage），远程资产名/版本号等任何外部输入都不流向命令行；
//   - 纵深防御：拒绝空路径、非绝对路径与含引号/控制字符（" \r \n \t）的路径——
//     需要转义碰运气的形状直接拒绝，宁可提取失败也不给解释器留歧义空间。
func buildMSIExtractCmd(msiPath, stage string) (*exec.Cmd, error) {
	for _, p := range []struct{ label, path string }{{"MSI 缓存路径", msiPath}, {"TARGETDIR", stage}} {
		if strings.TrimSpace(p.path) == "" {
			return nil, fmt.Errorf("%s 不能为空", p.label)
		}
		if !filepath.IsAbs(p.path) {
			return nil, fmt.Errorf("%s 必须是绝对路径: %q", p.label, p.path)
		}
		if strings.ContainsAny(p.path, "\"\r\n\t") {
			return nil, fmt.Errorf("%s 含引号或控制字符，拒绝进入 msiexec 命令行: %q", p.label, p.path)
		}
	}
	return exec.Command("msiexec.exe", "/a", msiPath, "/qn", "TARGETDIR="+stage), nil
}

// findPayloadDir 在管理映像根下递归定位含目标 exe 的目录（大小写不敏感）。
// 只取第一个命中；正常映像内 exe 唯一。
func findPayloadDir(stageRoot, exe string) string {
	wanted := strings.ToLower(exe)
	var found string
	_ = filepath.WalkDir(stageRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" || d.IsDir() {
			return nil //nolint:nilerr // 遍历中的局部 IO 错误跳过即可
		}
		if strings.ToLower(d.Name()) == wanted {
			found = filepath.Dir(path)
		}
		return nil
	})
	return found
}

// copyTree 把 src 目录内容整体复制到 dst（保留相对子目录结构）。
// 文件统一 0755：MSI 有效载荷恒含主 exe，且目标只在托管隔离目录内。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if rel == "." {
				return nil // dst 已由 StageDir 独占创建，不重复建
			}
			return os.MkdirAll(target, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFileTo(path, target)
	})
}

// ---------- 本地导入（Keyviz 领域流程，无内核对应物） ----------

// ImportLocal 导入本地已安装的 Keyviz（官方 MSI 安装版目录即可：Program Files\keyviz）。
// 与 piclite 同构：Keyviz 配置恒在 %APPDATA%\org.keyviz\store.json 用户目录，
// 与 exe 位置无关，故只迁移 exe 一个文件。
// 调用方需先确保源实例未运行（运行中的 exe 被 Windows 独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (KeyvizVersionInfo, error) {
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() {
		return KeyvizVersionInfo{}, fmt.Errorf("源目录未找到 %s: %s", exeName, srcDir)
	}

	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainVersionRe.MatchString(version) {
		// 版本探测失败（非 Windows 平台或资源缺失）：时间戳兜底，与 frpc ImportLocal 同构
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return KeyvizVersionInfo{}, fmt.Errorf("版本 v%s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return KeyvizVersionInfo{}, err
	}

	if err := copyFileTo(srcExe, filepath.Join(targetDir, exeName)); err != nil {
		_ = os.RemoveAll(targetDir)
		return KeyvizVersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
		"copied":      exeName,
	})

	return KeyvizVersionInfo{
		Version:     "v" + version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        dirSize(targetDir, fi.Size()),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
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
		slog.Debug("keyviz 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
