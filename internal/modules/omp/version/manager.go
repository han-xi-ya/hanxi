package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hanxi/packages/go/artifact"
)

const (
	// dirPrefix 版本隔离目录前缀（Tree entryName，与 ccswitch_/rustdesk_ 同构）
	dirPrefix = "omp"
	// exeName 版本目录内资产文件名：保留官方原名，ResolveExe 与布局自检共用
	exeName = "omp-windows-x64.exe"
	// fetchBudget 单次下载的总超时预算：omp 单文件 ~244MB，10 分钟为下限口径，
	// 慢源靠镜像回退而非拉预算（与 ccswitch 同规）。
	fetchBudget = 10 * time.Minute
	// importProbeTimeout 导入嗅探允许执行 `--version` 的硬超时（只打印版本即退，
	// 无副作用；超时/失败走哈希兜底，绝不阻塞导入主路径）。
	importProbeTimeout = 8 * time.Second
)

// plainVersionRe 纯版本号（如 18.16.0），用于目录令牌校验。
var plainVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// importedDirRe 版本探测失败时的导入兜底目录令牌（现行形态 imported-<sha12>）。
var importedDirRe = regexp.MustCompile(`^imported-[0-9a-f]{12}$`)

// ompVersionRe 匹配 `omp --version` 输出中的版本号（样本：omp/18.16.0）。
// 与 envcheck/detect/omp.go 同一实测形态锚定——两处各自为政是刻意的：
// envcheck 只做只读探测，本包要做令牌决策，词形变更时两侧都会红，互为提醒。
var ompVersionRe = regexp.MustCompile(`(?m)^omp/v?(\d+\.\d+\.\d+)`)

// Manager omp 版本管理引擎：远程列表、单文件 exe 下载与校验、隔离目录存档、
// 本地导入与激活切换（激活状态持久化在模块层 store，本包无状态）。
// 下载/校验/落位事务全部委托 packages/go/artifact 内核，与 ccswitch 同谱；
// 内核 Fetch 自带 HTTP 栈与回环直连纪律，本包不再持有下载客户端。
type Manager struct {
	tree    *artifact.Tree
	fetch   func(ctx context.Context, src artifact.Source, dest string, prog func(artifact.Progress), timeout time.Duration) error
	mirrors func(version, assetName string) []string
}

// NewManager 以指定 versions 根目录创建版本管理引擎；构造无副作用
// （Tree 打开不触盘，staging/账本操作全部延迟到 Download/Import/Remove）。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		tree:    OpenTree(versionsDir),
		fetch:   artifact.Fetch,
		mirrors: assetMirrors,
	}
}

// OpenTree 打开 omp 版本树（装配根启动恢复按事务背书清理现场时用；
// 目录前缀等领域知识只在本包定义，调用方不重复拼写）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, dirPrefix)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）
func (m *Manager) ListRemote() ([]OmpRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描版本树中已落位的 omp 目录（令牌形状外的一律不列，
// 排序与"最新可用"由 Tree.Versions 单点负责）。
func (m *Manager) ListInstalled() ([]OmpVersionInfo, error) {
	versions, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}
	list := make([]OmpVersionInfo, 0, len(versions))
	for _, v := range versions {
		token := v.Version
		if !plainVersionRe.MatchString(token) && !importedDirRe.MatchString(token) {
			continue // 非本托管族令牌（外来/历史形态目录不干涉也不展示）
		}
		exe := filepath.Join(v.Dir, exeName)
		fi, err := os.Stat(exe)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
			continue // 无可用 exe（损坏/半途中断）跳过，与 douzy ListInstalled 同口径
		}
		info := OmpVersionInfo{
			Version: "v" + token,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    fi.Size(),
			SHA256:  v.Meta.AssetSHA256,
			Source:  v.Meta.Source,
		}
		if !v.Meta.InstalledAt.IsZero() {
			info.InstalledAt = v.Meta.InstalledAt.Format("2006-01-02 15:04:05")
		} else {
			info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		list = append(list, info)
	}
	return list, nil
}

