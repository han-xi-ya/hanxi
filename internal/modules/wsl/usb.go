package wsl

// F9 USB 直通（usbipd-win 集成）：设备表管理 + "开机自动共享"账本重放。
//
// 范式来源（BACKLOG F9 卡片裁定，逐条对齐 portproxy 账本先例）：
//   - 期望态存 Hanxi 自有账本（<StateDir>/wsl-usbipd.json），usbipd 自己的绑定/
//     自动附加状态与外部工具的规则一律不代管不触碰；
//   - 开机自动共享 = 账本 + 重放（hanxi 启动延时补打一发、hanxi 拉起的发行版
//     起停后补打一发），不自建 Windows 计划任务；代价是仅 hanxi 在跑时生效，
//     收益是账本可视化、可勾选停用、无系统残留；总开关默认关；
//   - 重放计划是纯函数（账本 × 设备表 × 运行名单 → 动作序列），单测离线可断言；
//   - bind 必须管理员：走本模块白名单提权通道（M10 基建，portkill 同款），
//     busid/GUID 全部正则白名单校验，前端零拼接面；attach/detach 用户态；
//   - 提权批量按 portproxy 先例合批：一条 PowerShell 链式 bind、逐条传播退出码
//     （#37 红线：半途失败不谎报整体成功）；
//   - 失败只记账不炸启动：任何一步失败落在条目的 lastStatus 上，UI 如实呈现；
//   - 不做 GUI 级驱动安装、不做远程主机 usbip（卡片边界）。
//
// usbipd-win 未安装时：GetUsbOverview 回 installed=false（非错误），前端渲染
// 引导卡（打开官方 releases 页 + winget 安装命令复制）；不代装（托管深度按
// 卡片裁定止步于引导）。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"hanxi/internal/jsonstore"
	"hanxi/internal/modules/wsl/usbipd"
)

// usbipdReleasesURL usbipd-win 官方发布页（引导卡固定地址，不接受前端传入）。
const usbipdReleasesURL = "https://github.com/dorssel/usbipd-win/releases"

// 重放动作种类（planUsbReplay 输出词表）。
const (
	usbStepAttach = "attach"      // 已共享未附加：直接 attach
	usbStepBind   = "bind+attach" // 未共享：提权 bind 后 attach
	usbStepSkip   = "skip"        // 跳过（设备不在场/发行版未运行/已附加…）
)

// USBShareEntry 账本里的一条"期望共享"记录（设备 × 目标发行版）。
type USBShareEntry struct {
	ID          string `json:"id"`
	BusID       string `json:"busId"` // 记录时的总线号（仅作位置提示，命中仍须核验物理身份）
	InstanceID  string `json:"instanceId,omitempty"`
	Vid         string `json:"vid,omitempty"`
	Pid         string `json:"pid,omitempty"`
	Serial      string `json:"serial,omitempty"`
	Guid        string `json:"guid,omitempty"`
	Description string `json:"description,omitempty"` // 设备名快照（账本表可读性）
	Distro      string `json:"distro"`
	Enabled     bool   `json:"enabled"`
	AddedAt     string `json:"addedAt"`
	LastStatus  string `json:"lastStatus,omitempty"` // 最近一次重放/操作归因
	LastAt      string `json:"lastAt,omitempty"`
}

// UsbView "USB 直通"页签总载荷。
type UsbView struct {
	Installed    bool            `json:"installed"`
	Version      string          `json:"version,omitempty"`
	Error        string          `json:"error,omitempty"` // 取数失败说明（未装不算失败）
	Devices      []usbipd.Device `json:"devices"`
	Ledger       []USBShareEntry `json:"ledger"`
	AutoEnabled  bool            `json:"autoEnabled"` // 开机自动共享总开关（默认关）
	ReplayBusy   bool            `json:"replayBusy"`
	LastReplay   string          `json:"lastReplay,omitempty"` // 最近一次后台重放摘要
	ReleasesPage string          `json:"releasesPage"`         // 引导卡固定跳转地址
}

// usbLedgerFile 账本文件形态。
type usbLedger struct {
	AutoEnabled bool            `json:"autoEnabled"`
	Entries     []USBShareEntry `json:"entries"`
}

// usbCLI usbipd 用户态命令面（单测替身注入点；usbipd.Runner 实现）。
type usbCLI interface {
	Version(ctx context.Context) (string, error)
	State(ctx context.Context) ([]usbipd.Device, error)
	Attach(ctx context.Context, distro, busID string) error
	Detach(ctx context.Context, busID string) error
}

// usbLedgerMu 账本"读 → 改 → 写"全程串行锁（ppLedgerMu 同款纪律）。
// 锁序：usbLedgerMu → s.mu，反向持锁禁止。
var usbLedgerMu sync.Mutex

var errUsbLedgerBusy = errors.New("USB 共享账本正被占用（编辑或重放在飞），请稍候再试")

// usbStartupReplayDelay 启动重放的等待窗口：usbipd 服务与 WSL 栈在开机后
// 需要时间就绪；包级变量供单测缩短。
var usbStartupReplayDelay = 12 * time.Second

