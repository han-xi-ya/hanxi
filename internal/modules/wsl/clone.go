package wsl

// 克隆与导入（备份/复制环境的两条正路）：
//   - CloneDistro 快路径 = 停源 → 流式拷贝 ext4.vhdx（wsl:clone 进度事件）→
//     wsl --import --vhd 挂载副本（需 WSL 2.7.3+；版本不足时指路「导出→导入」替代动线）。
//   - ImportDistro = wsl --import 把导出的 tar / 官方 rootfs 落成新增发行版。
// 基线与六操作一致：源名过实时白名单、新名过字符集校验并与本机名单防撞、
// 目标目录复用 moveTarget 把关；克隆占源与目标两把单飞闸；
// 克隆失败保留已拷 VHDX 并点名路径，绝不暗删用户目录里的东西。

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// EventClone 克隆进度事件名（app.go 中注册载荷类型）。
const EventClone = "wsl:clone"

// CloneProgress 克隆进度事件载荷。
type CloneProgress struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Stage   string `json:"stage"` // copying | importing | done | error
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"` // done 时的完整回执文案
}

// newDistroNameRe 新增发行版名的字符集防线：字母/数字起头（天然排除以 '-'
// 打头的选项注入形态），允许常见连字符/点/下划线/空格，≤64 字符。
var newDistroNameRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,63}$`)

// importExtRe 导入源文件白名单：本模块导出产物与主流 rootfs 形态。
var importExtRe = regexp.MustCompile(`(?i)\.(tar|tar\.gz|tgz)$`)

// vhdxMinVersion --import --vhd 形态的最低 WSL 版本（微软 2.7.3 引入）。
var vhdxMinVersion = []int{2, 7, 3}

// validateNewDistroName 新名合法性 + 与本机名单防撞（大小写不敏感）。
func (s *WslService) validateNewDistroName(ctx context.Context, newName string) error {
	if !newDistroNameRe.MatchString(newName) {
		return fmt.Errorf("新发行版名 %q 不合法（字母数字开头，允许 空格 . _ -，≤64 字符）", newName)
	}
	names, err := s.quietNames(ctx)
	if err != nil {
		return fmt.Errorf("无法获取本机发行版清单，操作中止: %w", err)
	}
	for _, n := range names {
		if strings.EqualFold(n, newName) {
			return fmt.Errorf("本机已存在发行版 %q，请换一个名字（Windows 发行版名大小写不敏感）", n)
		}
	}
	return nil
}

// CloneDistro 克隆发行版。校验全同步（失败即时报错），拷贝/导入转后台协程
// 推 wsl:clone 事件——数十 GB 的盘对拷不该吊死一次 RPC。
func (s *WslService) CloneDistro(name, newName, target string) (OperationOutcome, error) {
	name, newName = strings.TrimSpace(name), strings.TrimSpace(newName)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if strings.EqualFold(name, newName) {
		return OperationOutcome{}, fmt.Errorf("源与目标同名，克隆无意义")
	}
	if err := s.distroAllowed(ctx, name); err != nil {
		return OperationOutcome{}, err
	}
	if err := s.validateNewDistroName(ctx, newName); err != nil {
		return OperationOutcome{}, err
	}

	// 快路径前提：源为 WSL2 且本机 WSL ≥ 2.7.3（--import --vhd 能力面）。
	version := ""
	for _, d := range s.wslDistros(ctx) {
		if strings.EqualFold(d.Name, name) {
			version = d.Version
			break
		}
	}
	if version != "2" {
		return OperationOutcome{}, fmt.Errorf("克隆快路径仅支持 WSL2 发行版（当前为 WSL%s）；WSL1 请先迁移为 WSL2", version)
	}
	if v := s.wslVersion(ctx); !versionAtLeast(v, vhdxMinVersion) {
		return OperationOutcome{}, fmt.Errorf("克隆所需的 wsl --import --vhd 能力要求 WSL %s+（当前 %s）；升级前可用「导出 → 导入」替代：先导出 tar，再用导入落成", strings.Join(intsToStr(vhdxMinVersion), "."), orUnknown(v))
	}

	store, err := s.lxss(ctx)
	if err != nil || store == nil {
		return OperationOutcome{}, fmt.Errorf("注册表巡查失败，无法定位源数据盘: %v", err)
	}
	src, found := store[strings.ToLower(name)]
	if !found || src.Vhdx == "" {
		return OperationOutcome{}, fmt.Errorf("未定位到 %s 的 ext4.vhdx（非常规布局？），无法克隆", name)
	}
	destDir, err := moveTarget(target, src.BasePath)
	if err != nil {
		return OperationOutcome{}, err
	}
	// 目标卷空间预检：拷贝写满即失败，不如动手前就说清（与瘦身同尺子）。
	// 拷贝按"逻辑大小"整文件复制，稀疏盘也要按这份口径备料。
	srcSize := fileSize(src.Vhdx)
	free, ferr := diskFree(filepath.VolumeName(destDir) + `\`)
	if ferr != nil {
		return OperationOutcome{}, fmt.Errorf("目标卷剩余空间查询失败: %w", ferr)
	}
	if free < uint64(srcSize)+compactFreeBuffer {
		return OperationOutcome{}, fmt.Errorf("空间预检不过：%s 剩余 %.1f GB，克隆需源盘 %.1f GB + 2GB 缓冲——换个大点的目标盘再来", filepath.VolumeName(destDir)+`\`, float64(free)/(1<<30), float64(srcSize)/(1<<30))
	}

	finishSrc, ok := s.tryBeginDistroOp(name, "clone")
	if !ok {
		return OperationOutcome{}, errDistroBusy
	}
	finishDst, ok := s.tryBeginDistroOp(newName, "clone")
	if !ok {
		finishSrc()
		return OperationOutcome{}, errDistroBusy
	}

	// 后台协程自挂 ctx（不继承上方 60s 校验 ctx——defer cancel 会掐死拷贝）；
	// 取消句柄进 longops：拷贝/挂载段都可停（半成品有各自的清理路径）。
	opCtx, opCancel := context.WithCancel(context.Background())
	s.registerClone(name, opCancel)
	go func() {
		defer opCancel()
		defer finishSrc()
		defer finishDst()
		defer s.finishClone(name)
		s.emit(EventClone, CloneProgress{Source: name, Target: newName, Stage: "copying"})
		if err := s.runClone(opCtx, name, newName, src.Vhdx, destDir); err != nil {
			msg := err.Error()
			if canceled(err) {
				msg = fmt.Sprintf("克隆已按请求取消（半成品已清理，源与既有发行版均未改动）: %v", err)
			}
			s.emit(EventClone, CloneProgress{Source: name, Target: newName, Stage: "error", Error: msg})
			return
		}
		s.emit(EventClone, CloneProgress{Source: name, Target: newName, Stage: "done",
			Message: fmt.Sprintf("%s 已克隆为 %s（源发行版克隆时被终止，需要可再启动）", name, newName)})
	}()
	return OperationOutcome{Success: true, Message: fmt.Sprintf("开始克隆 %s → %s（保存到 %s），进度见行内提示；随时可取消", name, newName, destDir)}, nil
}

// runClone 后台主体：停源 → 拷 VHDX（进度）→ --import --vhd → 复验在册。
// 全程 30 分钟护栏 + 可取消 ctx；拷贝段取消自清半成品（本轮创建的文件，
// 非用户资产），import 段失败/取消则保留已拷盘并点名路径（它已是有价值的数据）。
func (s *WslService) runClone(parent context.Context, name, newName, vhdx, destDir string) error {
	ctx, cancel := context.WithTimeout(parent, opTimeout)
	defer cancel()

	_, _ = s.runWsl(ctx, "--terminate", name) // 尽力收敛运行态：拷贝必须面对静止的 VHDX

	src, err := os.Open(vhdx)
	if err != nil {
		return fmt.Errorf("打开源数据盘失败: %w", err)
	}
	defer src.Close()
	total, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("探测源数据盘大小失败: %w", err)
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("创建克隆目标目录失败: %w", err)
	}
	destPath := filepath.Join(destDir, "ext4.vhdx")
	dest, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("创建克隆数据盘失败: %w", err)
	}
	defer dest.Close()

	buf := make([]byte, 8*1024*1024)
	var written int64
	lastEmit := time.Now()
	copyCanceled := false
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dest.Write(buf[:n]); werr != nil {
				return fmt.Errorf("写克隆盘失败: %w", werr)
			}
			written += int64(n)
			if written >= total || time.Since(lastEmit) >= 300*time.Millisecond {
				s.emit(EventClone, CloneProgress{Source: name, Target: newName, Stage: "copying", Done: written, Total: total})
				lastEmit = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("读源数据盘失败: %w", rerr)
		}
		if ctx.Err() != nil {
			copyCanceled = true
			break
		}
	}
	if copyCanceled {
		_ = dest.Close()
		_ = os.Remove(destPath) // 半成品清理：本轮刚创建的不完整拷贝，不是用户资产
		return fmt.Errorf("拷贝阶段被取消: %w", ctx.Err())
	}
	if err := dest.Close(); err != nil {
		return fmt.Errorf("关闭克隆数据盘失败: %w", err)
	}

	s.setCloneStage(name, "importing")
	s.emit(EventClone, CloneProgress{Source: name, Target: newName, Stage: "importing", Done: written, Total: total})
	if out, err := s.runWsl(ctx, "--import", "--vhd", newName, destPath); err != nil {
		_, _ = s.runWsl(context.Background(), "--unregister", newName) // 半成品新实例尽力回收
		if canceled(ctx.Err()) {
			return fmt.Errorf("导入阶段被取消（已拷贝的数据盘保留在 %s，可自行处理）: %w", destPath, ctx.Err())
		}
		return fmt.Errorf("挂载克隆盘失败（已拷贝的数据盘保留在 %s，可自行处理）: %w %s", destPath, err, strings.TrimSpace(out))
	}
	// 复验在册：退出码 0 但名单里没有它，不算成功。
	if names, err := s.quietNames(ctx); err == nil {
		present := false
		for _, n := range names {
			if strings.EqualFold(n, newName) {
				present = true
				break
			}
		}
		if !present {
			return fmt.Errorf("克隆导入命令返回成功，但名单中未见 %s，请重新复采核实", newName)
		}
	}
	return nil
}

// ImportDistro 导入 tar（本模块导出产物或官方 rootfs）为新增发行版。
// 同步等待（wsl --import 内部完成解包，数十分钟级），成功后复验在册。
func (s *WslService) ImportDistro(name, target, tarPath string) (DistroOpResult, error) {
	name, tarPath = strings.TrimSpace(name), strings.TrimSpace(filepath.Clean(tarPath))
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if err := s.validateNewDistroName(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	if !importExtRe.MatchString(tarPath) {
		return DistroOpResult{}, fmt.Errorf("导入源须为 .tar / .tar.gz / .tgz 文件: %s", tarPath)
	}
	st, err := os.Stat(tarPath)
	if err != nil || st.IsDir() || st.Size() == 0 {
		return DistroOpResult{}, fmt.Errorf("导入源文件不存在或为空: %s", tarPath)
	}
	destDir, err := moveTarget(target, "")
	if err != nil {
		return DistroOpResult{}, err
	}
	finish, ok := s.tryBeginDistroOp(name, "import")
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()

	if out, err := s.runWsl(ctx, "--import", name, destDir, tarPath); err != nil {
		return DistroOpResult{}, fmt.Errorf("导入 %s 失败: %w %s", name, err, strings.TrimSpace(out))
	}
	if names, err := s.quietNames(ctx); err == nil {
		present := false
		for _, n := range names {
			if strings.EqualFold(n, name) {
				present = true
				break
			}
		}
		if !present {
			return DistroOpResult{}, fmt.Errorf("导入命令返回成功，但名单中未见 %s，请重新复采核实", name)
		}
	}
	return DistroOpResult{Success: true, Message: fmt.Sprintf("%s 已导入（数据落在 %s），下次唤终端即可以普通权限进入", name, destDir)}, nil
}

// versionAtLeast 点分数版本 ≥ min；无法解析（空串/非数字段）一律判不满足。
func versionAtLeast(v string, min []int) bool {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) < len(min) {
		return false
	}
	for i, m := range min {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return false
		}
		if n > m {
			return true
		}
		if n < m {
			return false
		}
	}
	return true
}

func intsToStr(ns []int) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = strconv.Itoa(n)
	}
	return out
}

func orUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return "未探测到"
	}
	return v
}