// DownloadContext 下载指定版本并事务落位：内核 Fetch 完成多源回退、字节上限、
// 流式与落盘 SHA-256 双核；本方法补单文件形态的 MZ 魔数断言与账本落位。
// txnID 为调用方事务 ID（journal 背书清理现场用），与 service 生命周期共呼吸。
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
	releases, err := m.ListRemote()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
		return err
	}
	var rel *OmpRelease
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
	// 官方摘要信任根：GitHub release API 的 asset.digest。缺失一律拒装——
	// 尽管 omp 上游实测从未缺位（models.go 侦查记录），纪律不因上游优秀而放松。
	if rel.SHA256 == "" {
		err := fmt.Errorf("上游未提供 omp %s 的官方 SHA-256 摘要，拒绝无校验安装", version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmpFile, err := os.CreateTemp("", "hanxi-omp-*.exe")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	tmpFile.Close()

	// 2. 受控下载（主址 + 镜像逐个回退；下载/校验/字节数全部委托内核）
	urls := m.mirrors(rel.Tag, rel.AssetName)
	src := artifact.Source{
		URL:      urls[0],
		Mirrors:  urls[1:],
		SHA256:   rel.SHA256,
		MaxBytes: rel.Size, // 与 release API 声明大小对齐：超限即断，杜绝异常放大
		FileName: rel.AssetName,
	}
	emit("downloading", 0, rel.Size, "")
	fetchErr := m.fetch(ctx, src, tmpPath, func(p artifact.Progress) {
		if p.Stage == artifact.StageDownload {
			emit("downloading", p.Done, p.Total, "")
		}
	}, fetchBudget)
	if fetchErr != nil {
		emit("error", 0, rel.Size, fmt.Sprintf("下载失败: %v", fetchErr))
		return fetchErr
	}
	emit("verify", 0, 0, "内核 SHA-256 双核通过")

	// 3. 单文件形态的最低落地断言：MZ 魔数（防镜像错误页伪装 exe 的最后一道）
	if err := verifyPEMagic(tmpPath); err != nil {
		emit("error", 0, rel.Size, err.Error())
		return err
	}

	// 4. staging 独占中转 → 原子落位 + 账本（与 ccswitch 事务族同一内核通道；
	// omp 无 zip 解包段，staging 内直接摆放官方原名资产）
	if err := ctx.Err(); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	token := strings.TrimPrefix(version, "v")
	if !plainVersionRe.MatchString(token) {
		err := fmt.Errorf("非法版本号: %q", version)
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
	if err := copyFileTo(tmpPath, filepath.Join(staging, exeName)); err != nil {
		emit("error", 0, 0, fmt.Sprintf("落位失败: %v", err))
		return err
	}
	assetSHA, _ := fileSHA256(filepath.Join(staging, exeName))
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   rel.SHA256, // 单文件形态：资产摘要即包摘要（幂等/防漂移键）
		AssetSHA256: assetSHA,
		Source:      artifact.SourceRemote,
		InstalledAt: time.Now(),
	}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("落位失败: %v", err))
		return err
	}

	emit("done", 100, 100, "")
	return nil
}

