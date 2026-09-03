package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hanxi/internal/platform/versioninfo"
)

const (
	exeName        = "PaperTodo.exe"
	installDirName = "papertodo" // 固定托管目录名（单版本覆盖布局，见下方设计说明）
	metaFileName   = "hanxi-meta.json"
	dataFileName   = "data.json"     // 上游便签数据（与 exe 同目录，卸载必须保留）
	tmpSuffix      = ".download-tmp" // 下载中转文件（托管目录内，保证与目标同卷可原子 rename）
	bakSuffix      = ".bak"          // 覆盖升级时的旧 exe 备份（安装成功后清除，失败回滚）
)

// 单版本覆盖目录设计（与 ccswitch/markeron 的 _X.Y.Z 多版本隔离不同，系上游形态所致）：
// PaperTodo 是绿色单文件程序，便签数据（data.json、note-assets.lmdb、plugins/）
// 恒在 exe 同目录——多版本目录意味着每次切换版本都要迁移用户创作内容，
// 风险和数据分裂远大于"回滚需重新下载"的收益。故收敛为固定 versions/papertodo
// 单目录：升级 = 备份旧 exe → 原子改名替换，数据原地不动；
// "切换版本"语义 = 在线覆盖安装目标版本（含回滚重装）。
//
// 完整性校验链（上游实证无官方 digest、未收录 winget、body 无哈希，
// ccswitch 的 digest 硬过滤不可照搬，按 markeron 精神降级为尽力而为的多层兜底）：
//  1. 下载 URL 恒由 repoOwner/repoName + 精确资产名拼接，配合 4 镜像回退；
//  2. 落盘字节数 == release API 声明 size（防截断/代理投毒换包）；
//  3. MZ 魔数（错误页/文本投毒最廉价闸）；
//  4. PE FileVersion 与目标版本核对（可读取时不一致即拒绝；资源缺失则降级放行并如实记录）；
//  5. 若上游未来补充 GitHub 官方 digest（解析层已兼容），自动升级为 sha256 硬校验；
//  6. 无论上游有无 digest，落盘后计算 sha256 写入 hanxi-meta.json 作为下载指纹，
//     供审计与盘上损坏感知（本链的信任根仍是 github.com TLS 与镜像运营方，如实承认）。
type Manager struct {
	versionsDir string
	client      *http.Client // 下载客户端（71MB 自包含 exe，长超时）
}

func NewManager(versionsDir string) *Manager {
	return &Manager{
		versionsDir: versionsDir,
		client:      &http.Client{Timeout: 15 * time.Minute},
	}
}

// InstallDir 托管安装目录（versions/papertodo）。
func (m *Manager) InstallDir() string {
	return filepath.Join(m.versionsDir, installDirName)
}

// ListRemote 获取远程可用版本（10 分钟内命中缓存）。
func (m *Manager) ListRemote() ([]PaperRelease, error) {
	return remoteCache.get()
}

// ListInstalled 扫描托管安装目录（至多一条）。exe 缺失/为空视为损坏安装返回空。
func (m *Manager) ListInstalled() ([]PaperVersionInfo, error) {
	dir := m.InstallDir()
	exe := filepath.Join(dir, exeName)
	fi, err := os.Stat(exe)
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return nil, nil
	}

	info := PaperVersionInfo{
		Version: resolveInstalledVersion(dir, exe),
		ExePath: exe,
		Dir:     dir,
		Size:    fi.Size(),
	}
	if data, err := os.Stat(filepath.Join(dir, dataFileName)); err == nil && !data.IsDir() {
		info.HasData = true
	}
	if meta, err := os.ReadFile(filepath.Join(dir, metaFileName)); err == nil {
		var mm map[string]any
		if json.Unmarshal(meta, &mm) == nil {
			info.InstalledAt, _ = mm["installedAt"].(string)
			info.Variant, _ = mm["variant"].(string)
			info.Source, _ = mm["source"].(string)
			info.AssetSHA256, _ = mm["assetSHA256"].(string)
			info.OfficialSHA, _ = mm["officialSHA256"].(string)
			info.PEVersion, _ = mm["peVersion"].(string)
			info.IsImport, _ = mm["isImport"].(bool)
		}
	}
	if info.InstalledAt == "" {
		info.InstalledAt = fi.ModTime().Format("2006-01-02 15:04:05")
	}
	return []PaperVersionInfo{info}, nil
}

