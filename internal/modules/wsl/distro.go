package wsl

// 发行版实例管理控制台：列表（跨语言状态归一 + 注册表占用信息）与操作面
// （唤终端 / 文件管理器 / 重启 / 停止 / 设默认 / 导出 / 迁移 / 删除附带
// 商店启动器清理 / 克隆 / 瘦身 / 只读详情 / wsl.conf 读写）。
// 命令面全部为 wsl.exe 公开子命令，形态参考同类仪表盘工具，实现全部自研；
// 安全基线对齐 InstallDistro 先例：每个操作先对 `wsl -l -q` 实时名单做白名单
// 校验，前端字符串绝不直接进命令（#37 三层防线的"事前"层）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

// exportDirName 导出落盘子目录（系统"下载"文件夹下，与 MSI 下载同址不同目录）。
const exportDirName = "WSL 导出"

// createNewConsole CREATE_NEW_CONSOLE: 让子进程在自己的可见控制台窗口里运行
// （Win11 起新控制台由系统"默认终端应用"接管呈现——设了 Windows Terminal 就出 WT）。
const createNewConsole = 0x00000010

// restartStoppedWait 重启时"确认已停止"的轮询上限；restartPollInterval 是包级
// 变量供单测加速（真实节奏 500ms）。
const restartStoppedWait = 10 * time.Second

var restartPollInterval = 500 * time.Millisecond

// DistroInstance 管理控制台的发行版实例行。
// Running/Default 为归一后的布尔语义（状态列原文是本地化文案，跨语言系统下
// 不可作为判据；运行态以 `wsl -l -q --running` 名单为准，默认以 `wsl -l -q`
// 首行为准，均带回退）。
type DistroInstance struct {
	Name      string `json:"name"`
	Running   bool   `json:"running"`
	Default   bool   `json:"default"`
	Version   string `json:"version"` // "1" | "2"（表尾列，非本地化）
	StateText string `json:"stateText"`
	BasePath  string `json:"basePath"`
	VhdxPath  string `json:"vhdxPath"`
	SizeBytes int64  `json:"sizeBytes"`
}

// DistroOpResult 发行版操作统一回执；导出成功后 ID/Path 供「打开位置」白名单回查。
type DistroOpResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
	Path    string `json:"path,omitempty"`
}

// ExportRecord 导出工件登记（本会话内有效）。多份导出全部在册——
// 前端经 ListDistroExports 拉全量列表，逐份可「打开位置」。
type ExportRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"` // 源发行版名
	Path string `json:"path"`
	Size int64  `json:"size"`
	At   string `json:"at"` // 落盘时间（与文件名时间戳同源）
}

// ---- 列表 ----

// ListInstances 汇总本机发行版现状：-l -v 拿名称/版本，-q 名单归一运行态与默认，
// Lxss 注册表补安装路径与 VHDX 占用。任何一路取数失败都不谎报：
// 缺失字段留空、状态回退 -l -v 原文判读，列表本体拿不到才报错。
func (s *WslService) ListInstances() ([]DistroInstance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows := s.wslDistros(ctx)
	if len(rows) == 0 {
		// 与 readiness.Distros 的"空即正常"语义一致：未安装任何发行版不是错误。
		return []DistroInstance{}, nil
	}

	runningSet, runningOK := s.quietSet(ctx, "--running")
	defName, defOK := "", false
	if names, err := s.quietNames(ctx); err == nil && len(names) > 0 {
		defName, defOK = names[0], true
	}
	store, _ := s.lxss(ctx) // 注册表是补充信息源，取不到只影响路径/占用列

	list := make([]DistroInstance, 0, len(rows))
	for _, d := range rows {
		inst := DistroInstance{Name: d.Name, StateText: d.State, Version: d.Version}
		switch {
		case runningOK:
			inst.Running = runningSet[strings.ToLower(d.Name)]
		default:
			inst.Running = stateLooksRunning(d.State)
		}
		switch {
		case defOK:
			inst.Default = strings.EqualFold(defName, d.Name)
		default:
			inst.Default = d.Default // 回退 -l -v 的 `*` 前缀（非本地化标记）
		}
		if e, ok := store[strings.ToLower(d.Name)]; ok {
			inst.BasePath = e.BasePath
			inst.VhdxPath = e.Vhdx
			inst.SizeBytes = e.Size
		}
		list = append(list, inst)
	}
	return list, nil
}