// usbDistroReplayDelay 发行版启动后补打重放的小窗口（等 guest 服务就绪）。
var usbDistroReplayDelay = 3 * time.Second

// usbWatcherInterval 是自动共享插拔观察器的低频采样周期；只在总开关开启且
// 账本非空时常驻。包级变量供单测缩短。
var usbWatcherInterval = 15 * time.Second

// usbWatcherDebounce absent→present 边沿的去抖窗口；连续两拍确认仍在场后才重放，
// 避免 USB 枚举过程中的瞬态设备表触发 UAC/attach。
var usbWatcherDebounce = 2 * time.Second

// ---- 账本持久化 ----

func (s *WslService) usbLoad() (usbLedger, error) {
	var led usbLedger
	if s.usbPath == "" {
		return led, fmt.Errorf("数据存储路径不可用，无法管理 USB 共享账本")
	}
	ok, err := jsonstore.Load(s.usbPath, &led)
	if err != nil && !errors.Is(err, jsonstore.ErrEmpty) && !errors.Is(err, jsonstore.ErrCorrupt) {
		return led, fmt.Errorf("读取 USB 共享账本失败: %w", err)
	}
	// 损坏/空文件：严格型——拒绝当空库覆盖（frpc/memo 同纪律），给用户逃生口。
	if err != nil {
		return led, fmt.Errorf("USB 共享账本文件损坏（%s），已拒绝覆盖写入——请人工修复或删除该文件: %w", s.usbPath, err)
	}
	_ = ok
	return led, nil
}

func (s *WslService) usbSave(led usbLedger) error {
	if s.usbPath == "" {
		return fmt.Errorf("数据存储路径不可用，无法保存 USB 共享账本")
	}
	return jsonstore.Save(s.usbPath, led)
}

// ---- 重放计划（纯函数，单测主战场） ----

// usbPlanStep 重放计划的一步。
type usbPlanStep struct {
	Kind   string // usbStepAttach | usbStepBind | usbStepSkip
	Entry  USBShareEntry
	BusID  string // 此刻实际总线号（换插口回落后可能与账本记录不同）
	Distro string
	Reason string // skip 的白话归因 / 执行提示（如"已在新插口 x-y"）
}

// planUsbReplay 账本 × 当前设备表 × 运行名单 → 动作序列。
// 匹配次序：账本 busid 原样在场时仍核验物理身份；原端口被其他设备占用时明确
// skip。换插口仅接受唯一稳定身份命中；旧账本没有新身份字段时，保守兼容唯一
// VID:PID 候选，多义即拒绝。已附加、发行版未运行、不兼容集线器都跳过且如实
// 记因；未共享的先 bind（提权）后 attach。
// runningOK=false（运行名单通道故障）时不做未运行拦截，交给 usbipd 自己报错。
func planUsbReplay(entries []USBShareEntry, devices []usbipd.Device, running map[string]bool, runningOK bool) []usbPlanStep {
	var steps []usbPlanStep
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		dev, note, found := matchUsbDevice(e, devices)
		if !found {
			if note == "" {
				note = "设备不在场（等待重新插入后自动补挂）"
			}
			steps = append(steps, usbPlanStep{Kind: usbStepSkip, Entry: e, Distro: e.Distro, Reason: note})
			continue
		}
		if dev.State == usbipd.StateIncompatible {
			steps = append(steps, usbPlanStep{Kind: usbStepSkip, Entry: e, BusID: dev.BusID, Distro: e.Distro, Reason: "不兼容的集线器，usbipd 拒绝共享"})
			continue
		}
		if dev.State == usbipd.StateAttached {
			steps = append(steps, usbPlanStep{Kind: usbStepSkip, Entry: e, BusID: dev.BusID, Distro: e.Distro, Reason: "已附加" + note})
			continue
		}
		if runningOK && !running[strings.ToLower(e.Distro)] {
			steps = append(steps, usbPlanStep{Kind: usbStepSkip, Entry: e, BusID: dev.BusID, Distro: e.Distro, Reason: fmt.Sprintf("发行版 %s 未运行", e.Distro)})
			continue
		}
		kind := usbStepAttach
		if dev.State != usbipd.StateShared {
			kind = usbStepBind // 未共享：先提权 bind 再 attach
		}
		steps = append(steps, usbPlanStep{Kind: kind, Entry: e, BusID: dev.BusID, Distro: e.Distro, Reason: note})
	}
	return steps
}