// resolveInstalledVersion 权威版本解析优先级：hanxi-meta 记录的远程 tag →
// PE FileVersion → unknown 日期兜底（recordly 同款三级策略；导入安装无 tag）。
func resolveInstalledVersion(dir, exe string) string {
	if meta, err := os.ReadFile(filepath.Join(dir, metaFileName)); err == nil {
		var mm map[string]any
		if json.Unmarshal(meta, &mm) == nil {
			if tag, ok := mm["tag"].(string); ok && tag != "" {
				return tag
			}
		}
	}
	if v, err := versioninfo.FileVersion(exe); err == nil && v != "" {
		return "v" + v
	}
	return "vunknown-" + time.Now().Format("20060102")
}

// assetFor 返回指定变体的资产；变体非法或资产缺失报错（列表保证双变体齐备）。
func (r *PaperRelease) assetFor(variant string) (PaperAsset, error) {
	switch variant {
	case VariantSelfContained:
		return r.SelfContained, nil
	case VariantNoRuntime:
		return r.NoRuntime, nil
	default:
		return PaperAsset{}, fmt.Errorf("未知运行库变体: %q", variant)
	}
}

// Download 下载指定版本与变体的绿色 exe，校验后原子覆盖进托管目录。
// 便签数据与 plugins/ 一概不触碰；运行中的实例由 service 层先行拦截
// （Windows 独占运行中的 exe，改名必失败，友好报错在 service 层给出）。
//
// onProgress 可选：实时上报各阶段进度。
func (m *Manager) Download(targetVersion, variant string, onProgress func(p DownloadProgress)) error {
	emit := func(stage string, done, total int64, msg string) {
		if onProgress != nil {
			onProgress(DownloadProgress{Version: targetVersion, Stage: stage, Done: done, Total: total, Message: msg})
		}
	}

	// 1. 解析远程资产（tag 归一；冷缓存先拉一轮列表）
	tag := "v" + strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if !ValidVariant(variant) {
		err := fmt.Errorf("未知运行库变体: %q", variant)
		emit("error", 0, 0, err.Error())
		return err
	}
	rel, ok := remoteCache.findRelease(tag)
	if !ok {
		if _, err := remoteCache.get(); err != nil {
			emit("error", 0, 0, fmt.Sprintf("获取远程版本列表失败: %v", err))
			return err
		}
		rel, ok = remoteCache.findRelease(tag)
	}
	if !ok {
		err := fmt.Errorf("远程列表不存在版本 %s（或该版本资产不全）", tag)
		emit("error", 0, 0, err.Error())
		return err
	}
	asset, err := rel.assetFor(variant)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}

	dir := m.InstallDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// 中转文件放托管目录内：与目标同卷，改名才是原子替换（跨卷 rename 会失败）
	tmpPath := filepath.Join(dir, exeName+tmpSuffix)
	defer os.Remove(tmpPath)

	// 2. 下载 exe（直连 + 镜像逐个回退）
	emit("downloading", 0, asset.Size, "")
	if err := downloadTo(m.client, assetMirrors(tag, asset.Name), tmpPath, func(done int64) {
		emit("downloading", done, asset.Size, "")
	}); err != nil {
		emit("error", 0, asset.Size, fmt.Sprintf("下载失败: %v", err))
		return err
	}

	// 3. 完整性校验链（见包注释），全部通过才允许换入
	emit("verify", 0, 0, "")
	if actual, err := fileSize(tmpPath); err != nil {
		emit("error", 0, asset.Size, fmt.Sprintf("读取临时文件失败: %v", err))
		return err
	} else if actual != asset.Size {
		err := fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", asset.Size, actual)
		emit("error", 0, asset.Size, err.Error())
		return err
	}
	if err := checkMZMagic(tmpPath); err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	localSHA := fileSHA256(tmpPath)
	if localSHA == "" {
		err := fmt.Errorf("无法读取下载文件")
		emit("error", 0, 0, err.Error())
		return err
	}
	if asset.SHA256 != "" {
		// 上游补充官方 digest 后自动启用硬校验（第一主依据）
		if err := verifySHA256(tmpPath, asset.SHA256); err != nil {
			emit("error", 0, asset.Size, err.Error())
			return fmt.Errorf("官方哈希校验失败（下载文件疑似被篡改或损坏）: %w", err)
		}
	}
	peVersion, peChecked, err := peVersionOf(tmpPath)
	if err != nil {
		emit("error", 0, 0, err.Error())
		return err
	}
	if peChecked && !peVersionMatches(peVersion, tag) {
		err := fmt.Errorf("PE 版本资源（%s）与目标版本 %s 不符，疑似拿错包", peVersion, tag)
		emit("error", 0, 0, err.Error())
		return err
	}

	// 4. 原子换入：旧 exe 备份 → 改名替换 → 失败回滚
	if err := swapInExe(dir, tmpPath); err != nil {
		emit("error", 0, 0, fmt.Sprintf("安装失败: %v", err))
		return err
	}

	// 5. 落盘元信息（tag 为权威版本；指纹与 PE 结果如实记录）
	meta := map[string]any{
		"installedAt":  time.Now().Format("2006-01-02 15:04:05"),
		"tag":          tag,
		"variant":      variant,
		"source":       asset.Name,
		"assetSize":    asset.Size,
		"assetSHA256":  localSHA,
		"peVersion":    peVersion,
		"peChecked":    peChecked,
		"verifiedHash": asset.SHA256 != "",
	}
	if asset.SHA256 != "" {
		meta["officialSHA256"] = asset.SHA256
	}
	_ = writeJSON(filepath.Join(dir, metaFileName), meta)

	emit("done", 100, 100, "")
	return nil
}

