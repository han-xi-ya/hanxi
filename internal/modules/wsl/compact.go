// VHDX 磁盘瘦身（压缩）——三级工作流，照参考工具的"先保命再动刀"骨架自研：
//
//	备份 → fstrim → Tier1 Optimize-VHD（Hyper-V 模块在位才尝试）→
//	省量不足/失败 → Tier2 注销+重导入（数据经备份 tar 过一遍）
//
// 安全红线：
//   - 强制先全量 tar 备份（不压缩求导入兼容），备份失败绝不进入销毁步骤；
//   - Tier1 的 VHDX 路径由后端从注册表推导，前端只给发行版名（提权注入红线）；
//   - Tier2 重导入失败时备份 tar 一律保留并在错误里点名路径——那是用户唯一的生命线；
//   - 全程事件推送 wsl:compact，重操作全局互斥闸（tryBeginHeavyOp）。
package wsl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// EventCompact 磁盘压缩进度事件名（app.go 中注册载荷类型）。
const EventCompact = "wsl:compact"

// CompactProgress 压缩进度事件载荷。Stage 时序：backup → trim → optimize → reimport → done|error。
type CompactProgress struct {
	Name     string `json:"name"`
	Stage    string `json:"stage"`
	Message  string `json:"message,omitempty"` // 阶段说明/终版回执
	Error    string `json:"error,omitempty"`
	Tier     string `json:"tier,omitempty"` // tier1 | tier2（实际达成路径，如实上报）
	BeforeMB int64  `json:"beforeMB"`
	AfterMB  int64  `json:"afterMB"`
}

const (
	compactFreeBuffer = 2 << 30   // 空间预检缓冲：VHDX 实占 + 2GB
	compactMinSaving  = 100 << 20 // Tier1 省量低于 100MB 视为无效，转 Tier2
)

