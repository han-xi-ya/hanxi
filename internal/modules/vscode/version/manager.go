package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"

	"hanxi/packages/go/netx"
)

const (
	exeName         = "Code.exe"     // 便携版与安装版主程序同名（product.nameShort 实证）
	codeCmdRel      = "bin/code.cmd" // 便携归档恒定成员（CLI 信使脚本，布局自检依据）
	productJSONMark = "resources/app/product.json"
	dataDirName     = "data"   // 官方便携模式触发器：Code.exe 同目录存在即数据全自包含
	treeEntryName   = "vscode" // 版本树目录前缀（<root>/vscode_<version>，与 ccswitch_ 同构）
	dirPrefix       = treeEntryName + "_"
	fetchBudget     = 15 * time.Minute // 下载总超时预算（沿用原下载客户端 15 分钟口径：zip ~330MB）

	// moduleMetaFileName 模块侧账本补充：内核 meta.json 只承载通用字段，
	// VS Code 的前端契约字段（verifiedHash 官方哈希校验标记、构建 commit、
	// 导入标记与来源明细等）属模块业务态，依 ADR-0002 §5 纪律自持落位于此。
	moduleMetaFileName = "meta.module.json"
)

// portableDirRe 版本目录名（vscode_1.136.1；imported- 收纳版本探测失败的导入）。
// 与 Tree 的通用令牌白名单取交集使用：目录形状判定保留本模块历史口径。
var portableDirRe = regexp.MustCompile(`^(?:[0-9][0-9a-zA-Z.]+|imported-\d{8}-\d{6})$`)

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 字节双核 +
// 流式上限），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// Manager VS Code 版本管理引擎：远程列表与便携版"下载 → 校验 → 解包 → 落位"
// 主流程委托 Wave 4 共享内核 packages/go/artifact（Fetch + UnpackZip + Tree）；
// 本包保留领域知识：官方更新网关直链解析、双形态资产策略（安装版 Inno 交互
// 安装留模块 bespoke）、便携布局锚点自检（exe + bin/code.cmd +
// */resources/app/product.json + data\ 激活器）、版本目录形状
// （x.y.z / imported-时间戳）、本地导入整套迁移与既有进度词表映射。
//
// 完整性策略（上游官方 sha256 仅最新版可查，见 remote.go，ADR-0002 §5 同款
// 分治）：
//  1. latest（列表首项且 SHA256 非空）：委托内核 Fetch 以官方 sha256 为信任根
//     做流式 + 落盘双核 + 字节数断言，解包仍 artifact.UnpackZip（闸门更全）；
//  2. 历史版本：无官方摘要即无信任根，内核拒无校验安装——"下载+字节数"段留
//     模块 bespoke（downloadTo 重试链，微软 CDN 偶发抖动依赖重试），解包/落位
//     仍委托内核；meta 记 verifiedHash=false 供 UI 如实展示（降级三层：
//     字节数 + CRC32/布局闸门 + 布局自检）。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree
	client      *http.Client // 降级链下载客户端（长超时：安装器 ~120MB / zip ~330MB）

	fetch fetcher
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		client:      netx.NewClient(15*time.Minute, nil),
		fetch:       artifact.Fetch,
	}
}

// OpenTree 打开 VS Code 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取指定形态的远程可用版本（10 分钟内命中缓存）。
// N13 形态标注：逐行回填 Form=查询形态——缓存命中时包级 ListRemote 交回的
// 是缓存共享切片，先拷贝断开引用再回填（不污染缓存源，免与并发读互相踩踏）；
// 形态归一与 platformOf 同口径（非 installer 一律按便携链取数，标注随之）。
func (m *Manager) ListRemote(form Form) ([]Release, error) {
	list, err := ListRemote(form)
	if err != nil {
		return nil, err
	}
	if form != FormInstaller {
		form = FormPortable
	}
	out := make([]Release, len(list))
	copy(out, list)
	for i := range out {
		out[i].Form = form
	}
	return out, nil
}

// ---------- 便携版 ----------