// matchUsbDevice 账本条目 → 当前在场设备。BusID 仅表示拓扑位置，不能单独充当
// 物理身份：原端口命中也必须核验账本已有身份字段；端口不同则只接受唯一稳定身份。
func matchUsbDevice(e USBShareEntry, devices []usbipd.Device) (usbipd.Device, string, bool) {
	var sameBus *usbipd.Device
	connected := make([]usbipd.Device, 0, len(devices))
	for i := range devices {
		d := devices[i]
		if !d.Connected() {
			continue
		}
		connected = append(connected, d)
		if strings.EqualFold(d.BusID, e.BusID) {
			sameBus = &devices[i]
		}
	}

	if sameBus != nil {
		if usbIdentityMatches(e, *sameBus) {
			return *sameBus, "", true
		}
		return usbipd.Device{}, "同端口当前是其他设备，物理身份不匹配，已跳过", false
	}

	hits := make([]usbipd.Device, 0, 1)
	for _, d := range connected {
		matched := usbStableIdentityMatches(e, d)
		if !usbHasStableIdentity(e) {
			matched = e.Vid != "" && e.Pid != "" && usbVidPidMatches(e, d)
		}
		if matched {
			hits = append(hits, d)
		}
	}
	if len(hits) == 1 {
		return hits[0], fmt.Sprintf("（换插口，现总线号 %s）", hits[0].BusID), true
	}
	if len(hits) > 1 {
		return usbipd.Device{}, "检测到多个相同身份候选，无法安全确认物理设备，已跳过", false
	}
	return usbipd.Device{}, "", false
}

// usbIdentityMatches 核验原端口上的设备。新账本只接受已记录的稳定身份命中；旧账本
// 没有稳定字段时至少要求 VID/PID 相符。完全没有身份线索的最老账本不再凭 BusID 操作。
func usbIdentityMatches(e USBShareEntry, d usbipd.Device) bool {
	if usbHasStableIdentity(e) {
		return usbStableIdentityMatches(e, d)
	}
	return e.Vid != "" && e.Pid != "" && usbVidPidMatches(e, d)
}

// usbStableIdentityMatches 判断稳定身份。Serial、InstanceID、Guid 任一已记录值精确命中
// 即可，其中 Serial/Guid 还会同时核对账本已有的 VID/PID。不会退化为仅按 VID/PID。
func usbStableIdentityMatches(e USBShareEntry, d usbipd.Device) bool {
	if e.Serial != "" && d.Serial != "" && strings.EqualFold(e.Serial, d.Serial) && usbVidPidCompatible(e, d) {
		return true
	}
	if e.InstanceID != "" && d.InstanceID != "" && strings.EqualFold(e.InstanceID, d.InstanceID) {
		return true
	}
	return e.Guid != "" && d.Guid != "" && strings.EqualFold(e.Guid, d.Guid) && usbVidPidCompatible(e, d)
}

func usbHasStableIdentity(e USBShareEntry) bool {
	return e.InstanceID != "" || e.Serial != "" || e.Guid != ""
}

func usbVidPidCompatible(e USBShareEntry, d usbipd.Device) bool {
	if e.Vid != "" && !strings.EqualFold(e.Vid, d.Vid) {
		return false
	}
	if e.Pid != "" && !strings.EqualFold(e.Pid, d.Pid) {
		return false
	}
	return true
}

func usbVidPidMatches(e USBShareEntry, d usbipd.Device) bool {
	return strings.EqualFold(e.Vid, d.Vid) && strings.EqualFold(e.Pid, d.Pid)
}

// usbBindReceipt 是批量提权 bind 脚本写到标准输出的一条机器回执。
type usbBindReceipt struct {
	BusID    string
	ExitCode int
}

const usbBindReceiptPrefix = "HANXI_USB_BIND|"

func usbBindReceiptPath() string {
	return fmt.Sprintf("%s\\hanxi-usb-bind-%d-%d.log", os.TempDir(), os.Getpid(), time.Now().UnixNano())
}

// buildUsbBindBatchScript 构造一次 UAC 内逐条执行且不中断的 bind 脚本。每条命令
// 无论成败都把 busid 与退出码追加到仅后端生成的临时文件；外层脚本固定以 0 收口，
// 让所有设备都有执行机会。提权承载层不回传目标 stdout，故不能靠控制台文本传回执。
func buildUsbBindBatchScript(busIDs []string, receiptPath string) string {
	parts := []string{fmt.Sprintf("Remove-Item -LiteralPath %s -Force -ErrorAction SilentlyContinue", psQuote(receiptPath))}
	for _, busID := range busIDs {
		parts = append(parts, fmt.Sprintf(
			"& usbipd bind --busid %s; $c=$LASTEXITCODE; Add-Content -LiteralPath %s -Value ('%s%s|' + $c) -Encoding UTF8",
			busID, psQuote(receiptPath), usbBindReceiptPrefix, busID,
		))
	}
	return strings.Join(parts, "; ") + "; exit 0"
}

// parseUsbBindReceipts 只接受本模块固定前缀与白名单 busid，忽略 usbipd 自身日志。
func parseUsbBindReceipts(out string) map[string]usbBindReceipt {
	receipts := make(map[string]usbBindReceipt)
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, usbBindReceiptPrefix) {
			continue
		}
		fields := strings.Split(strings.TrimPrefix(line, usbBindReceiptPrefix), "|")
		if len(fields) != 2 {
			continue
		}
		busID, err := usbipd.CheckBusID(fields[0])
		if err != nil {
			continue
		}
		var code int
		if _, err := fmt.Sscanf(fields[1], "%d", &code); err != nil {
			continue
		}
		receipts[strings.ToLower(busID)] = usbBindReceipt{BusID: busID, ExitCode: code}
	}
	return receipts
}