// quietNames 执行 `wsl -l -q`：quiet 模式输出 NUL 分隔的发行版原名，
// 天然跨语言（首行即默认发行版，微软 CLI 约定）。
func (s *WslService) quietNames(ctx context.Context, extra ...string) ([]string, error) {
	out, err := s.runWsl(ctx, append([]string{"-l", "-q"}, extra...)...)
	names := parseQuietList(out)
	if len(names) == 0 && err != nil {
		return nil, err
	}
	return names, nil
}

func (s *WslService) quietSet(ctx context.Context, extra ...string) (map[string]bool, bool) {
	names, err := s.quietNames(ctx, extra...)
	if err != nil {
		return nil, false
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[strings.ToLower(n)] = true
	}
	return set, true
}

// parseQuietList 拆 NUL 分隔名单（decodeWslOutput 已把 UTF-16 解成 UTF-8 串并
// trim 两端，NUL 分隔符原样保留在中间）。
func parseQuietList(out string) []string {
	var names []string
	for part := range strings.SplitSeq(out, "\x00") {
		if n := strings.TrimSpace(part); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// stateLooksRunning 仅在 -q --running 通道不可得时回退使用：
// 状态列原文本地化，中英文各匹配一种形态即可（Running/正在运行）。
func stateLooksRunning(state string) bool {
	lower := strings.ToLower(state)
	return strings.Contains(lower, "running") || strings.Contains(state, "正在")
}

// lxssEntry 是 Lxss 注册表巡查脚本的行模型。
type lxssEntry struct {
	Name     string `json:"name"`
	BasePath string `json:"basePath"`
	Vhdx     string `json:"vhdx"`
	Size     int64  `json:"size"`
	Pfn      string `json:"pfn"` // PackageFamilyName（商店发行版才有）
}

// lxssScript 只读枚举 HKCU Lxss 发行版登记，补 ext4.vhdx 路径与字节数。
// 单元素管道会被 ConvertTo-Json 摊成对象而非数组，Go 侧双形态解析兜底。
const lxssScript = `$items = @(Get-ItemProperty 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Lxss\*' -ErrorAction SilentlyContinue | ForEach-Object {
  $vhdx = ''
  $size = [int64]0
  if ($_.BasePath) {
    $c1 = Join-Path $_.BasePath 'ext4.vhdx'
    $c2 = Join-Path $_.BasePath 'LocalState\ext4.vhdx'
    if (Test-Path $c1) { $vhdx = $c1 } elseif (Test-Path $c2) { $vhdx = $c2 }
    if ($vhdx) { $size = (Get-Item -LiteralPath $vhdx).Length }
  }
  [pscustomobject]@{ name = $_.DistributionName; basePath = $_.BasePath; vhdx = $vhdx; size = $size; pfn = [string]$_.PackageFamilyName }
})
ConvertTo-Json -Compress -InputObject $items`

func (s *WslService) lxss(ctx context.Context) (map[string]lxssEntry, error) {
	out, err := s.localPS(ctx, lxssScript)
	if err != nil || out == "" {
		return nil, err
	}
	var entries []lxssEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		var one lxssEntry
		if err2 := json.Unmarshal([]byte(out), &one); err2 != nil {
			return nil, fmt.Errorf("Lxss 巡查输出解析失败: %w", err)
		}
		entries = []lxssEntry{one}
	}
	m := make(map[string]lxssEntry, len(entries))
	for _, e := range entries {
		if e.Name != "" {
			m[strings.ToLower(e.Name)] = e
		}
	}
	return m, nil
}

// ---- 白名单与互斥 ----

// distroAllowed 以 `wsl -l -q` 实时名单校验发行版名；名单取不到时保守拒绝
// （操作面无"读不到就放行"的余地：拿不准目标是否存在时执行命令等于裸奔）。
func (s *WslService) distroAllowed(ctx context.Context, name string) error {
	names, err := s.quietNames(ctx)
	if err != nil {
		return fmt.Errorf("无法获取本机发行版清单，操作中止: %w", err)
	}
	if slices.Contains(names, name) {
		return nil
	}
	return fmt.Errorf("发行版 %q 不在本机已安装清单中，已拒绝执行", name)
}

// tryBeginDistroOp 每发行版单飞闸（迁移等重操作叠加全局闸）；返回的 finish
// 必须 defer。忙则 false——与参考实现的"哨兵检查拒绝而非排队"一致，
// 快速失败让用户看到实情，不制造排队假象。
func (s *WslService) tryBeginDistroOp(name, op string) (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heavyOps > 0 {
		return nil, false
	}
	key := strings.ToLower(name)
	if _, busy := s.distroOps[key]; busy {
		return nil, false
	}
	s.distroOps[key] = op
	return func() {
		s.mu.Lock()
		delete(s.distroOps, key)
		s.mu.Unlock()
	}, true
}

// tryBeginHeavyOp 迁移专属：会 `wsl --shutdown` 打停全部运行实例，
// 与任何发行版操作互斥。
func (s *WslService) tryBeginHeavyOp() (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heavyOps > 0 || len(s.distroOps) > 0 {
		return nil, false
	}
	s.heavyOps++
	return func() {
		s.mu.Lock()
		s.heavyOps--
		s.mu.Unlock()
	}, true
}

var errDistroBusy = fmt.Errorf("该发行版或 WSL 子系统有操作正在进行，请等待其完成后重试")

// ---- 操作面（参数全部先过白名单，前端零拼接面） ----

// OpenTerminal 唤终端即启动：拉起系统默认终端进入该发行版。
// 刻意不做后台保活——WSL 无前台进程时自动停机是平台语义，
// 塞一个野生的 sleep 进程换"常亮"状态，生命周期没人收尸，得不偿失。
func (s *WslService) OpenTerminal(name string) (DistroOpResult, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	if err := s.startTerm(ctx, name); err != nil {
		return DistroOpResult{}, fmt.Errorf("启动终端失败: %w", err)
	}
	return DistroOpResult{Success: true, Message: fmt.Sprintf("已为 %s 启动终端会话", name)}, nil
}

// openDistroFolderExplorer 用 explorer.exe 打开发行版 9P 共享根（\\wsl$\<名>）。
// 刻意不复用 AppService.OpenPath（其 explorer.exe <file> 语义在文件对象上是"执行"，
// 与 bcu/ccswitch 等模块同纪律）；name 已过 wsl -l 白名单，单 argv 传参不进 shell。
// Start 后即 Release 脱手：explorer 窗口生命周期归用户。
func openDistroFolderExplorer(_ context.Context, name string) error {
	cmd := exec.Command("explorer.exe", `\\wsl$\`+name)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// OpenDistroFolder 资源管理器打开发行版文件共享。停止的发行版 9P 共享不存在
// （explorer 会报"路径不存在"），故确认停止时先静默拉起（echo 探针）——explorer
// 持有目录句柄期间 9P 会话活跃不会被空闲停机，关窗后按平台语义自然回落。
// 与唤终端同属"只读外呼"：免确认、免单飞闸（无状态改动可竞）。
func (s *WslService) OpenDistroFolder(name string) (DistroOpResult, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	lifted := false
	if set, ok := s.quietSet(ctx, "--running"); ok && !set[strings.ToLower(name)] {
		if out, err := s.runWsl(ctx, "-d", name, "--exec", "/bin/echo", "hanxi-start-probe"); err != nil {
			return DistroOpResult{}, fmt.Errorf("无法启动 %s，打开文件取消: %w %s", name, err, strings.TrimSpace(out))
		}
		lifted = true
	}
	if err := s.openFolder(ctx, name); err != nil {
		return DistroOpResult{}, fmt.Errorf("打开资源管理器失败: %w", err)
	}
	msg := fmt.Sprintf("已在资源管理器打开 \\\\wsl$\\%s", name)
	if lifted {
		msg += "（发行版原为停止，已顺手拉起）"
	}
	return DistroOpResult{Success: true, Message: msg}, nil
}

// RestartDistro 重启发行版：停止 → 轮询确认已停 → echo 探针拉起验证。
// 语义拍板（对齐讨论，刻意有别于 wsl-dashboard）：拉起验证即止、不做
// sleep infinity 后台保活——无前台会话数秒后自动回落"已停止"是平台语义
// （见 OpenTerminal 注释），回执如实讲清，不拿保活假装"常亮运行"。
// 探针走 runWsl（HideWindow 捕获输出）而非 startTerm：重启不该弹终端窗口。
func (s *WslService) RestartDistro(name string) (DistroOpResult, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	finish, ok := s.tryBeginDistroOp(name, "restart")
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()

	key := strings.ToLower(name)
	set, setOK := s.quietSet(ctx, "--running")
	caveat := ""
	switch {
	case !setOK:
		// 运行名单不可得：停止步无法求证，terminate 尽力而为 + 回执如实标注。
		_, _ = s.runWsl(ctx, "--terminate", name)
		caveat = "（运行名单不可得，停止步骤未求证）"
	case set[key]:
		// terminate 返回 ≠ 已出名单（收敛有滞后），不轮询确认就拉起会"假重启"——
		// 参考实现同款时序坑，此处以名单为唯一判据。
		if _, err := s.runWsl(ctx, "--terminate", name); err != nil {
			return DistroOpResult{}, fmt.Errorf("终止 %s 失败，重启中止: %w", name, err)
		}
		if err := s.waitDistroStopped(ctx, key); err != nil {
			return DistroOpResult{}, err
		}
	}
	if out, err := s.runWsl(ctx, "-d", name, "--exec", "/bin/echo", "hanxi-restart-ok"); err != nil {
		return DistroOpResult{}, fmt.Errorf("%s 启动失败，重启未完成: %w %s", name, err, strings.TrimSpace(out))
	}
	return DistroOpResult{
		Success: true,
		Message: fmt.Sprintf("%s 已重启：启动验证通过。之后不进入终端的话，发行版空闲片刻会自动回落为「已停止」——属平台常态 %s", name, caveat),
	}, nil
}

// waitDistroStopped 轮询运行名单直到 name 退出。超上限仍未退出即中止重启
// （宁可如实失败，不对没收住的运行态直接拉起）。
func (s *WslService) waitDistroStopped(ctx context.Context, key string) error {
	deadline := time.Now().Add(restartStoppedWait)
	for {
		if set, ok := s.quietSet(ctx, "--running"); ok && !set[key] {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("终止后 %v 仍在运行名单中，重启中止——请先手动「⏹ 停止」确认状态再重启", restartStoppedWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(restartPollInterval):
		}
	}
}

// TerminateDistro 终止发行版（wsl --terminate，用户态命令，数据无损、幂等）。
func (s *WslService) TerminateDistro(name string) (DistroOpResult, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	finish, ok := s.tryBeginDistroOp(name, "terminate")
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()
	if out, err := s.runWsl(ctx, "--terminate", name); err != nil {
		return DistroOpResult{}, fmt.Errorf("终止 %s 失败: %w %s", name, err, strings.TrimSpace(out))
	}
	return DistroOpResult{Success: true, Message: fmt.Sprintf("%s 已终止", name)}, nil
}

// SetDefaultDistro 设默认发行版（wsl --set-default，用户态命令）。
func (s *WslService) SetDefaultDistro(name string) (DistroOpResult, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	finish, ok := s.tryBeginDistroOp(name, "set-default")
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()
	if out, err := s.runWsl(ctx, "--set-default", name); err != nil {
		return DistroOpResult{}, fmt.Errorf("设置默认发行版失败: %w %s", err, strings.TrimSpace(out))
	}
	return DistroOpResult{Success: true, Message: fmt.Sprintf("%s 已设为默认发行版", name)}, nil
}

// UnregisterDistro 删除发行版：数据销毁级操作。
// 先 terminate 再 unregister 是参考实现用卡死事故换来的教训——对运行中的
// 发行版直接 unregister 可能长时间挂起；terminate 失败不拦路（本就停止则
// 是幂等空转），unregister 的成败以退出码与输出如实上报。
func (s *WslService) UnregisterDistro(name string) (DistroOpResult, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	finish, ok := s.tryBeginDistroOp(name, "unregister")
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()
	// 商店身份要在注销前抓：Lxss 登记一删，PFN 就无处可查了。
	pfn := ""
	if store, err := s.lxss(ctx); err == nil {
		pfn = store[strings.ToLower(name)].Pfn
	}
	_, _ = s.runWsl(ctx, "--terminate", name) // 尽力收敛运行态，成败交给下一步兜底
	if out, err := s.runWsl(ctx, "--unregister", name); err != nil {
		return DistroOpResult{}, fmt.Errorf("删除 %s 失败: %w %s", name, err, strings.TrimSpace(out))
	}
	return DistroOpResult{
		Success: true,
		Message: fmt.Sprintf("%s 已删除（数据文件由系统移除）%s", name, s.cleanupLauncher(ctx, pfn)),
	}, nil
}

// cleanupLauncher 删除发行版后清理商店 Appx 启动器（"删干净"的最后一环）：
// 仅当 PFN 未被其它在册发行版共用时才 Remove-AppxPackage（动了会连坐别人——
// 如实保留说明）；当前用户态操作免提权，best-effort：任何失败都不拦删除主流程，
// 只在回执尾注点名剩余事项。
func (s *WslService) cleanupLauncher(ctx context.Context, pfn string) string {
	if pfn == "" {
		return "" // 非商店形态（导入/rootfs），本就没有启动器
	}
	if store, err := s.lxss(ctx); err != nil {
		return "；启动器引用计数求证失败，如有残留在「设置→应用」请自行卸载"
	} else {
		for _, e := range store {
			if strings.EqualFold(e.Pfn, pfn) {
				return "；该启动器包仍被其它发行版共用，未卸载"
			}
		}
	}
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// 存在性由脚本自证（absent 幂等），Go 侧只区分"执行失败/确实卸载/本就没有"。
	script := fmt.Sprintf("$p = Get-AppxPackage -PackageFamilyName %s; if ($p) { $p | Remove-AppxPackage | Out-Null; 'removed' } else { 'absent' }", psQuote(pfn))
	out, err := s.localPS(cctx, script)
	if err != nil {
		return fmt.Sprintf("；商店启动器卸载失败（%s）——可在「设置→应用」手动处理", strings.TrimSpace(err.Error()))
	}
	if strings.Contains(out, "removed") {
		return "；商店启动器已一并卸载"
	}
	return ""
}

// exportIDRe 导出工件名白名单形态（文件名本身由后端拼装，Reveal 回查时严格匹配；
// 字母数字含 Unicode 类，与 sanitizeFileNamePart 的清洗口径对齐）。
var exportIDRe = regexp.MustCompile(`^[-\pL\pN ._()]{1,120}\.(tar|tar\.gz)$`)

// exportDir 导出落盘目录；包级变量供单测替换临时目录（revealInExplorer 同款手法）。
var exportDir = func() string { return filepath.Join(downloadDir(), exportDirName) }

// ExportDistro 导出为 tar（gzip=true 走 --format tar.gz，WSL 2.4.4+）。
// 落盘固定到「下载\WSL 导出」目录，文件名后端拼装（清洗后的发行版名 + 时间戳），
// 同名不覆盖——不收前端路径参数，杜绝本接口被当任意写盘面。
// 长操作同步等待（重型通道 30 分钟护栏），失败清理半成品文件并如实报因。
func (s *WslService) ExportDistro(name string, gzip bool) (DistroOpResult, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	finish, ok := s.tryBeginDistroOp(name, "export")
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()

	id, path, err := s.exportCore(ctx, name, gzip, exportDir())
	if err != nil {
		return DistroOpResult{}, err
	}
	return DistroOpResult{
		Success: true,
		Message: fmt.Sprintf("%s 已导出（%s，%.1f MB）", name, id, sizeMB(path)),
		ID:      id, Path: path,
	}, nil
}

// exportCore 导出主体（不含白名单与单飞闸——由调用方将守门，
// 磁盘压缩的强制备份步骤在重操作闸内直接复用本函数）。
// 返回实落盘的工件名与路径，并登记进导出记录。
func (s *WslService) exportCore(ctx context.Context, name string, gzip bool, dir string) (id, path string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("创建导出目录失败: %w", err)
	}
	ext := ".tar"
	args := []string{"--export", name}
	if gzip {
		ext, args = ".tar.gz", append(args, "--format", "tar.gz")
	}
	started := time.Now()
	id = fmt.Sprintf("%s-%s%s", sanitizeFileNamePart(name), started.Format("20060102-150405"), ext)
	// 同秒重名（双击/多实例同名）走 uniquePath 递增不覆盖，ID 取实落盘文件名。
	path = uniquePath(dir, id)
	id = filepath.Base(path)
	args = append(args, path)

	if out, err := s.runWsl(ctx, args...); err != nil {
		_ = os.Remove(path) // 半成品不留在用户下载目录
		return "", "", fmt.Errorf("导出 %s 失败: %w %s", name, err, strings.TrimSpace(out))
	}
	if _, err := os.Stat(path); err != nil {
		return "", "", fmt.Errorf("导出命令返回成功但文件未落盘: %w", err)
	}
	s.mu.Lock()
	s.exportRecords[id] = ExportRecord{ID: id, Name: name, Path: path, Size: fileSize(path), At: started.Format("2006-01-02 15:04:05")}
	s.mu.Unlock()
	return id, path, nil
}

// ListDistroExports 返回本会话全部导出工件登记（新→旧）。
// 登记只在内存：跨会话的旧工件到「下载\WSL 导出」文件夹自查。
func (s *WslService) ListDistroExports() []ExportRecord {
	s.mu.Lock()
	out := make([]ExportRecord, 0, len(s.exportRecords))
	for _, r := range s.exportRecords {
		out = append(out, r)
	}
	s.mu.Unlock()
	slices.SortFunc(out, func(a, b ExportRecord) int { return strings.Compare(b.At, a.At) })
	return out
}

// RevealDistroExport 在资源管理器中定位导出产物；只认本会话后端自己登记过的
// 工件名（RevealDownload 同款红线：不接受任意路径）。
func (s *WslService) RevealDistroExport(id string) error {
	id = strings.TrimSpace(id)
	if !exportIDRe.MatchString(id) {
		return fmt.Errorf("导出文件名 %q 格式不合法", id)
	}
	s.mu.Lock()
	rec, found := s.exportRecords[id]
	s.mu.Unlock()
	path := ""
	if found {
		path = rec.Path
	}
	if path == "" {
		return fmt.Errorf("该导出由其它会话产生或记录已失效，请前往「下载\\%s」查找", exportDirName)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("文件已不在原位: %w", err)
	}
	return revealInExplorer(path)
}

// moveTarget 迁移目标路径的后端校验：绝对路径、盘符真实存在、
// 目标目录不存在则允许（由本命令创建）、存在则必须为空目录；
// 拒绝迁进当前实例自身目录（自我吞噬）。
func moveTarget(target, currentBasePath string) (string, error) {
	target = strings.TrimSpace(strings.Trim(target, `"`))
	if target == "" || !filepath.IsAbs(target) {
		return "", fmt.Errorf("迁移目标必须是绝对路径（如 D:\\WSL\\Ubuntu）")
	}
	if !validMovePath(target) {
		return "", fmt.Errorf("迁移目标路径含非法字符或缺盘符")
	}
	root := filepath.VolumeName(target) + `\`
	if _, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("目标盘 %s 不存在", root)
	}
	clean := filepath.Clean(target)
	if currentBasePath != "" && strings.EqualFold(clean, filepath.Clean(currentBasePath)) {
		return "", fmt.Errorf("目标与当前安装位置相同，无需迁移")
	}
	if currentBasePath != "" {
		rel := strings.ToLower(clean)
		base := strings.ToLower(filepath.Clean(currentBasePath))
		if strings.HasPrefix(rel, base+`\`) || strings.HasPrefix(base, rel+`\`) {
			return "", fmt.Errorf("目标目录与当前安装位置存在嵌套关系，已拒绝")
		}
	}
	if st, err := os.Stat(clean); err == nil {
		if !st.IsDir() {
			return "", fmt.Errorf("目标路径已是文件，请选择空目录或新目录")
		}
		entries, err := os.ReadDir(clean)
		if err != nil {
			return "", fmt.Errorf("无法读取目标目录: %w", err)
		}
		if len(entries) > 0 {
			return "", fmt.Errorf("目标目录非空——wsl --move 要求空目录或不存在的路径")
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("检查目标目录失败: %w", err)
	}
	return clean, nil
}

// validMovePath 拒绝 Windows 路径非法字符（尖括号/引号/竖线/问号/星号）。
// 冒号只允许出现在盘符位（IsAbs 已保证 D:\ 形态，盘符之后再遇冒号即非法）。
func validMovePath(p string) bool {
	if filepath.VolumeName(p) == "" || strings.ContainsAny(p, `<>|"?*`) {
		return false
	}
	rest := p
	if len(rest) >= 2 && rest[1] == ':' {
		rest = rest[2:]
	}
	return !strings.Contains(rest, ":")
}

// MoveDistro 迁移 VHDX 到其他盘（WSL2 通道 wsl --manage --move）。
// 全程单一提权会话内完成：`wsl --shutdown`（迁移会打停全部运行实例，
// 前端确认框已点名）→ 等 3s → 移动命令对 sharing-violation 型瞬时冲突
// 重试至多 5 次 → $LASTEXITCODE 逐层传播（#37 红线：绝不"执行完毕"式假成功）。
// 提权理由与参考项目一致：WSL 2.7.8+ 不提权移动常撞 E_ACCESSDENIED。
func (s *WslService) MoveDistro(name, target string) (DistroOpResult, error) {
	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	finish, ok := s.tryBeginHeavyOp()
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()

	basePath := ""
	if store, err := s.lxss(ctx); err == nil {
		if e, found := store[strings.ToLower(name)]; found {
			basePath = e.BasePath
		}
	}
	clean, err := moveTarget(target, basePath)
	if err != nil {
		return DistroOpResult{}, err
	}

	inner := fmt.Sprintf(
		"wsl --shutdown; Start-Sleep -Seconds 3; "+
			"$tries = 0; while ($true) { $tries++; wsl --manage %s --move %s; "+
			"if ($LASTEXITCODE -eq 0) { exit 0 }; if ($tries -ge 5) { exit $LASTEXITCODE }; Start-Sleep -Seconds 3 }",
		psQuote(name), psQuote(clean),
	)
	out, err := s.elevProc(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inner)
	if err != nil {
		return DistroOpResult{}, err
	}
	if !out.Success {
		return DistroOpResult{Success: false, Message: out.Message}, nil // UAC 取消等：如实回执不报错
	}
	// 移动成功的复验：注册表 BasePath 确实指向新位置才敢报成功。
	msg := fmt.Sprintf("%s 迁移完成，已回到新位置继续运行", name)
	if store, serr := s.lxss(ctx); serr == nil {
		if e, found := store[strings.ToLower(name)]; found && !strings.EqualFold(filepath.Clean(e.BasePath), clean) {
			msg = fmt.Sprintf("迁移命令执行完毕，但注册表位置（%s）与目标（%s）不一致，请重新体检核实", e.BasePath, clean)
		}
	}
	return DistroOpResult{Success: true, Message: msg}, nil
}

// startTerminalSession 直接以 CREATE_NEW_CONSOLE 拉起 wsl.exe 交互会话：
// 子进程自带一个全新可见控制台窗口（Win11 由"默认终端应用"接管呈现——设了
// Windows Terminal 就出 WT，老系统出 conhost），不再借 cmd start 转手。
// 历史踩坑（2026-09-15）：旧实现 cmd.exe /c start + HideWindow，本意"只藏 cmd 壳"，
// 实际 start 开的新窗口会继承 cmd 启动信息里的 SW_HIDE——窗口存在但完全不可见，
// 用户感知"点了没弹终端"（发行版其实已被拉起）。详见 docs/TROUBLESHOOTING.md。
// 终端会话生命周期归用户：Start 后即 Release 句柄脱手，Hanxi 退出与体检 ctx
// 超时都不牵连它（与 launcher runExe 同款语义）；刻意不做后台保活，见 OpenTerminal 注释。
func startTerminalSession(_ context.Context, name string) error {
	// 刻意不用 CommandContext：wsl.exe 会活到用户敲 exit 为止，
	// 挂 60s 体检 ctx 会在超时处把会话击杀——这里只取 name，ctx 不外接。
	cmd := exec.Command("wsl.exe", "-d", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release() // Windows 无僵尸进程语义，Release 即彻底脱手不留句柄
}

// ---- 小工具 ----

// psQuote PowerShell 单引号安全字面量（” 转义），提权/巡查脚本内嵌参数统一走它。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// sanitizeFileNamePart 发行版名转安全文件名片段：非法字符落为下划线。
var fileNameIllegalRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func sanitizeFileNamePart(s string) string {
	s = fileNameIllegalRe.ReplaceAllString(strings.TrimSpace(s), "_")
	if s == "" {
		s = "distro"
	}
	return s
}

func fileSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

func sizeMB(path string) float64 {
	return float64(fileSize(path)) / (1024 * 1024)
}