// ListInstalled 扫描本地已安装便携版目录（委托 Tree 扫描，最新在前）。
// Code.exe 缺失/为空或 bin/code.cmd 缺失均视为损坏安装跳过（官方归档恒含二者）。
// 账本双轨：新下载链 = 内核 meta.json（artifact.Meta）+ 模块 meta.module.json；
// 导入链与迁移前的历史账本 = 模块自写 map 形态 meta.json。
func (m *Manager) ListInstalled() ([]VersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []VersionInfo
	for _, e := range vers {
		if !portableDirRe.MatchString(e.Version) {
			continue // 形状外令牌（含历史/外来目录）不列入，与迁移前目录名白名单同口径
		}
		dir := e.Dir
		if !validPortableLayout(dir) {
			continue
		}
		exe := filepath.Join(dir, exeName)
		fi, _ := os.Stat(exe) // validPortableLayout 已保证存在

		info := VersionInfo{
			Version: e.Version,
			ExePath: exe,
			Dir:     dir,
			Size:    dirSize(dir, fi.Size()),
		}
		if e.Meta.Schema != 0 {
			// 新下载链：安装时刻由内核统一账本承载（RFC3339 落盘、展示层归一）
			if !e.Meta.InstalledAt.IsZero() {
				info.InstalledAt = e.Meta.InstalledAt.Local().Format("2006-01-02 15:04:05")
			}
			mm := readModuleMeta(dir)
			info.IsImport = mm.IsImport
			info.Source = mm.Source
			info.Verified = mm.VerifiedHash
		} else {
			legacyAt, isImport, src, verified := readLegacyMetaFields(dir)
			info.InstalledAt = legacyAt
			info.IsImport = isImport
			info.Source = src
			info.Verified = verified
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// validPortableLayout 便携目录布局判定：Code.exe 非空 + bin/code.cmd 存在。
func validPortableLayout(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, exeName))
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(codeCmdRel))); err != nil || fi.IsDir() {
		return false
	}
	return true
}

// hasProductJSONAnchor Electron 运行时目录名 = commit 前 10 位（随版本漂移，
// 只能按"存在某个 */resources/app/product.json"判定，不可写死目录名）。
// 解包后的 staging 目录树上遍历（迁移前在 zip 条目名上顺手判定，语义等价）。
func hasProductJSONAnchor(dir string) bool {
	var errWalk error
	found := false
	errWalk = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // 遍历故障：按"缺锚点"拒装处理
		}
		if d.IsDir() || filepath.Base(path) != "product.json" {
			return nil
		}
		if rel, rerr := filepath.Rel(dir, path); rerr == nil &&
			strings.HasSuffix(filepath.ToSlash(rel), productJSONMark) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found && errWalk == nil
}

// EnsureDataDir 保证便携版数据目录存在（幂等）：data\ 是官方便携模式激活器，
// 缺失时实例会把数据写回 %APPDATA%\Code，破坏托管隔离承诺——启动前必须兜底。
func (m *Manager) EnsureDataDir(version string) error {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(dir, dataDirName), 0755)
}

// ResolveExe 返回指定版本便携 Code.exe 路径（不存在返回错误）。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, exeName), nil
}

// Remove 卸载指定便携版本（委托 Tree：rename 隔离后删除，文件占用时留下可恢复
// 状态）。刻意不动 data\：用户装过的扩展/配置随目录一起删属预期行为，但调用方
// （service 层）须先确保该版本实例未运行。
func (m *Manager) Remove(version string) error {
	_, token, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return m.tree.Remove(token, nil)
}

// resolveVersionDir 定位版本隔离目录（vscode_X.Y.Z 或 vscode_imported-时间戳）。
// 形状外令牌（含路径穿越）先于任何磁盘访问被拒，错误口径与原实现一致。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	token = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !portableDirRe.MatchString(token) {
		return "", "", fmt.Errorf("非法版本号: %q", version)
	}
	if d, rerr := m.tree.Resolve(token); rerr == nil {
		return d, token, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", version)
}

