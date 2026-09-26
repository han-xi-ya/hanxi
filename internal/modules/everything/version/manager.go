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
	"sort"
	"strings"
	"time"

	"hanxi/internal/platform/versioncmp"
	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/dirstats"
)

const (
	// treeEntryName 版本树目录前缀（<root>/everything_<token>，历史布局
	// everything_v1.4.1.1032 以 token "v1.4.1.1032" 表达，v 前缀留在令牌内）。
	treeEntryName = "everything"
	// dirPrefix 导入链直造目录时的版本隔离目录前缀（与 Tree 的 entry 命名同源：
	// everything_ + "v" == everything_v）。
	dirPrefix = treeEntryName + "_" + "v"
	// fetchBudget 单次下载的总超时预算（沿用原下载客户端 10 分钟口径）。
	fetchBudget = 10 * time.Minute
)

// exeCandidates 各通道便携 zip 内的 exe 命名不统一（1.4 为小写 everything.exe、1.5 为 Everything.exe），
// 定位时大小写不敏感逐一尝试。
var exeCandidates = []string{"Everything.exe", "everything.exe"}

// fetcher 受控下载接缝：默认为内核 artifact.Fetch（官方摘要必检 + 流式上限 +
// 落盘全核），失败注入测试替换为模拟中断/坏摘要源。
type fetcher func(ctx context.Context, src artifact.Source, destPath string, prog func(artifact.Progress), timeout time.Duration) error

// enricher 槽位活体补齐接缝（HEAD 补大小 + 官方 sha256 清单），测试注入恒等函数
// 以隔离远程解析层。
type enricher func(rel EverythingRelease) EverythingRelease

// Manager Everything 版本管理引擎："下载 → 校验 → 解包 → 落位"主流程委托
// Wave 4 共享内核 packages/go/artifact（Fetch + UnpackZip + Tree）；本包只保留
// Everything 领域知识：官网下载页槽位解析（remote.go——非标准 releases 形状，
// 资产 URL/digest 的解析层留模块，Fetch 只管拿 bytes+验摘要）、Everything.exe
// 大小写容错锚点与平铺布局自检、本地整套导入链、既有进度词表映射。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	fetch  fetcher
	enrich enricher
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		fetch:       artifact.Fetch,
		enrich:      enrichLive,
	}
}

// OpenTree 打开 Everything 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取远程可用版本槽位（10 分钟缓存，失败降级快照）。
// N13 形态标注：逐行回填 Form=hostedForm——与 snipaste（get 交回克隆切片、
// 回填就地安全）不同，本模块缓存命中/快照路径交回的是共享底层切片
// （releaseCache.data 与内置 snapshotReleases），回填前先 copy 再动，
// 杜绝污染缓存源与快照。
func (m *Manager) ListRemote() ([]EverythingRelease, error) {
	list, err := remoteCache.get()
	if err != nil {
		return nil, err
	}
	out := make([]EverythingRelease, len(list))
	copy(out, list)
	for i := range out {
		out[i].Form = hostedForm
	}
	return out, nil
}