// ImportLocal 导入本地已有 omp.exe（如官方脚本装的 %LOCALAPPDATA%\omp\omp.exe）：
// 复制进 staging 事务落位，版本令牌三级嗅探——
//  1. 内容哈希命中远程列表官方摘要 → 采用官方版本号（最可信）；
//  2. 就地执行 `--version` 嗅探（8s 硬超时，只打印即退）；
//  3. 兜底 imported-<sha12>（与 envcheck 历史记录口径一致，诚实标注不可考）。
//
// 只接受常规文件 + MZ 头断言；导入账本标 Source=imported，
// ZipSHA256=自算哈希（同哈希重复导入幂等，内核 Commit 保障）。
func (m *Manager) ImportLocal(ctx context.Context, txnID, srcPath string) (OmpVersionInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fi, err := os.Stat(srcPath)
	if err != nil {
		return OmpVersionInfo{}, fmt.Errorf("源文件不可读: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() == 0 {
		return OmpVersionInfo{}, fmt.Errorf("源必须是常规非空文件: %s", srcPath)
	}
	if err := verifyPEMagic(srcPath); err != nil {
		return OmpVersionInfo{}, err
	}
	srcSHA, err := fileSHA256(srcPath)
	if err != nil {
		return OmpVersionInfo{}, err
	}

	token := m.sniffToken(ctx, srcPath, srcSHA)
	staging, discard, err := m.tree.StageDir(txnID)
	if err != nil {
		return OmpVersionInfo{}, err
	}
	defer discard()
	if err := copyFileTo(srcPath, filepath.Join(staging, exeName)); err != nil {
		return OmpVersionInfo{}, err
	}
	meta := artifact.Meta{
		Entry:       exeName,
		ZipSHA256:   srcSHA,
		AssetSHA256: srcSHA,
		Source:      artifact.SourceImported,
		InstalledAt: time.Now(),
	}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		return OmpVersionInfo{}, err
	}
	return OmpVersionInfo{
		Version:     "v" + token,
		ExePath:     filepath.Join(m.treeRoot(), dirPrefix+"_"+token, exeName),
		Dir:         filepath.Join(m.treeRoot(), dirPrefix+"_"+token),
		Size:        fi.Size(),
		InstalledAt: meta.InstalledAt.Format("2006-01-02 15:04:05"),
		SHA256:      srcSHA,
		Source:      artifact.SourceImported,
	}, nil
}

// sniffToken 版本令牌三级嗅探（见 ImportLocal 注释）。
func (m *Manager) sniffToken(ctx context.Context, srcPath, sha string) string {
	if releases, err := m.ListRemote(); err == nil {
		for _, r := range releases {
			if strings.EqualFold(r.SHA256, sha) {
				return strings.TrimPrefix(r.Version, "v")
			}
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, importProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, srcPath, "--version").Output()
	if err == nil {
		if mm := ompVersionRe.FindSubmatch(out); mm != nil {
			return string(mm[1])
		}
	}
	return "imported-" + sha[:12]
}

// treeRoot 返回版本树根目录（ImportLocal 结果定位用）。
func (m *Manager) treeRoot() string { return m.tree.Root }

// ResolveExe 返回指定版本 exe 的完整路径（激活切换/状态展示共用）。
// 未安装/损坏返回错误；version 为空时回退"最新可用"（Tree.Resolve 内核语义）。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, err := m.tree.Resolve(version)
	if err != nil {
		return "", err
	}
	exe := filepath.Join(dir, exeName)
	fi, err := os.Stat(exe)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return "", fmt.Errorf("版本目录缺少可用 exe: %s", exe)
	}
	return exe, nil
}

// Remove 卸载指定版本（委托 Tree：rename 隔离后删除，文件占用时留下可恢复状态）
func (m *Manager) Remove(version string) error {
	token := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !plainVersionRe.MatchString(token) && !importedDirRe.MatchString(token) {
		return fmt.Errorf("非法版本号: %q", version)
	}
	return m.tree.Remove(token, nil)
}

// ---------- 文件工具（单文件形态最小集，内核 Fetch 不覆盖的本地断言） ----------

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

// verifyPEMagic 校验文件头 MZ 魔数：单文件 exe 无 zip 布局可自检，
// PE 魔数是"落地文件确实是可执行体而非 HTML 错误页/截断残片"的最低断言
// （rustdesk/subnetdesk 同规）。
func verifyPEMagic(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil {
		return fmt.Errorf("读取文件头失败: %w", err)
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return fmt.Errorf("文件不是有效的 Windows 可执行体（MZ 头缺失，疑似镜像返回了错误页）")
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
