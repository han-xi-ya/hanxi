// Package version 实现 RAMMap 版本管理：微软 Sysinternals 官方直链下载、
// "下载 → 校验 → 解包 → 落位"（委托 packages/go/artifact 的 UnpackZip/Tree
// 闸门）与本地导入/卸载/解析。
//
// 完整性策略（WindTerm 同款降级三层，无信任根在场）：上游不为单工具发布
// 官方摘要，无信任根——bespoke 直链下载（单源无镜像 + 重试 + 流式上限），
// 随后字节数核对 + artifact.UnpackZip 的 ZipSlip/炸弹/CRC32 全量闸门 +
// 便携布局自检；自算 zip/exe 摘要落账本防同版本内容漂移（Tree.Commit 收口）。
// meta 记 verifiedHash=false 供 UI 如实展示"未经官方哈希校验"。
//
// 版本模型（区别于 GitHub 家族，方案 PLAN_N4_RAMMAP §1.5 定）：上游同址覆盖式
// 最新版、无版本目录、无历史资产——版本令牌取 zip 的 Last-Modified 日期
// （YYYY-MM-DD），落位目录 rammap_<日期>/；日期变即上游发新。
//
// zip 布局（实测 737KB）：平铺无根目录，含 RAMMap.exe（x86）/RAMMap64.exe
// （x64）/RAMMap64a.exe（arm64）/Eula.txt；本模块按 GOARCH 取对应载荷
// （x64→RAMMap64.exe，arm64→RAMMap64a.exe），三架构 exe 同级共存不解套。
// 载荷 manifest 为 requireAdministrator（提权三重契约，见 instance 包）。
package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
	"hanxi/packages/go/artifact"
)

const (
	treeEntryName = "rammap"

	// moduleMetaFileName 模块侧账本（verifiedHash/PE 版本等前端契约字段）。
	moduleMetaFileName = "meta.module.json"
)

// payloadExeName 当前体系结构的 RAMMap 载荷 exe 名（官方 zip 三架构平铺并列，
// 按 runtime.GOARCH 取件：x64→RAMMap64.exe，ARM64→RAMMap64a.exe；x86 载荷
// RAMMap.exe 在 64 位 Windows 上无托管价值，不提供）。
func payloadExeName() string {
	if runtime.GOARCH == "arm64" {
		return "RAMMap64a.exe"
	}
	return "RAMMap64.exe"
}

// Manager RAMMap 版本管理引擎。
type Manager struct {
	versionsDir string
	tree        *artifact.Tree

	dl     func(ctx context.Context, client *http.Client, url, dest string, maxBytes int64, onProgress func(int64)) error
	client *http.Client // bespoke 下载客户端（5min 超时）
}

// NewManager 以 versions 根目录创建引擎；构造无副作用。
func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		tree:        OpenTree(versionsDir),
		dl:          downloadTo,
		client:      &http.Client{Timeout: 5 * time.Minute},
	}
}

// OpenTree 打开 RAMMap 版本树（装配根启动恢复按事务背书清理现场用）。
func OpenTree(versionsDir string) *artifact.Tree {
	return artifact.OpenTree(versionsDir, treeEntryName)
}

// ListRemote 获取"最新版"单条列表（HEAD 探测 Last-Modified）。
func (m *Manager) ListRemote() ([]RammapRelease, error) {
	list, _, err := remoteCache.get()
	return list, err
}

// ListInstalled 扫描本地已装版本（按日期令牌降序；payload exe 缺失/为空跳过）。
func (m *Manager) ListInstalled() ([]RammapVersionInfo, error) {
	vers, err := m.tree.Versions()
	if err != nil {
		return nil, err
	}
	var list []RammapVersionInfo
	for _, v := range vers {
		exe := filepath.Join(v.Dir, payloadExeName())
		fi, serr := os.Stat(exe)
		if serr != nil || fi.Size() == 0 {
			continue
		}
		info := RammapVersionInfo{
			Version: v.Version,
			ExePath: exe,
			Dir:     v.Dir,
			Size:    fi.Size(),
			SHA256:  v.Meta.AssetSHA256,
			Source:  v.Meta.Source,
		}
		if info.SHA256 == "" {
			info.SHA256 = fileSHA256(exe)
		}
		mm := readModuleMeta(v.Dir)
		info.VerifiedHash = mm.VerifiedHash
		info.IsImport = mm.IsImport
		if mm.IsImport {
			info.Source = mm.Source
		}
		info.InstalledAt = formatInstalledAt(v.Meta.InstalledAt, exe)
		list = append(list, info)
	}
	return list, nil
}