// Remove 卸载托管安装：只删 exe / 备份 / 中转残留 / hanxi-meta，
// **刻意保留 data.json、note-assets.lmdb、plugins/ 等用户创作内容**——
// 便签不是缓存而是数据本体，误删即事故；重装/导入本地后原地复活。
// 目录清空时才随手移除目录。
func (m *Manager) Remove() error {
	dir := m.InstallDir()
	exe := filepath.Join(dir, exeName)
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("尚无托管安装，无需卸载")
	}
	for _, victim := range []string{exe, exe + bakSuffix, exe + tmpSuffix, filepath.Join(dir, metaFileName)} {
		if err := os.Remove(victim); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_ = os.Remove(dir) // 目录非空（用户数据还在）时静默失败，属预期
	return nil
}

// ResolveExe 返回托管 PaperTodo.exe 路径（未安装报错，供启动/唤窗信使使用）。
func (m *Manager) ResolveExe() (string, error) {
	installed, err := m.ListInstalled()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("尚未安装 PaperTodo，请先在版本管理在线下载或用「导入本地」收编已有副本")
	}
	return installed[0].ExePath, nil
}

// ImportLocal 导入本地已有的 PaperTodo 目录（收编用户自己下载的绿色版及其便签数据）。
// 与 ccswitch"只搬 exe"不同：PaperTodo 数据恒在 exe 同目录，导入必须整套迁移，
// 否则用户便签被留在原地成为孤儿。源目录须含 PaperTodo.exe；
// 托管目录已有安装时拒绝（防覆盖现有数据）；拒绝源目录包含 Hanxi 数据目录的自嵌套。
// 调用方需先确保源实例未运行（Windows 下运行中的 exe 被独占，拷贝必然失败）。
func (m *Manager) ImportLocal(srcDir string) (PaperVersionInfo, error) {
	srcDir = filepath.Clean(strings.TrimSpace(srcDir))
	srcExe := filepath.Join(srcDir, exeName)
	fi, err := os.Stat(srcExe)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return PaperVersionInfo{}, fmt.Errorf("源目录未找到可用的 %s: %s", exeName, srcDir)
	}

	target := m.InstallDir()
	if _, err := os.Stat(filepath.Join(target, exeName)); err == nil {
		return PaperVersionInfo{}, fmt.Errorf("托管目录已有 PaperTodo（%s），请先卸载再导入",
			resolveInstalledVersion(target, filepath.Join(target, exeName)))
	}
	if same := strings.EqualFold(filepath.Clean(srcDir), filepath.Clean(target)); same {
		return PaperVersionInfo{}, fmt.Errorf("源目录就是托管目录本身，无需导入")
	}
	if isUnderDir(target, srcDir) {
		return PaperVersionInfo{}, fmt.Errorf("源目录包含 Hanxi 数据目录，拒绝自嵌套导入")
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return PaperVersionInfo{}, err
	}
	if err := copyTree(srcDir, target); err != nil {
		_ = os.RemoveAll(target)
		return PaperVersionInfo{}, err
	}
	tfi, err := os.Stat(filepath.Join(target, exeName))
	if err != nil || tfi.Size() == 0 {
		_ = os.RemoveAll(target)
		return PaperVersionInfo{}, fmt.Errorf("导入后布局无效：缺少可用的 %s", exeName)
	}

	version := "vunknown-" + time.Now().Format("20060102")
	if v, verr := versioninfo.FileVersion(srcExe); verr == nil && v != "" {
		version = "v" + v
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	_ = writeJSON(filepath.Join(target, metaFileName), map[string]any{
		"installedAt": now,
		"tag":         version,
		"isImport":    true,
		"source":      srcDir,
	})

	info := PaperVersionInfo{
		Version:     version,
		ExePath:     filepath.Join(target, exeName),
		Dir:         target,
		Size:        tfi.Size(),
		InstalledAt: now,
		IsImport:    true,
		Source:      srcDir,
	}
	if data, derr := os.Stat(filepath.Join(target, dataFileName)); derr == nil && !data.IsDir() {
		info.HasData = true
	}
	return info, nil
}