// ListInstalled 扫描本地已安装版本目录（委托 Tree 扫描）。
// 目录命名 everything_vX.Y.Z（token 含 v 前缀，与历史布局一致）；
// token 剥 v 后仅接受 x.y.z 数字起头形状（imported-指纹 等导入兜底目录
// 沿历史口径不列入）；exe 缺失/为空视为损坏安装跳过（配置与索引库若损坏属
// Everything 运行期问题，不在此拦截）。
// 排序收口在本层：Tree 按目录令牌（v 前缀）做数值分段比较会退化字典序
// （1.10 与 1.5 一类多位数段会错序），剥 v 后用 versioncmp 重排为最新在前。
func (m *Manager) ListInstalled() ([]EverythingVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}

	var list []EverythingVersionInfo
	for _, v := range vers {
		version, ok := versionFromToken(v.Version)
		if !ok {
			continue
		}
		exe, ok := findExe(v.Dir)
		if !ok {
			continue
		}
		fi, statErr := os.Stat(exe)
		if statErr != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue
		}

		info := EverythingVersionInfo{
			Version: version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    dirSize(v.Dir, fi.Size()),
		}
		// 账本双形态：新下载链走内核统一账本（artifact.Meta，含 schema），来源
		// 展示按官方资产名重建；导入链与迁移前的历史账本走模块自写 map 形态
		// （installedAt 原样展示、isImport/source 携带）。
		if !v.Meta.InstalledAt.IsZero() {
			info.InstalledAt = v.Meta.InstalledAt.Local().Format("2006-01-02 15:04:05")
			if v.Meta.Source == artifact.SourceRemote {
				info.Source = assetName(version)
			}
		}
		if info.InstalledAt == "" {
			legacyAt, isImport, src := readLegacyMetaFields(v.Dir)
			info.InstalledAt = legacyAt
			info.IsImport = isImport
			info.Source = src
		}
		if info.InstalledAt == "" {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	sort.SliceStable(list, func(i, j int) bool {
		return versioncmp.Compare(list[i].Version, list[j].Version) > 0
	})
	return list, nil
}

// Download 下载官方 x64 便携 zip 并安装到 versions/everything_v<版本>/。
// 完整性主流程收口至内核 artifact.Fetch：以 voidtools 官方 sha256 清单中该资产
// 的哈希（remote.go 解析层已入 EverythingRelease.SHA256）为信任根做流式 + 落盘
// 双 SHA-256 校验，Content-Length 与流式上限双核；解包经 artifact.UnpackZip
// （ZipSlip/炸弹/CRC32 全量闸门，取代原 extractAll），落位经 Tree.Commit
// （staging + 原子 rename，同版本异摘要防漂移——原实现直写最终目录，半件即污染安装）。
// 原"四级完整性"链的归属：第 1 级（官方清单 sha256）升级为必检——清单缺失不再
// 降级直装，与内核"无校验安装一律拒绝"的裁定对齐；第 2 级（HEAD 声明字节数）
// 保留为 Fetch 后的活体双核；第 3 级（zip 逐条目 CRC32）由 UnpackZip 收口；
// 第 4 级（Everything.exe 大小写容错锚点 + 平铺布局自检）属领域判定，留本模块
// Commit 前自检（仿 checkPortableLayout）。
// 进度回调沿用本模块既有词表（downloading/verify/extract/done/error），不发明新词
// ——内核的 verify 阶段与官方摘要校验同名，如实映射而非折并造幻影。
//
// txnID 为调用方事务 ID（journal 背书用：staging 目录名 .tmp-<txnID>，崩溃
// 恢复据此按事务定位并清理现场，见 internal/ops.CleanTxnResidue）。
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

	// 1. 解析目标版本对应的远程槽位（模块知识：官网下载页解析，列表可能来自
	// stale 缓存，下载前补一次活体详情，保证校验强度）
	releases, err := m.ListRemote()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *EverythingRelease
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
	if rel.Stale || rel.SHA256 == "" || rel.Size == 0 {
		r := m.enrich(*rel)
		rel = &r
	}
	// 官方摘要信任根：voidtools 按版本发布 Everything-<v>.sha256 清单（覆盖全部
	// 资产）。清单不可得即拒装——不再沿原"四级兜底"第 1 级的降级直装形态
	// （内核纪律：托管下载不允许无校验安装）。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 Everything %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpZip, err := os.CreateTemp("", "hanxi-everything-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)
	tmpZip.Close()

	// 2. 受控下载（voidtools 无镜像，单源直连；下载/摘要双核/字节数双核全部委托内核）
	src := artifact.Source{
		URL:      rel.AssetURL,
		SHA256:   rel.SHA256,
		MaxBytes: rel.Size, // 与 HEAD 声明大小对齐：超限即断，杜绝异常放大
		FileName: assetName(version),
	}
	emit("downloading", 0, rel.Size, "")
	fetchErr := m.fetch(ctx, src, tmpZipPath, func(p artifact.Progress) {
		// 内核进度 → 既有词表：download 对应 downloading（内核未见 Content-Length
		// 时回退槽位声明的 HEAD 大小，进度条口径不劣于原实现）；verify 与既有
		// 词表同名如实映射；其余阶段本模块不上报
		switch p.Stage {
		case artifact.StageDownload:
			total := p.Total
			if total == 0 {
				total = rel.Size
			}
			emit("downloading", p.Done, total, "")
		case artifact.StageVerify:
			emit("verify", 0, 0, "")
		}
	}, fetchBudget)
	if fetchErr != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", fetchErr))
		return fetchErr
	}
	// HEAD 声明字节数活体双核（Fetch 已核 GET 侧 Content-Length 与流式上限，
	// 此处对齐槽位探测声明值，保留原"四级完整性"第 2 级口径）
	if rel.Size > 0 {
		if fi, serr := os.Stat(tmpZipPath); serr != nil {
			emit("error", 0, rel.Size, fmt.Sprintf("读取临时文件失败: %v", serr))
			return serr
		} else if fi.Size() != rel.Size {
			err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", rel.Size, fi.Size())
			emit("error", 0, rel.Size, err.Error())
			return err
		}
	}

	// 3. 解包进独占中转目录（staging 与最终目录同卷，供原子落位；
	// 目录名 .tmp-<txnID> 由事务 ID 派生，journal 背书恢复据此收口现场）
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
	if err := artifact.UnpackZipContext(ctx, tmpZipPath, staging, artifact.DefaultLimits, nil); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}
	// 4. 大小写容错锚点 + 平铺布局自检（模块策略，内核不感知）
	exeName, err := checkPortableLayout(staging)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	// exe 诊断摘要入账（ListInstalled 不展示，纯账本诊断）
	assetSHA, _ := fileSHA256(filepath.Join(staging, exeName))

	// 5. 原子落位 + 写账本（meta.json 由内核统一形状落盘；同版本同摘要幂等，
	// 异摘要拒绝——防止同版本号内容漂移）。token 携 v 前缀对齐历史目录名。
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   rel.SHA256,
		AssetSHA256: assetSHA,
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, "v"+version, meta); err != nil {
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

// ResolveExe 返回指定版本的 Everything.exe 路径（不存在返回错误）
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	exe, ok := findExe(dir)
	if !ok {
		return "", fmt.Errorf("版本 %s 安装损坏：缺少 Everything.exe", version)
	}
	return exe, nil
}