// ImportLocal 导入本地便携版 VS Code 目录（整套迁移，data\ 除外——导入即新环境）。
// 安装版目录（含 unins000.exe）拒绝导入：安装版由注册表自动感知，无需迁移。
// 刻意不经 Tree/UnpackZip：整树复制迁移无 zip 语义可委托，落位账本沿用模块
// 自写 map 形态（与历史导入目录兼容，ListInstalled 双轨读取覆盖）。
func (m *Manager) ImportLocal(srcDir string) (VersionInfo, error) {
	srcDir = strings.TrimSpace(srcDir)
	if !validPortableLayout(srcDir) {
		return VersionInfo{}, fmt.Errorf("源目录不是有效的 VS Code 便携版（缺少 %s 或 %s）: %s", exeName, codeCmdRel, srcDir)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "unins000.exe")); err == nil {
		return VersionInfo{}, fmt.Errorf("源目录是安装版（含卸载器）：安装版会被自动感知，无需导入")
	}

	srcExe := filepath.Join(srcDir, exeName)
	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr != nil || !plainSemver.MatchString(version) {
		version = "imported-" + time.Now().Format("20060102-150405")
	}
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		return VersionInfo{}, fmt.Errorf("版本 %s 已存在，请先卸载再导入", version)
	}

	fi, err := os.Stat(srcExe)
	if err != nil {
		return VersionInfo{}, err
	}

	if err := copyTreeExcept(srcDir, targetDir, []string{dataDirName, "meta.json", moduleMetaFileName}); err != nil {
		_ = os.RemoveAll(targetDir)
		return VersionInfo{}, fmt.Errorf("迁移便携版目录失败: %w", err)
	}
	// 导入即自包含：强制补齐 data\，绝不把源目录旧数据搬进来
	if err := os.MkdirAll(filepath.Join(targetDir, dataDirName), 0755); err != nil {
		_ = os.RemoveAll(targetDir)
		return VersionInfo{}, err
	}

	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": time.Now().Format("2006-01-02 15:04:05"),
		"isImport":    true,
		"source":      srcDir,
	})
	return VersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, exeName),
		Dir:         targetDir,
		Size:        dirSize(targetDir, fi.Size()),
		InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// Download 下载指定形态版本：
//   - portable：zip → 校验 → 保布局解压隔离落位 versions/vscode_X.Y.Z/ + 补建 data\；
//   - installer：exe → 校验 → 静默运行官方 Inno 安装器（安装位置随本机既有安装）。
//
// 便携形态的解包/落位委托内核（UnpackZip 全闸门 + Tree staging/Commit 原子落位，
// 取代原 extractAll 直写最终目录——半件即污染安装的旧弱点）；下载段按 ADR-0002 §5
// 信任根分治：最新版委托 artifact.Fetch 官方 sha256 双核，历史版无官方摘要
// （上游接口形态如此）走模块降级链（downloadTo 重试 + 字节数核对）。安装版
// 形态是 Inno 交互安装语义（msiexec/Setup.exe 非 artifact 策略族），下载/安装
// 全段留模块 bespoke。
//
// 进度回调沿用本模块既有词表（downloading/verify/extract/install/done/error）。
//
// txnID 为调用方事务 ID（journal 背书用：便携版 staging 目录名 .tmp-<txnID>，
// 崩溃恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue；
// 安装版无 staging，仅随事务留账不落地使用）。
// Download 保留旧调用面，供版本包单测与非事务调用使用。
func (m *Manager) Download(txnID, version string, form Form, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, form, onProgress)
}