// swapInExe 原子换入：tmp → exe，旧 exe 先备份为 .bak；替换失败回滚备份。
// tmp 与目标同目录（同卷），os.Rename 为原子操作（store.go tmp+rename 同款语义）。
func swapInExe(dir, tmpPath string) error {
	exe := filepath.Join(dir, exeName)
	bak := exe + bakSuffix
	_ = os.Remove(bak) // 清理上一轮残留备份（成功安装的末尾也会删，这里防御中断残留）

	hadOld := false
	if _, err := os.Stat(exe); err == nil {
		if err := os.Rename(exe, bak); err != nil {
			return fmt.Errorf("备份旧版本失败: %w", err)
		}
		hadOld = true
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		if hadOld {
			_ = os.Rename(bak, exe) // 尽力回滚：旧 exe 归位优先于报错信息
		}
		return fmt.Errorf("替换新版本失败: %w", err)
	}
	_ = os.Remove(bak)
	return nil
}

// peVersionOf 读取 PE FileVersion 资源。返回 (版本, 是否成功核对, 硬错误)；
// 资源不存在/解析失败（含非 Windows 平台单测）返回 checked=false，由调用方降级放行并如实记录。
func peVersionOf(path string) (string, bool, error) {
	v, err := versioninfo.FileVersion(path)
	if err != nil || v == "" {
		return "", false, nil
	}
	return v, true, nil
}

// peVersionMatches PE 版本号与 tag 数值核心等值比较（缺失段按 0 补齐：
// "3.31.0.0" ≡ "v3.31"）。任何非数字段判不等（fail-closed）。
func peVersionMatches(pe, tag string) bool {
	a := strings.Split(strings.TrimPrefix(strings.TrimSpace(pe), "v"), ".")
	b := strings.Split(strings.TrimPrefix(strings.TrimSpace(tag), "v"), ".")
	n := max(len(a), len(b))
	for i := range n {
		x, y := 0, 0
		if i < len(a) {
			v, err := strconv.Atoi(a[i])
			if err != nil {
				return false
			}
			x = v
		}
		if i < len(b) {
			v, err := strconv.Atoi(b[i])
			if err != nil {
				return false
			}
			y = v
		}
		if x != y {
			return false
		}
	}
	return true
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

// copyTree 递归拷贝目录树（便签目录含数据与图片库，跳过符号链接防环）。
// 备份/中转残留文件一概不搬运。
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), bakSuffix) || strings.HasSuffix(e.Name(), tmpSuffix) {
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
			// 便签目录理论无符号链接；保守跳过防环路与越权写
			continue
		case e.IsDir():
			if err := copyTree(s, d); err != nil {
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