func deviceSharedAtBusID(devices []usbipd.Device, busID string) bool {
	for _, d := range devices {
		if strings.EqualFold(d.BusID, busID) {
			return d.State == usbipd.StateShared || d.State == usbipd.StateAttached
		}
	}
	return false
}

// ---- 重放执行 ----

// runUsbReplay 执行一轮重放；distroFilter 空=全账本，非空=只重放该发行版的条目。
// 单飞闸：同一时刻至多一轮（后台/手动撞车时后来者快速失败，不排队挂死）。
// 任何失败都收进回执与账本状态，绝不让调用链炸掉。
func (s *WslService) runUsbReplay(ctx context.Context, distroFilter, trigger string) (OperationOutcome, error) {
	s.mu.Lock()
	if s.usbReplayBusy {
		s.mu.Unlock()
		return OperationOutcome{}, errors.New("USB 共享重放正在进行中，请稍候")
	}
	s.usbReplayBusy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.usbReplayBusy = false
		s.mu.Unlock()
	}()

	usbLedgerMu.Lock()
	defer usbLedgerMu.Unlock()

	led, err := s.usbLoad()
	if err != nil {
		return OperationOutcome{}, err
	}
	if !led.AutoEnabled && trigger != "manual" {
		return OperationOutcome{Message: "开机自动共享总开关未开，跳过重放"}, nil
	}
	if len(led.Entries) == 0 {
		return OperationOutcome{Message: "USB 共享账本是空的——先在设备表上勾选要自动共享的设备"}, nil
	}
	devices, err := s.usbRun.State(ctx)
	if err != nil {
		msg := fmt.Sprintf("重放取消：读取 usbipd 设备表失败（%v）", usbipdErrText(err))
		s.noteUsbReplay(msg)
		return OperationOutcome{Message: msg}, nil
	}
	running, runningOK := s.quietSet(ctx, "--running")
	steps := planUsbReplay(led.Entries, devices, running, runningOK)
	if distroFilter != "" {
		steps = slices.DeleteFunc(slices.Clone(steps), func(st usbPlanStep) bool {
			return !strings.EqualFold(st.Distro, distroFilter)
		})
	}

	var (
		binds         []usbPlanStep
		attaches      = make(map[string]usbPlanStep) // 条目 ID → 步骤（bind 成功后进入 attach 队列）
		done, skipped int
		lines         []string
	)
	for _, st := range steps {
		switch st.Kind {
		case usbStepSkip:
			skipped++
			s.usbSetEntryStatus(&led, st.Entry.ID, "跳过："+st.Reason)
			continue
		case usbStepBind:
			binds = append(binds, st)
		case usbStepAttach:
			attaches[st.Entry.ID] = st
		}
	}

	// 第一段：需要补绑定的设备合批提权。一次 UAC 内逐条执行、逐条输出结构化
	// 回执，不因前项失败截断后项；回执缺失时重读 state 对账，绝不凭整体成功猜测。
	if len(binds) > 0 {
		valid := make([]usbPlanStep, 0, len(binds))
		busIDs := make([]string, 0, len(binds))
		for _, b := range binds {
			id, err := usbipd.CheckBusID(b.BusID)
			if err != nil {
				s.usbSetEntryStatus(&led, b.Entry.ID, "绑定参数不合法："+err.Error())
				lines = append(lines, fmt.Sprintf("%s 绑定参数不合法：%s", b.BusID, err))
				continue
			}
			valid = append(valid, b)
			busIDs = append(busIDs, id)
		}
		if len(valid) > 0 {
			if err := ctx.Err(); err != nil {
				return OperationOutcome{}, err
			}
			receiptPath := usbBindReceiptPath()
			defer os.Remove(receiptPath)
			out, callErr := s.elevProc(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", buildUsbBindBatchScript(busIDs, receiptPath))
			receiptBytes, _ := os.ReadFile(receiptPath)
			receipts := parseUsbBindReceipts(string(receiptBytes))
			var reconciled []usbipd.Device
			missingReceipt := false
			for _, b := range valid {
				if _, ok := receipts[strings.ToLower(b.BusID)]; !ok {
					missingReceipt = true
					break
				}
			}
			if missingReceipt {
				if state, stateErr := s.usbRun.State(ctx); stateErr == nil {
					reconciled = state
				}
			}
			fallbackText := usbipdErrText(callErr)
			if callErr == nil && !out.Success {
				fallbackText = strings.TrimSpace(out.Message)
			}
			if fallbackText == "" {
				fallbackText = "提权批次未返回该设备的结构化回执"
			}
			for _, b := range valid {
				receipt, ok := receipts[strings.ToLower(b.BusID)]
				switch {
				case ok && receipt.ExitCode == 0:
					attaches[b.Entry.ID] = b
				case ok:
					text := fmt.Sprintf("usbipd bind 退出码 %d", receipt.ExitCode)
					s.usbSetEntryStatus(&led, b.Entry.ID, "绑定失败："+text)
					lines = append(lines, fmt.Sprintf("%s 绑定失败：%s", b.BusID, text))
				case deviceSharedAtBusID(reconciled, b.BusID):
					attaches[b.Entry.ID] = b
					lines = append(lines, fmt.Sprintf("%s 回执缺失，重读现态确认已共享", b.BusID))
				default:
					s.usbSetEntryStatus(&led, b.Entry.ID, "绑定未确认："+fallbackText)
					lines = append(lines, fmt.Sprintf("%s 绑定未确认：%s", b.BusID, fallbackText))
				}
			}
		}
	}

	// 第二段：attach 逐条用户态执行（顺序稳定，失败各自记因、不拦后面的设备）。
	for _, st := range steps {
		step, queued := attaches[st.Entry.ID]
		if !queued || st.Kind == usbStepSkip {
			continue
		}
		if err := ctx.Err(); err != nil {
			return OperationOutcome{}, err
		}
		if err := s.usbRun.Attach(ctx, step.Distro, step.BusID); err != nil {
			text := usbipdErrText(err)
			s.usbSetEntryStatus(&led, step.Entry.ID, "附加失败："+text)
			lines = append(lines, fmt.Sprintf("%s→%s 失败：%s", step.BusID, step.Distro, text))
			continue
		}
		done++
		status := fmt.Sprintf("已附加到 %s", step.Distro)
		if step.Reason != "" {
			status += step.Reason
		}
		s.usbSetEntryStatus(&led, step.Entry.ID, status)
	}

	if err := s.usbSave(led); err != nil {
		return OperationOutcome{}, err
	}
	summary := fmt.Sprintf("重放完成：附加 %d 台，跳过 %d 台", done, skipped)
	if len(lines) > 0 {
		summary += "（" + strings.Join(lines, "；") + "）"
	}
	s.noteUsbReplay(fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), summary))
	return OperationOutcome{Success: true, Message: summary}, nil
}