// Download 旧调用面（非事务/单测）。
func (m *Manager) Download(txnID, version string, onProgress func(p DownloadProgress)) error {
	return m.DownloadContext(context.Background(), txnID, version, onProgress)
}

// DownloadContext 下载并安装官方 zip，可由事务 ctx 取消（P0 批 2b 生命周期）。
// 取消边界：传输与解包全程 ctx 感知即时中止；落位为 Tree.Commit 原子 rename。
// 进度词表 downloading/verify/extract/done/error。
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

	releases, size, err := remoteCache.get()
	if err != nil {
		emit("error", 0, 0, fmt.Sprintf("探测官方版本失败: %v", err))
		return err
	}
	if len(releases) == 0 {
		err := fmt.Errorf("官方源无可用版本")
		emit("error", 0, 0, err.Error())
		return err
	}
	// 上游单条"最新版"：请求版本非当前日期即拒（无历史资产可下载，如实报错
	// 而非静默装最新版——防用户点旧版结果拿到新版）。
	rel := releases[0]
	if version != "" && version != rel.Version {
		err := fmt.Errorf("上游仅提供最新版（%s），无 %s 的历史资产可下载", rel.Version, version)
		emit("error", 0, 0, err.Error())
		return err
	}

	tmp, err := os.CreateTemp("", "hanxi-rammap-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	tmp.Close()

	url := zipURL
	declaredSize := size
	emit("downloading", 0, declaredSize, "")
	if derr := m.dl(ctx, m.client, url, tmpPath, declaredSize, func(done int64) {
		emit("downloading", done, declaredSize, "")
	}); derr != nil {
		emit("error", 0, declaredSize, fmt.Sprintf("下载失败: %v", derr))
		return derr
	}

	// 层 2：字节数核对
	actual, err := fileSize(tmpPath)
	if err != nil {
		emit("error", 0, declaredSize, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	}
	if declaredSize > 0 && actual != declaredSize {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", declaredSize, actual)
		emit("error", 0, declaredSize, err.Error())
		return err
	}

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
	defer discard()

	emit("extract", 0, 0, "")
	if err := artifact.UnpackZipContext(ctx, tmpPath, staging, artifact.DefaultLimits, nil); err != nil {
		emit("error", 0, 0, fmt.Sprintf("解压失败: %v", err))
		return err
	}
	// 层 4：布局自检——本架构 payload exe 在场非空（三架构 exe 平铺）。
	exe := filepath.Join(staging, payloadExeName())
	if fi, serr := os.Stat(exe); serr != nil || fi.Size() == 0 {
		err := fmt.Errorf("zip 布局无效：缺少可用的 %s", payloadExeName())
		emit("error", 0, 0, err.Error())
		return err
	}

	if err := writeModuleMeta(staging, moduleMeta{
		Source: rel.AssetName, VerifiedHash: false, ZipSize: actual, ComputedSHA: computedZipSHA,
	}); err != nil {
		emit("error", 0, 0, fmt.Sprintf("写入来源账目失败: %v", err))
		return err
	}
	meta := artifact.Meta{
		Entry:       payloadExeName(),
		ZipSHA256:   "", // 上游无官方摘要，如实留空（computedSHA 见模块账本）
		AssetSHA256: fileSHA256(exe),
		Source:      artifact.SourceRemote,
	}
	if err := m.tree.Commit(staging, rel.Version, meta); err != nil {
		emit("error", 0, 0, fmt.Sprintf("落位失败: %v", err))
		return err
	}
	emit("done", 100, 100, "")
	return nil
}

// ImportLocal 收纳本机已有 RAMMap 目录（三架构 exe 平铺，按当前体系结构取
// 载荷；Eula.txt 一并保留）。版本令牌优先取 PE FileVersion（如 1.63），取不到
// 回落 imported-时间戳。
func (m *Manager) ImportLocal(srcDir string) (RammapVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, payloadExeName())
	fi, err := os.Stat(srcExe)
	if err != nil || fi.Size() == 0 {
		return RammapVersionInfo{}, fmt.Errorf("导入失败：%s 内未找到可用的 %s", srcDir, payloadExeName())
	}
	token := importToken(srcExe)
	if err := artifact.ValidateVersionToken(token); err != nil {
		return RammapVersionInfo{}, err
	}
	stagingID := "import-" + token + "-" + time.Now().Format("150405")
	staging, discard, err := m.tree.StageDir(stagingID)
	if err != nil {
		return RammapVersionInfo{}, err
	}
	defer discard()
	if err := copyDirFlat(srcDir, staging); err != nil {
		return RammapVersionInfo{}, fmt.Errorf("迁移 RAMMap 目录失败: %w", err)
	}
	dstExe := filepath.Join(staging, payloadExeName())
	if dstFi, derr := os.Stat(dstExe); derr != nil || dstFi.Size() == 0 {
		return RammapVersionInfo{}, fmt.Errorf("导入收口异常：中转目录内找不到 %s", payloadExeName())
	}
	if err := writeModuleMeta(staging, moduleMeta{Source: srcDir, IsImport: true}); err != nil {
		return RammapVersionInfo{}, fmt.Errorf("写入导入账目失败: %v", err)
	}
	meta := artifact.Meta{Entry: payloadExeName(), AssetSHA256: fileSHA256(dstExe), Source: artifact.SourceImported}
	if err := m.tree.Commit(staging, token, meta); err != nil {
		return RammapVersionInfo{}, err
	}
	return RammapVersionInfo{
		Version: token, ExePath: filepath.Join(m.versionsDir, treeEntryName+"_"+token, payloadExeName()),
		Dir: filepath.Join(m.versionsDir, treeEntryName+"_"+token), Size: fi.Size(),
		SHA256: meta.AssetSHA256, InstalledAt: time.Now().Format("2006-01-02 15:04:05"),
		IsImport: true, Source: srcDir,
	}, nil
}