// resolveVersionDir 定位版本隔离目录（everything_vX.Y.Z）。形状外令牌（含路径
// 穿越）先于任何磁盘访问被拒，错误口径与原实现一致。
func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	ver := strings.TrimSpace(version)
	if !plainVersionRe.MatchString(ver) {
		return "", "", fmt.Errorf("非法版本号: %q", ver)
	}
	if d, rerr := m.tree.Resolve("v" + ver); rerr == nil {
		return d, "v" + ver, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先在下方版本管理下载或导入", ver)
}

// versionFromToken 把 Tree 扫出的版本令牌规范化为裸版本号
// （v1.5.0.1422b → 1.5.0.1422b，与原目录名剥离 everything_v 前缀的口径一致）。
// 刻意要求 v 前缀（原 dirNameRe `^everything_v[0-9]...` 同款严格）：
// everything_1.2.3 之类无前缀外来目录不列入（列了也无法按裸版本 Remove）；
// everything_vimported-<指纹> 之类导入兜底目录沿历史口径不列入。
func versionFromToken(token string) (string, bool) {
	rest, hadV := strings.CutPrefix(strings.TrimSpace(token), "v")
	if hadV && plainVersionRe.MatchString(rest) {
		return rest, true
	}
	return "", false
}

// checkPortableLayout 大小写容错锚点自检（模块策略）：staging 平铺根内存在
// 非空 Everything 主 exe（1.4 小写/1.5 大写/模糊兜底）。返回磁盘实际条目名
// 供账本 Entry 记录（Windows 大小写不敏感，findExe 命中的是候选拼写而非实名，
// 故按目录枚举做 EqualFold 还原）；不符即判定安装无效，调用方丢弃 staging。
func checkPortableLayout(staging string) (string, error) {
	exe, ok := findExe(staging)
	if !ok {
		return "", fmt.Errorf("zip 布局无效：缺少 Everything.exe")
	}
	fi, err := os.Stat(exe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return "", fmt.Errorf("zip 布局无效：缺少可用的 Everything.exe")
	}
	base := filepath.Base(exe)
	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", fmt.Errorf("读取中转目录失败: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), base) {
			return e.Name(), nil
		}
	}
	return base, nil
}