// backupDirOr 解析瘦身备份落盘目录：留空=默认「下载\WSL 导出」；
// 指定则必须是绝对路径合法、存在且为目录或尚不存在（大盘备份常被 C 盘
// 容量卡住，允许指定其它卷）。
func backupDirOr(target string) (string, error) {
	t := strings.TrimSpace(strings.Trim(target, `"`))
	if t == "" {
		return exportDir(), nil
	}
	if !filepath.IsAbs(t) || !validMovePath(t) {
		return "", fmt.Errorf("备份目录须为含盘符的绝对路径且不含非法字符: %s", t)
	}
	clean := filepath.Clean(t)
	if st, err := os.Stat(clean); err == nil {
		if !st.IsDir() {
			return "", fmt.Errorf("备份目标是文件，请指定目录")
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("检查备份目录失败: %w", err)
	}
	return clean, nil
}

// CompactDistro 受理磁盘压缩。校验与空间预检同步完成（不足即时拒绝，
// 不烧 UAC 也不做半程手术），执行主体转后台协程推事件。
// backupDir 留空即用默认导出目录；备份/停机前的安全段可取消，
// 进入数据盘处理/重建段后取消会被拒绝（CancelCompact）。
func (s *WslService) CompactDistro(name, backupDir string) (OperationOutcome, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return OperationOutcome{}, err
	}
	version := ""
	for _, d := range s.wslDistros(ctx) {
		if strings.EqualFold(d.Name, name) {
			version = d.Version
			break
		}
	}
	if version != "2" {
		return OperationOutcome{}, fmt.Errorf("磁盘瘦身仅支持 WSL2 发行版（当前 WSL%s，无 ext4.vhdx 数据盘可言）", version)
	}
	store, err := s.lxss(ctx)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("注册表巡查失败，无法定位数据盘: %w", err)
	}
	entry, found := store[strings.ToLower(name)]
	if !found || entry.Vhdx == "" {
		return OperationOutcome{}, fmt.Errorf("未定位到 %s 的 ext4.vhdx，无法瘦身", name)
	}
	alloc, sparse, derr := diskAllocStats(entry.Vhdx)
	if derr != nil {
		return OperationOutcome{}, fmt.Errorf("数据盘实占探测失败: %w", derr)
	}
	bDir, berr := backupDirOr(backupDir)
	if berr != nil {
		return OperationOutcome{}, berr
	}
	// 空间预检：源盘（Tier2 重导入暂存）与备份盘（tar 落盘点）双向把关——
	// 两者可能不同卷，哪一头烧穿都会把手术台拆了。
	for _, probe := range []struct{ label, path string }{
		{"数据盘所在卷", entry.Vhdx},
		{"备份落盘卷", bDir},
	} {
		root := filepath.VolumeName(probe.path) + `\`
		free, ferr := diskFree(root)
		if ferr != nil {
			return OperationOutcome{}, fmt.Errorf("%s（%s）剩余空间查询失败: %w", probe.label, root, ferr)
		}
		if free < uint64(alloc)+compactFreeBuffer {
			return OperationOutcome{}, fmt.Errorf("空间预检不过：%s %s 剩余 %.1f GB，不足以安全进行（需数据盘实占 %.1f GB + 2GB 缓冲）——备份 tar 与重导入暂存都要吃空间", probe.label, root, float64(free)/(1<<30), float64(alloc)/(1<<30))
		}
	}
	finish, ok := s.tryBeginHeavyOp()
	if !ok {
		return OperationOutcome{}, errDistroBusy
	}
	opCtx, opCancel := context.WithCancel(context.Background())
	s.registerCompact(opCancel)
	go func() {
		defer opCancel()
		defer finish()
		defer s.finishCompact()
		s.runCompact(opCtx, name, entry.Vhdx, entry.BasePath, alloc, sparse, bDir)
	}()
	return OperationOutcome{Success: true, Message: fmt.Sprintf("开始为 %s 瘦身（备份到 %s；备份/停机段可取消，动盘后不可）", name, bDir)}, nil
}

// runCompact 后台主体。注意 ctx 独立于受理请求的 60s 校验 ctx；
// 阶段推进写入取消判据（setCompactStage），备份/停机段可被取消打断。
func (s *WslService) runCompact(parent context.Context, name, vhdx, basePath string, alloc int64, sparse bool, backupDir string) {
	ctx, cancel := context.WithTimeout(parent, 2*opTimeout) // 备份+压缩+重导入，双倍护栏
	defer cancel()
	emit := func(p CompactProgress) {
		p.Name, p.BeforeMB = name, alloc/(1<<20)
		s.emit(EventCompact, p)
	}
	fail := func(stage, msg string) {
		if canceled(ctx.Err()) {
			msg = "已按请求取消（「" + compactStageText[stage] + "」段中止，数据盘未被改动）；" + msg
		}
		emit(CompactProgress{Stage: "error", Error: msg, Tier: stage})
	}

	// 1) 强制备份：未压缩 tar（导入兼容性最稳），成功前绝不进入任何销毁步骤。
	s.setCompactStage("backup")
	emit(CompactProgress{Stage: "backup", Message: "第一步：全量导出备份 tar（数十 GB 时耗时较久，请勿退出；本段可取消）"})
	_, backupPath, err := s.exportCore(ctx, name, false, backupDir)
	if err != nil {
		fail("backup", fmt.Sprintf("备份失败，压缩中止（未对数据做任何改动）: %v", err))
		return
	}

	// 2) fstrim：运行态下把 guest 已删除块标记给宿主（尽力而为，失败不拦路）。
	s.setCompactStage("trim")
	emit(CompactProgress{Stage: "trim", Message: "第二步：guest 内 fstrim 释放已删块（本段可取消）"})
	if _, err := s.runWsl(ctx, "-d", name, "-u", "root", "--", "fstrim", "-v", "/"); err != nil {
		emit(CompactProgress{Stage: "trim", Message: "fstrim 未成功（发行版可能未运行或内核不支持），继续"})
	}

	// 3) 停机并确认停止（Optimize/注销都必须面对静止的 VHDX）。
	s.setCompactStage("waiting")
	if _, err := s.runWsl(ctx, "--terminate", name); err != nil {
		fail("waiting", fmt.Sprintf("终止 %s 失败，中止（备份已就绪：%s）: %v", name, backupPath, err))
		return
	}
	if !s.waitStopped(ctx, name, 60*time.Second) {
		if canceled(ctx.Err()) {
			fail("waiting", fmt.Sprintf("取消时停止确认未完成（备份已就绪：%s）", backupPath))
			return
		}
		fail("waiting", fmt.Sprintf("%s 迟迟未进入停止态，中止（备份已就绪：%s）", name, backupPath))
		return
	}

	// 4) Tier1：Hyper-V 模块在位才值得弹 UAC；Optimize-VHD 全量压缩。
	s.setCompactStage("optimize")
	if s.hyperVAvailable(ctx) {
		emit(CompactProgress{Stage: "optimize", Tier: "tier1", Message: "第三步（Tier1）：Optimize-VHD 全量压缩（UAC 提权）"})
		inner := fmt.Sprintf("$ProgressPreference='SilentlyContinue'; Import-Module Hyper-V -ErrorAction Stop; Optimize-VHD -Path %s -Mode Full", psQuote(vhdx))
		out, err := s.elevProc(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inner)
		if err == nil && out.Success {
			if after, _, aerr := diskAllocStats(vhdx); aerr == nil {
				if alloc-after >= compactMinSaving {
					emit(CompactProgress{Stage: "done", Tier: "tier1", AfterMB: after / (1 << 20),
						Message: fmt.Sprintf("%s 瘦身完成（Tier1）：实占 %.1f GB → %.1f GB，省 %.1f GB；备份保留在 %s", name, float64(alloc)/(1<<30), float64(after)/(1<<30), float64(alloc-after)/(1<<30), backupPath)})
					return
				}
				emit(CompactProgress{Stage: "optimize", Tier: "tier1", Message: "Tier1 省量不足 100MB（碎片本就不多），转 Tier2 注销重导入"})
			}
		} else {
			detail := out.Message
			if err != nil {
				detail = err.Error()
			}
			emit(CompactProgress{Stage: "optimize", Tier: "tier1", Message: "Tier1 未成功（" + detail + "），转 Tier2"})
		}
	} else {
		emit(CompactProgress{Stage: "optimize", Message: "本机无 Hyper-V 模块（家庭版常见），跳过 Tier1 直接 Tier2"})
	}

	// 5) Tier2：注销 → 重导入（数据经备份 tar 过一遍）。
	s.setCompactStage("reimport")
	emit(CompactProgress{Stage: "reimport", Tier: "tier2", Message: "第四步（Tier2）：注销实例并从备份重导入——数据以备份 tar 为准重建（此段不可取消）"})
	if out, err := s.runWsl(ctx, "--unregister", name); err != nil {
		fail("reimport", fmt.Sprintf("注销失败，中止（数据未动，备份仍在：%s）: %v %s", backupPath, err, strings.TrimSpace(out)))
		return
	}
	destDir := reimportDir(basePath, name)
	if out, err := s.runWsl(ctx, "--import", name, destDir, backupPath); err != nil {
		fail("reimport", fmt.Sprintf("重导入失败！注销已执行——你的数据现在只在备份 tar：%s。可到 Hanxi「➕ 添加实例」页以任意新名字恢复它，或原样恢复：%s（命令面 wsl --import %s <目标空目录> <备份>）。错误: %v %s",
			backupPath, backupPath, name, err, strings.TrimSpace(out)))
		return
	}
	// 原本稀疏的盘尽力恢复稀疏标志（失败只记说明，不判整体失败）。
	if sparse {
		if _, err := s.runWsl(ctx, "--manage", name, "--set-sparse", "true", "--allow-unsafe"); err != nil {
			emit(CompactProgress{Stage: "reimport", Tier: "tier2", Message: "稀疏属性恢复未成功（可稍后手动 wsl --manage --set-sparse），不影响数据"})
		}
	}
	after := int64(0)
	if nv, _, aerr := diskAllocStats(filepath.Join(destDir, "ext4.vhdx")); aerr == nil {
		after = nv
	}
	emit(CompactProgress{Stage: "done", Tier: "tier2", AfterMB: after / (1 << 20),
		Message: fmt.Sprintf("%s 瘦身完成（Tier2 重建）：新位置 %s；原实占 %.1f GB → 新实占 %.1f GB；备份保留在 %s", name, destDir, float64(alloc)/(1<<30), float64(after)/(1<<30), backupPath)})
}

// waitStopped 轮询 -q --running 直到目标不在运行名单（wsl 状态收敛有延迟）。
func (s *WslService) waitStopped(ctx context.Context, name string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		set, ok := s.quietSet(ctx, "--running")
		if ok && !set[strings.ToLower(name)] {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
}

// hyperVAvailable 只读探测 Hyper-V 模块（Tier1 前提，避免白白弹 UAC）。
const hyperVCheckScript = `if (Get-Module -ListAvailable -Name Hyper-V) { 'y' } else { 'n' }`

func (s *WslService) hyperVAvailable(ctx context.Context) bool {
	out, err := s.localPS(ctx, hyperVCheckScript)
	return err == nil && strings.TrimSpace(out) == "y"
}

// reimportDir Tier2 重导入落位：源 BasePath 旁的新目录（不与旧位置嵌套）。
func reimportDir(basePath, name string) string {
	dir := filepath.Dir(basePath)
	if dir == "" || dir == "." {
		dir = downloadDir()
	}
	return filepath.Join(dir, fmt.Sprintf("%s_reimport_%s", sanitizeFileNamePart(name), time.Now().Format("20060102-150405")))
}

// diskFree 卷剩余字节数；包级变量供单测替换（空间预检的确定性测试面）。
var diskFree = func(root string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}