// Remove 卸载指定版本（委托 Tree：rename 隔离后删除）。
func (m *Manager) Remove(version string) error {
	_, token, err := m.resolveVersionDir(version)
	if err != nil {
		return err
	}
	return m.tree.Remove(token, nil)
}

// ResolveExe 返回指定版本的本架构载荷绝对路径（存在性校验）。
func (m *Manager) ResolveExe(version string) (string, error) {
	dir, _, err := m.resolveVersionDir(version)
	if err != nil {
		return "", err
	}
	exe := filepath.Join(dir, payloadExeName())
	if fi, serr := os.Stat(exe); serr != nil || fi.Size() == 0 {
		return "", fmt.Errorf("版本 %s 安装损坏：未找到 %s", version, payloadExeName())
	}
	return exe, nil
}

// PEVersion 返回载荷 exe 的 PE 文件版本（如 1.63，供前端展示；读不到返回空串）。
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

func (m *Manager) resolveVersionDir(version string) (dir, token string, err error) {
	ver := strings.TrimSpace(version)
	if d, rerr := m.tree.Resolve(ver); rerr == nil {
		return d, ver, nil
	}
	return "", "", fmt.Errorf("版本 %s 未安装，请先下载", ver)
}

// ---------- 领域工具 ----------

// importToken 导入版本令牌：PE FileVersion（形如 1.63）优先，取不到按
// imported-时间戳兜底（不与日期令牌冲突）。
func importToken(exe string) string {
	if fv, err := versioninfo.FileVersion(exe); err == nil {
		ver := strings.TrimRight(strings.TrimSpace(fv), ".")
		if ver != "" && looksLikeVersion(ver) {
			return "v" + ver
		}
	}
	return "imported-" + time.Now().Format("20060102150405")
}

// looksLikeVersion 宽松版本形（1~4 段点分数字，允许 RAMMap 的两段 "1.63"）。
func looksLikeVersion(s string) bool {
	if s == "" {
		return false
	}
	segs := strings.Split(s, ".")
	if len(segs) > 4 {
		return false
	}
	for _, p := range segs {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func formatInstalledAt(fromMeta time.Time, exe string) string {
	if !fromMeta.IsZero() {
		return fromMeta.Local().Format("2006-01-02 15:04:05")
	}
	if fi, err := os.Stat(exe); err == nil {
		return fi.ModTime().Format("2006-01-02 15:04:05")
	}
	return ""
}

// ---------- 账本 ----------

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

// copyDirFlat 顶层平铺复制（跳过历史账本文件，payload 与 Eula 全收）。
func copyDirFlat(src, dst string) error {
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue
		}
		if e.Name() == "meta.json" || e.Name() == moduleMetaFileName {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
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