// assetName 按官方资产命名模板拼 x64 便携 zip 文件名（落盘名与来源展示共用）。
func assetName(version string) string {
	return fmt.Sprintf("Everything-%s.x64.zip", version)
}

// enrichLive 对 stale/字段缺失的槽位记录做活体补齐：HEAD 补大小与时间、拉取官方 sha256。
func enrichLive(rel EverythingRelease) EverythingRelease {
	probeAssets([]EverythingRelease{rel})
	return rel
}

// readLegacyMetaFields 读取模块自写的导入账本与迁移前历史账本（无 schema 的
// map 形态）：installedAt 原样字符串、isImport 布尔、source 来源（资产名/导入目录）。
// 新下载链的 artifact.Meta 账本（含 schema）不走本函数（其 Source 为 "remote"
// 语义，展示来源由调用方按资产名重建）。
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

// ---------- 领域流程（不进内核的部分） ----------

// findExe 在目录内大小写不敏感定位 Everything.exe（1.4 小写 / 1.5 大写）。
func findExe(dir string) (string, bool) {
	for _, name := range exeCandidates {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode().IsRegular() {
			return p, true
		}
	}
	// 兜底：大小写之外的未来命名（如 Everything64.exe）不硬枚举，读目录模糊匹配
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			if n := e.Name(); strings.HasPrefix(strings.ToLower(n), "everything") && strings.HasSuffix(strings.ToLower(n), ".exe") {
				return filepath.Join(dir, n), true
			}
		}
	}
	return "", false
}