// usbSetEntryStatus 账本内单条状态落位（调用方持 usbLedgerMu）。
func (s *WslService) usbSetEntryStatus(led *usbLedger, id, status string) {
	for i := range led.Entries {
		if led.Entries[i].ID == id {
			led.Entries[i].LastStatus = status
			led.Entries[i].LastAt = time.Now().Format("2006-01-02 15:04:05")
			return
		}
	}
}

func (s *WslService) noteUsbReplay(msg string) {
	s.mu.Lock()
	s.usbReplayMsg = msg
	s.mu.Unlock()
}

// usbipdErrText 错误→白话文本（context 取消/超时也归成可读说明而非 Go 原文）。
func usbipdErrText(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "命令超时未完成"
	}
	if errors.Is(err, context.Canceled) {
		return "操作已取消"
	}
	return err.Error()
}

// ---- 异步重放调度与插拔观察 ----

// usbGenerationCurrent 是后台自动任务执行动作前的最后一道代际门。关闭总开关、
// 模块销毁或更新自动化配置都会递增 generation，使旧 goroutine 即使晚醒也不能执行。
func (s *WslService) usbGenerationCurrent(gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usbAutomationGen == gen
}

// scheduleUsbReplay 后台补打一发重放（不阻塞调用方；单飞闸兜并发）。
// explicit=true 仅用于用户显式要求的立即重放，可忽略自动化 generation；其余任务
// 都必须在等待后再次验代际，确保总开关关闭后旧任务不能复活。
func (s *WslService) scheduleUsbReplay(trigger string, delay time.Duration, distroFilter string, explicit bool) {
	s.mu.Lock()
	if s.usbReplayCancel != nil {
		s.usbReplayCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.usbReplayCancel = cancel
	gen := s.usbAutomationGen
	s.mu.Unlock()
	go func() {
		defer cancel()
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return
			}
		}
		if !explicit && !s.usbGenerationCurrent(gen) {
			return
		}
		rctx, rcancel := context.WithTimeout(ctx, 3*time.Minute)
		defer rcancel()
		if _, err := s.runUsbReplay(rctx, distroFilter, trigger); err != nil && !errors.Is(err, context.Canceled) {
			s.noteUsbReplay(fmt.Sprintf("[%s] 自动重放未完成：%s", time.Now().Format("15:04:05"), usbipdErrText(err)))
		}
	}()
}

// startUsbWatcherIfNeeded 按持久化开关与账本决定是否常驻低频 watcher。
func (s *WslService) startUsbWatcherIfNeeded() {
	usbLedgerMu.Lock()
	led, err := s.usbLoad()
	usbLedgerMu.Unlock()
	if err != nil || !led.AutoEnabled || len(led.Entries) == 0 {
		s.stopUsbWatcher()
		return
	}

	s.mu.Lock()
	if s.usbWatcherCancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.usbWatcherCancel = cancel
	gen := s.usbAutomationGen
	s.mu.Unlock()
	go s.runUsbWatcher(ctx, gen)
}