// DownloadContext 下载并安装，可由事务 context 取消（P0 批 2b 生命周期）。
// 取消边界：两条传输链（内核 Fetch / 官方源降级链）均即时中止；解包/换目录
// 前后审取消；静默安装器（Inno）一旦拉起即走完——半途放弃比跑完更危险。
func (m *Manager) DownloadContext(ctx context.Context, txnID, version string, form Form, onProgress func(p DownloadProgress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: version, Form: string(form), Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	rel, err := m.resolveDownloadable(form, version)
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("解析下载直链失败: %v", err))
		return err
	}

	ext := ".zip"
	if form == FormInstaller {
		ext = ".exe"
	}
	tmp, err := os.CreateTemp("", "hanxi-vscode-*"+ext)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	tmp.Close()

	verified := false
	if form == FormPortable && rel.SHA256 != "" {
		// 层 1+2（最新版）：内核 Fetch 官方 sha256 流式/落盘双核 + 字节数与
		// 流式上限双核，全部收口委托
		emit("downloading", 0, rel.Size, "")
		src := artifact.Source{
			URL:      rel.DownloadURL,
			SHA256:   rel.SHA256,
			MaxBytes: rel.Size, // 与 HEAD 实测大小对齐：超限即断，杜绝异常放大
			FileName: rel.AssetName,
		}
		if ferr := m.fetch(ctx, src, tmpPath, func(p artifact.Progress) {
			// 内核进度 → 既有词表：只有流式下载阶段对应 downloading，
			// verify（官方摘要双核）由内核折进 download，不造幻影步骤
			if p.Stage == artifact.StageDownload {
				emit("downloading", p.Done, p.Total, "")
			}
		}, fetchBudget); ferr != nil {
			emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", ferr))
			return ferr
		}
		verified = true
	} else {
		// 降级链（历史便携版无官方摘要 / 安装版）：downloadTo 重试 + 字节数核对。
		// 安装版另有 MZ 魔数形态兜底（rustdesk 单 exe 先例），见下方 verify 段。
		emit("downloading", 0, rel.Size, "")
		if derr := downloadTo(ctx, m.client, rel.DownloadURL, tmpPath, func(done int64) {
			emit("downloading", done, rel.Size, "")
		}); derr != nil {
			emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", derr))
			return derr
		}
	}

	// 层 2：字节数（防截断/代理篡改；Fetch 路径内核已双核，此处覆盖降级链）
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

	// 层 1：官方 sha256（仅最新版有；历史版本如实降级）——Fetch 路径已在内核
	// 内完成，本阶段作为进度叙事保留（迁移前词表含 verify，不删步防前端断档）
	emit("verify", 0, 0, "")
	if form == FormInstaller && rel.SHA256 != "" {
		if err := verifySHA256(tmpPath, rel.SHA256); err != nil {
			emit("error", 0, rel.Size, err.Error())
			return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
		}
		verified = true
	} else if form == FormInstaller {
		// 安装器无官方哈希时的形态兜底：MZ 魔数（rustdesk 单 exe 先例）
		if err := verifyMZ(tmpPath); err != nil {
			emit("error", 0, rel.Size, err.Error())
			return err
		}
	}

	if form == FormInstaller {
		if err := ctx.Err(); err != nil {
			emit("error", 0, 0, err.Error())
			return err
		}
		emit("install", 0, 0, "")
		if err := runInstallerSilent(tmpPath, rel.Version); err != nil {
			emit("error", 0, 0, fmt.Sprintf("静默安装失败: %v", err))
			return err
		}
		emit("done", 100, 100, "")
		return nil
	}

	// 便携版：解包进独占中转目录（staging 与最终目录同卷，供原子落位；
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）
	if err := artifact.ValidateVersionToken(version); err != nil {
		emit("error", 0, 0, err.Error())
		return err
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
	// 官方归档 zip 无根目录，Code.exe 落在解包根——ZipSlip/炸弹/CRC32 全量闸门
	// 收口于内核（取代原 extractAll；每个 entry 读满触发 CRC 复核的纪律由内核承接）
	if err := artifact.UnpackZipContext(ctx, tmpPath, staging, artifact.DefaultLimits, nil); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}
	// 层 4：布局自检（Code.exe + bin/code.cmd + */resources/app/product.json，
	// 模块策略内核不感知）；不符即丢弃 staging 报错，最终目录不出现半件
	if !validPortableLayout(staging) {
		err := fmt.Errorf("zip 布局无效：缺少 %s 或 %s", exeName, codeCmdRel)
		emit("error", 0, 0, err.Error())
		return err
	}
	if !hasProductJSONAnchor(staging) {
		err := fmt.Errorf("zip 布局无效：缺少 %s", productJSONMark)
		emit("error", 0, 0, err.Error())
		return err
	}
	// 便携模式激活器：data\ 目录（官方文档实证——存在即数据全自包含）
	if err := os.MkdirAll(filepath.Join(staging, dataDirName), 0755); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}

	// 模块侧账本（前端契约字段与诊断信息，内核 meta.json 不收）；随后
	// 原子落位 + 写内核统一账本（同版本同摘要幂等，异摘要拒绝——防内容漂移）
	if err := writeModuleMeta(staging, moduleMeta{
		Source:       rel.AssetName,
		VerifiedHash: verified,
		Commit:       rel.Commit,
		ZipSize:      actual,
		ComputedSHA:  fileSHA256(tmpPath),
	}); err != nil {
		emit("error", 0, 0, fmt.Sprintf("写入来源账目失败: %v", err))
		return err
	}
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   rel.SHA256, // 官方包摘要（Fetch 路径内核已全核复核；降级链如实留空）
		AssetSHA256: fileSHA256(filepath.Join(staging, exeName)),
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, version, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("落位失败: %v", err))
		return err
	}

	emit("done", 100, 100, "")
	return nil
}