// ImportLocal 导入本地便携安装整套（exe + 配置 + 语言包 + 索引库），保留用户的定制体验。
// 与 frpc 只拷单 exe 的 ImportLocal 不同：Everything 的价值一半在索引库与配置上。
// 离线导入通道不依赖远程摘要（内核纪律仅约束远程下载链），账本沿模块自写 map
// 形态落盘，ListInstalled 经 readLegacyMetaFields 回读。
// 调用方需先确保源目录实例未运行（索引库被写锁时拷贝结果不可信）。
func (m *Manager) ImportLocal(srcDir string) (EverythingVersionInfo, error) {
	srcExe, ok := findExe(srcDir)
	if !ok {
		return EverythingVersionInfo{}, fmt.Errorf("源目录未找到 Everything.exe: %s", srcDir)
	}
	fi, err := os.Stat(srcExe)
	if err != nil {
		return EverythingVersionInfo{}, err
	}

	version := importVersionTag(srcExe, fi)
	targetDir := filepath.Join(m.versionsDir, dirPrefix+version)
	if _, err := os.Stat(targetDir); err == nil {
		if strings.HasPrefix(version, importedTagPrefix) {
			// 兜底目录沿历史口径不进版本面板——查重报错必须自带目录路径，
			// 否则用户面对"先卸载"指引无处下手（面板里没有这一行）。
			return EverythingVersionInfo{}, fmt.Errorf("该源已导入过（兜底版本 %s），先手动删除目录再重试：%s", version, targetDir)
		}
		return EverythingVersionInfo{}, fmt.Errorf("版本 %s 已安装，请先卸载再导入", version)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return EverythingVersionInfo{}, err
	}

	// 白名单迁移：exe + 配置/语言/索引数据（ini/lng/db）+ 会话 + 插件目录。
	// 临时与锁文件（*.tmp、~*、含 "-wal"/"-shm" 的件）坚决不碰，保证拷贝的是完整一致的索引库。
	var copied []string
	entries, _ := os.ReadDir(srcDir)
	for _, e := range entries {
		name := e.Name()
		if isTempLike(name) {
			continue
		}
		if e.IsDir() {
			if strings.EqualFold(name, "Plugins") {
				if err := copyDirTo(filepath.Join(srcDir, name), filepath.Join(targetDir, name)); err != nil {
					_ = os.RemoveAll(targetDir)
					return EverythingVersionInfo{}, err
				}
				copied = append(copied, name+"/")
			}
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if strings.EqualFold(name, filepath.Base(srcExe)) ||
			ext == ".ini" || ext == ".lng" || ext == ".db" || name == "Session.json" {
			if err := copyFileTo(filepath.Join(srcDir, name), filepath.Join(targetDir, name)); err != nil {
				_ = os.RemoveAll(targetDir)
				return EverythingVersionInfo{}, err
			}
			copied = append(copied, name)
		}
	}

	// 一次取值双用：账本落盘与返回结构共享同一时间戳，杜绝跨秒分裂
	// （面板刷新前读 meta、刷新后读列表，两处 installedAt 显示不一致）。
	installedAt := time.Now().Format("2006-01-02 15:04:05")
	_ = writeJSON(filepath.Join(targetDir, "meta.json"), map[string]any{
		"installedAt": installedAt,
		"isImport":    true,
		"source":      srcDir,
		"copied":      strings.Join(copied, ", "),
	})

	return EverythingVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(targetDir, filepath.Base(srcExe)),
		Dir:         targetDir,
		Size:        dirSize(targetDir, fi.Size()),
		InstalledAt: installedAt,
		IsImport:    true,
		Source:      srcDir,
	}, nil
}

// isTempLike 识别导入时不应搬运的临时/锁文件。
func isTempLike(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".tmp") || strings.HasPrefix(lower, "~") {
		return true
	}
	if strings.Contains(lower, "-wal") || strings.Contains(lower, "-shm") {
		return true // SQLite 锁态文件：索引库一致性要求拷贝时坚决不碰
	}
	return false
}

// importedTagPrefix 导入兜底版本 tag 前缀（真实 FileVersion 不可得时用）。
const importedTagPrefix = "imported-"

// importVersionTag 读取导入源的版本标签：FileVersion 探测失败（非 Windows
// 平台或资源缺失）时以**源指纹**兜底——绝对路径 + exe 字节数 + 修改时刻做
// SHA-256 截 8 hex（目录命名判重指纹，非安全摘要，截断够用）。
// 旧形态 "imported-<秒级时间戳>" 已废：同一源跨秒重复导入 tag 各不相同，
// 查重永不命中，everything_vimported-* 孤儿目录互积残留（ListInstalled 沿
// 历史口径不列入、Remove 按裸版本也清不掉）。确定性 tag 让"重复导入同一源"
// 天然撞进 ImportLocal 的目录查重分支被拒；源真换了内容（尺寸/时刻变化）
// 则指纹自然更新、不误拦。
func importVersionTag(srcExe string, fi os.FileInfo) string {
	version, vErr := versioninfo.FileVersion(srcExe)
	if vErr == nil && plainVersionRe.MatchString(version) {
		return version
	}
	abs, aErr := filepath.Abs(srcExe)
	if aErr != nil {
		abs = filepath.Clean(srcExe)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d", abs, fi.Size(), fi.ModTime().UnixNano())
	return importedTagPrefix + hex.EncodeToString(h.Sum(nil))[:8]
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

func copyDirTo(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirTo(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFileTo(s, d); err != nil {
			return err
		}
	}
	return nil
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
		slog.Debug("everything 版本目录大小度量降级，回退主程序文件大小", "dir", dir, "partial", st.Partial, "err", st.Err)
		return fallback
	}
	return st.Bytes
}