func (s *WslService) stopUsbWatcher() {
	s.mu.Lock()
	if s.usbWatcherCancel != nil {
		s.usbWatcherCancel()
		s.usbWatcherCancel = nil
	}
	s.mu.Unlock()
}

// usbPresentSet 只标记当前设备表里能安全命中启用账本条目的在场设备。
func usbPresentSet(entries []USBShareEntry, devices []usbipd.Device) map[string]bool {
	present := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		if d, _, ok := matchUsbDevice(e, devices); ok && d.Connected() {
			present[e.ID] = true
		}
	}
	return present
}

func (s *WslService) usbWatcherSnapshot(ctx context.Context) (map[string]bool, bool) {
	usbLedgerMu.Lock()
	led, err := s.usbLoad()
	usbLedgerMu.Unlock()
	if err != nil || !led.AutoEnabled || len(led.Entries) == 0 {
		return nil, false
	}
	devices, err := s.usbRun.State(ctx)
	if err != nil {
		return nil, true // 采样失败不伪造 absent 边沿，保留上一帧
	}
	return usbPresentSet(led.Entries, devices), true
}

// runUsbWatcher 对 absent→present 做双阶段去抖：首见只记候选，持续满窗口且仍在场
// 才调度全账本重放。设备再次 absent 后才能形成下一条边沿。
func (s *WslService) runUsbWatcher(ctx context.Context, gen uint64) {
	defer func() {
		s.mu.Lock()
		if s.usbAutomationGen == gen {
			s.usbWatcherCancel = nil
		}
		s.mu.Unlock()
	}()

	previous, keep := s.usbWatcherSnapshot(ctx)
	if !keep {
		return
	}
	candidates := make(map[string]time.Time)
	ticker := time.NewTicker(usbWatcherInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if !s.usbGenerationCurrent(gen) {
				return
			}
			current, keep := s.usbWatcherSnapshot(ctx)
			if !keep {
				return
			}
			if current == nil { // state 采样失败
				continue
			}
			trigger := false
			for id := range current {
				if since, ok := candidates[id]; ok {
					if now.Sub(since) >= usbWatcherDebounce {
						trigger = true
						delete(candidates, id)
					}
				} else if !previous[id] {
					candidates[id] = now
				}
			}
			for id := range candidates {
				if !current[id] {
					delete(candidates, id)
				}
			}
			if trigger {
				s.scheduleUsbReplay("watcher", 0, "", false)
			}
			previous = current
		}
	}
}

// cancelUsbAutomation 收回等待/在飞自动重放与 watcher，并先递增 generation 防旧任务
// 在取消信号到达前越过边界。显式 ReplayUsbNow 不使用这里的上下文。
func (s *WslService) cancelUsbAutomation() {
	s.mu.Lock()
	s.usbAutomationGen++
	if s.usbReplayCancel != nil {
		s.usbReplayCancel()
		s.usbReplayCancel = nil
	}
	if s.usbWatcherCancel != nil {
		s.usbWatcherCancel()
		s.usbWatcherCancel = nil
	}
	s.mu.Unlock()
}

// ---- Wails 绑定方法（参数白名单化，前端零拼接面） ----

// GetUsbOverview "USB 直通"页总取数：usbipd 存在性 + 设备表 + 账本 + 开关态。
// 未安装不是错误（installed=false，前端渲染引导卡）；设备表拉取失败如实进 error。
func (s *WslService) GetUsbOverview() (UsbView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	view := UsbView{ReleasesPage: usbipdReleasesURL, Devices: []usbipd.Device{}}
	led, lerr := s.usbLoad()
	if lerr != nil {
		view.Error = lerr.Error()
	}
	view.Ledger = led.Entries
	view.AutoEnabled = led.AutoEnabled
	if ver, err := s.usbRun.Version(ctx); err != nil {
		if errors.Is(err, usbipd.ErrNotInstalled) {
			view.Installed = false
		} else {
			view.Installed = true
			view.Error = fmt.Sprintf("usbipd 探测失败: %s", usbipdErrText(err))
		}
	} else {
		view.Installed = true
		view.Version = ver
	}
	if view.Installed {
		devs, err := s.usbRun.State(ctx)
		if err != nil {
			view.Error = fmt.Sprintf("读取 usbipd 设备表失败: %s", usbipdErrText(err))
		} else {
			view.Devices = devs
		}
	}
	s.mu.Lock()
	view.ReplayBusy = s.usbReplayBusy
	view.LastReplay = s.usbReplayMsg
	s.mu.Unlock()
	if lerr != nil {
		return view, lerr // 账本损坏仍回载荷（引导修复文案在 error 上），同时报错拦写
	}
	return view, nil
}

// OpenUsbipdReleases 打开 usbipd-win 官方发布页（固定地址白名单）。
func (s *WslService) OpenUsbipdReleases() error {
	return s.opener.OpenURL(usbipdReleasesURL)
}