// resolveDownloadable 定位版本下载信息：优先远程缓存；缓存未命中
// （展示窗口之外的旧版本）则现场 HEAD 解析直链。
func (m *Manager) resolveDownloadable(form Form, version string) (Release, error) {
	list, lerr := ListRemote(form)
	if lerr == nil {
		if rel, ok := lookupRemote(list, version); ok {
			return rel, nil
		}
	}
	rel, err := resolveRelease(downloadClient(), form, version)
	if err != nil {
		if lerr != nil {
			return Release{}, fmt.Errorf("版本列表获取失败且 %s 直链解析失败: %v / %v", version, lerr, err)
		}
		return Release{}, fmt.Errorf("版本 %s 无有效下载直链: %w", version, err)
	}
	return rel, nil
}

// ---------- 账本 ----------

// moduleMeta 模块侧账本内容（meta.module.json）：verifiedHash 为前端 VersionInfo
// 契约字段（是否经官方 sha256 校验安装），source/commit/zipSize/computedSHA 为
// 迁移前 meta.json 诊断字段的如实搬迁，isImport/source 承载导入来源明细。
type moduleMeta struct {
	Source       string `json:"source,omitempty"`
	IsImport     bool   `json:"isImport,omitempty"`
	VerifiedHash bool   `json:"verifiedHash,omitempty"`
	Commit       string `json:"commit,omitempty"`
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

// readLegacyMetaFields 读取模块自写的导入账本与迁移前历史下载账本（无 schema 的
// map 形态 meta.json）。新下载链的 artifact.Meta 账本（含 schema）不走本函数。
func readLegacyMetaFields(dir string) (installedAt string, isImport bool, source string, verified bool) {
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
	verified, _ = mm["verifiedHash"].(bool)
	return installedAt, isImport, source, verified
}

// ---------- 本地文件工具（导入链与降级账本专用） ----------

// copyTreeExcept 整树复制（跳过 skip 名单中的顶层项），用于本地导入。
func copyTreeExcept(srcDir, dstDir string, skip []string) error {
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return os.MkdirAll(dstDir, 0755)
		}
		top := strings.Split(filepath.ToSlash(rel), "/")[0]
		if skipSet[top] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dstDir, rel)
		if info.IsDir() {
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

// dirSize 版本目录整树字节和：主程序文件只是入口，多文件载荷才是体量的
// 主体，单报主 exe 尺寸与真实安装体量级失真。dirstats 度量恒跳过符号链接/
// 重解析点防环；2s 挂钟预算超限或度量失败回退旧口径（主程序文件大小）并
// Debug 报账，不谎报全量。
func dirSize(dir string, fallback int64) int64 {
	st := dirstats.MeasureBudgeted(dir, 2*time.Second)
	if st.Err != nil || st.Partial || st.Bytes <= 0 {
		slog.Debug("vscode 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