// BindUsbDevice 共享设备（bind，提权；--force 留作真机验证后的进阶口，UI 暂不放）。
func (s *WslService) BindUsbDevice(busID string, force bool) (OperationOutcome, error) {
	args, err := usbipd.BindArgs(busID, force)
	if err != nil {
		return OperationOutcome{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return s.elevateUsb(ctx, args...)
}

// UnbindUsbDevice 取消共享（在场设备按 busid，提权）。
func (s *WslService) UnbindUsbDevice(busID string) (OperationOutcome, error) {
	args, err := usbipd.UnbindArgs(busID)
	if err != nil {
		return OperationOutcome{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return s.elevateUsb(ctx, args...)
}

// UnbindAbsentUsbDevice 取消共享（"已共享但不在场"的设备按 GUID，提权）。
func (s *WslService) UnbindAbsentUsbDevice(guid string) (OperationOutcome, error) {
	args, err := usbipd.UnbindGUIDArgs(guid)
	if err != nil {
		return OperationOutcome{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return s.elevateUsb(ctx, args...)
}

// elevateUsb usbipd 白名单提权通道：参数全部出自 usbipd 包的正则白名单构造。
func (s *WslService) elevateUsb(ctx context.Context, args ...string) (OperationOutcome, error) {
	out, err := s.elevProc(ctx, "usbipd.exe", args...)
	if err != nil {
		return OperationOutcome{}, err
	}
	if !out.Success {
		return out, nil // UAC 取消等：如实回执不报错
	}
	return out, nil
}

// AttachUsbDevice 附加设备到发行版（用户态）。发行版名先过 `wsl -l -q` 实时
// 白名单（InstallDistro 同纪律）；未运行的发行版顺手拉起（OpenDistroFolder 同
// 先例——attach 的硬前提是实例在跑，用户点名要挂给它，拉起在意图之内）。
func (s *WslService) AttachUsbDevice(busID, distro string) (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	distro = strings.TrimSpace(distro)
	if err := s.distroAllowed(ctx, distro); err != nil {
		return OperationOutcome{}, err
	}
	lifted := ""
	if set, ok := s.quietSet(ctx, "--running"); ok && !set[strings.ToLower(distro)] {
		if out, err := s.runWsl(ctx, "-d", distro, "--exec", "/bin/echo", "hanxi-usb-lift"); err != nil {
			return OperationOutcome{}, fmt.Errorf("无法启动 %s，附加取消: %w %s", distro, err, strings.TrimSpace(out))
		}
		lifted = "（发行版原为停止，已顺手拉起）"
	}
	if err := s.usbRun.Attach(ctx, distro, busID); err != nil {
		return OperationOutcome{}, fmt.Errorf("附加失败: %s", usbipdErrText(err))
	}
	return OperationOutcome{Success: true, Message: fmt.Sprintf("设备 %s 已附加到 %s%s", busID, distro, lifted)}, nil
}

// DetachUsbDevice 从客户端卸下（用户态）。
func (s *WslService) DetachUsbDevice(busID string) (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := s.usbRun.Detach(ctx, busID); err != nil {
		return OperationOutcome{}, fmt.Errorf("卸下失败: %s", usbipdErrText(err))
	}
	return OperationOutcome{Success: true, Message: fmt.Sprintf("设备 %s 已从 WSL 卸下（回到 Windows）", busID)}, nil
}

// SetUsbAutoAttach 开机自动共享总开关（默认关；打开即补打一发重放）。
func (s *WslService) SetUsbAutoAttach(enabled bool) (OperationOutcome, error) {
	if !usbLedgerMu.TryLock() {
		return OperationOutcome{}, errUsbLedgerBusy
	}
	led, err := s.usbLoad()
	if err != nil {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, err
	}
	led.AutoEnabled = enabled
	if err := s.usbSave(led); err != nil {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, err
	}
	usbLedgerMu.Unlock()
	if enabled {
		s.cancelUsbAutomation()
		s.startUsbWatcherIfNeeded()
		s.scheduleUsbReplay("enabled", 0, "", false)
		return OperationOutcome{Success: true, Message: "已开启开机自动共享，并立即按账本补打一发（结果见账本行内状态）"}, nil
	}
	s.cancelUsbAutomation()
	return OperationOutcome{Success: true, Message: "已关闭开机自动共享；等待中与在飞的自动任务已取消（账本保留，显式立即重放仍可用）"}, nil
}

// SetUsbShare 登记/更新"设备 → 发行版"的自动共享条目（以 busid 一设备一条）。
// 设备名/VID/PID 取自当前设备表快照（供换插口回落匹配）；此刻不在场的设备拒绝
// 登记（没有 busid 快照与匹配线索，登记了也重放不动）。
func (s *WslService) SetUsbShare(busID, distro string) (OperationOutcome, error) {
	id, err := usbipd.CheckBusID(busID)
	if err != nil {
		return OperationOutcome{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	distro = strings.TrimSpace(distro)
	if err := s.distroAllowed(ctx, distro); err != nil {
		return OperationOutcome{}, err
	}
	devices, err := s.usbRun.State(ctx)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("读取 usbipd 设备表失败: %s", usbipdErrText(err))
	}
	var found *usbipd.Device
	for i := range devices {
		if devices[i].Connected() && strings.EqualFold(devices[i].BusID, id) {
			found = &devices[i]
			break
		}
	}
	if found == nil {
		return OperationOutcome{}, fmt.Errorf("设备 %s 此刻不在场（或总线号已变化），请把设备插上再来登记", id)
	}
	if !usbLedgerMu.TryLock() {
		return OperationOutcome{}, errUsbLedgerBusy
	}
	led, err := s.usbLoad()
	if err != nil {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, err
	}
	idx := slices.IndexFunc(led.Entries, func(e USBShareEntry) bool {
		return strings.EqualFold(e.BusID, id)
	})
	entry := USBShareEntry{
		ID:          fmt.Sprintf("usb-%d", time.Now().UnixNano()),
		BusID:       found.BusID,
		InstanceID:  found.InstanceID,
		Vid:         found.Vid,
		Pid:         found.Pid,
		Serial:      found.Serial,
		Guid:        found.Guid,
		Description: found.Description,
		Distro:      distro,
		Enabled:     true,
		AddedAt:     time.Now().Format("2006-01-02 15:04:05"),
	}
	existed := idx >= 0
	if existed {
		entry.ID = led.Entries[idx].ID // 改绑目标发行版不换身份
		led.Entries[idx] = entry
	} else {
		led.Entries = append(led.Entries, entry)
	}
	if err := s.usbSave(led); err != nil {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, err
	}
	usbLedgerMu.Unlock()
	s.startUsbWatcherIfNeeded()
	if existed {
		return OperationOutcome{Success: true, Message: fmt.Sprintf("设备 %s（%s）的自动共享目标已改为 %s", entry.BusID, entry.Description, distro)}, nil
	}
	return OperationOutcome{Success: true, Message: fmt.Sprintf("已登记：%s（%s）开机自动共享给 %s", entry.BusID, entry.Description, distro)}, nil
}

// SetUsbShareEnabled 勾选/停用单条账本（"可勾选停用"卡片要求）。
func (s *WslService) SetUsbShareEnabled(id string, enabled bool) (OperationOutcome, error) {
	return s.usbPatchEntry(id, func(e *USBShareEntry) { e.Enabled = enabled })
}

// RemoveUsbShare 删除账本条目（不触碰 usbipd 的系统绑定）。
func (s *WslService) RemoveUsbShare(id string) (OperationOutcome, error) {
	if !usbLedgerMu.TryLock() {
		return OperationOutcome{}, errUsbLedgerBusy
	}
	led, err := s.usbLoad()
	if err != nil {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, err
	}
	idx := slices.IndexFunc(led.Entries, func(e USBShareEntry) bool { return e.ID == id })
	if idx < 0 {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, fmt.Errorf("账本条目 %s 不存在（可能已被删除）", id)
	}
	removed := led.Entries[idx]
	led.Entries = slices.Delete(led.Entries, idx, idx+1)
	if err := s.usbSave(led); err != nil {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, err
	}
	usbLedgerMu.Unlock()
	s.startUsbWatcherIfNeeded()
	return OperationOutcome{Success: true, Message: fmt.Sprintf("已移除 %s（%s）的自动共享登记；usbipd 的系统绑定未动，需要退绑请点该设备的「取消共享」", removed.BusID, removed.Description)}, nil
}

func (s *WslService) usbPatchEntry(id string, patch func(*USBShareEntry)) (OperationOutcome, error) {
	if !usbLedgerMu.TryLock() {
		return OperationOutcome{}, errUsbLedgerBusy
	}
	led, err := s.usbLoad()
	if err != nil {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, err
	}
	idx := slices.IndexFunc(led.Entries, func(e USBShareEntry) bool { return e.ID == id })
	if idx < 0 {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, fmt.Errorf("账本条目 %s 不存在（可能已被删除）", id)
	}
	patch(&led.Entries[idx])
	if err := s.usbSave(led); err != nil {
		usbLedgerMu.Unlock()
		return OperationOutcome{}, err
	}
	usbLedgerMu.Unlock()
	s.startUsbWatcherIfNeeded()
	return OperationOutcome{Success: true, Message: "账本已更新"}, nil
}

// ReplayUsbNow 手动补打一发重放（忽略总开关——手动点击即显式意图）。
func (s *WslService) ReplayUsbNow() (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return s.runUsbReplay(ctx, "", "manual")
}

// ---- 供 module.go 接线的小件 ----

// scheduleUsbStartupReplay 模块激活时（hanxi 启动预激活）补打启动重放。
func (s *WslService) scheduleUsbStartupReplay() {
	s.startUsbWatcherIfNeeded()
	s.scheduleUsbReplay("startup", usbStartupReplayDelay, "", false)
}
