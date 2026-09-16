package wsl

// NAT 模式端口转发管理（netsh portproxy）：
// WSL2 NAT 下 Windows 本机访问不到发行版内监听的服务，正规解法是
// `netsh interface portproxy add v4tov4`；WSL 重启后 guest IP 会漂移，
// 因此"应用规则"必须能一键重同步。
//
// 边界与红线：
//   - 规则持久化在 Hanxi 自有数据文件；netsh 现态里非本工具登记的转发一律如实
//     展示但绝不触碰（Foreign 清单）——增删只围绕自己账本内的监听口；
//   - 一切系统变更走同一个提权批量脚本（单 UAC），netsh 参数全部由后端从
//     白名单校验（端口数字/IPv4 点分十进制）拼装，前端零拼接面；
//   - 防火墙规则用固定命名前缀 "Hanxi WSL " + 端口，先删后加幂等；
//   - 账本文件的"读-改-写"全程经 ppLedgerMu 串行（含标脏/清脏），并发增删改互不覆盖；
//     账本被应用流程持有时增删改快速失败，绝不在锁上排队挂死前端；
//   - 停机的发行版不会被转发流程顺手拉起（IP 拿不到即跳过并如实上报）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PortRule 一条端口转发规则（Hanxi 账本内的期望态）。
type PortRule struct {
	ID       string `json:"id"`
	Distro   string `json:"distro"`
	Port     int    `json:"port"`     // 本机监听端口（TCP）
	Guest    int    `json:"guest"`    // guest 目标端口（0=与 Port 相同）
	Listen   string `json:"listen"`   // 本机监听地址（0.0.0.0 或具体 IPv4）
	Firewall bool   `json:"firewall"` // 同步防火墙入站放行
	Note     string `json:"note,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// ActiveProxy netsh 现态的一条转发。
type ActiveProxy struct {
	ListenAddr  string `json:"listenAddr"`
	ListenPort  int    `json:"listenPort"`
	ConnectAddr string `json:"connectAddr"`
	ConnectPort int    `json:"connectPort"`
}

// PortRuleView 规则 + 现态对照（前端呈现"已应用/IP 漂移/待应用"）。
type PortRuleView struct {
	PortRule
	Applied   bool   `json:"applied"`
	ActiveIP  string `json:"activeIP,omitempty"` // netsh 当前指向
	TargetIP  string `json:"targetIP,omitempty"` // 解析出的发行版现 IP（漂移对比）
	DistroRun bool   `json:"distroRunning"`
}

// PortProxyView 端口转发总览载荷。
type PortProxyView struct {
	Rules       []PortRuleView `json:"rules"`
	Foreign     []ActiveProxy  `json:"foreign"`     // 非本工具登记的现态转发（只展示不管理）
	Pending     bool           `json:"pending"`     // 账本有改动未应用
	NetworkMode string         `json:"networkMode"` // 宿主网络模式；mirrored 下本机 localhost 直通，通常无需转发
}

var ppIPv4Re = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)

func validListenAddr(s string) bool {
	if !ppIPv4Re.MatchString(s) {
		return false
	}
	for p := range strings.SplitSeq(s, ".") {
		if n, err := strconv.Atoi(p); err != nil || n > 255 {
			return false
		}
	}
	return true
}

// ppShowScript 只读导出 netsh portproxy 现态（UTF-8 包装防中文表头乱码）。
const ppShowScript = `$OutputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; netsh interface portproxy show all`

// netshShowAll 包级变量：单测替换（返回 netsh 原文）。
var netshShowAll = func(ctx context.Context) (string, error) {
	return runLocalPS(ctx, ppShowScript)
}

// parsePortproxyShow 解析 show all 数据行：每行 4 列（监听地址 端口 连接地址 端口）。
// 表头/分隔线/空行按"前两列必须是 IPv4+数字"的结构约束天然过滤。
func parsePortproxyShow(out string) []ActiveProxy {
	var list []ActiveProxy
	for raw := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(raw)
		if len(fields) != 4 {
			continue
		}
		lp, err1 := strconv.Atoi(fields[1])
		cp, err2 := strconv.Atoi(fields[3])
		if err1 != nil || err2 != nil || lp < 1 || lp > 65535 || cp < 1 || cp > 65535 {
			continue
		}
		if !ppIPv4Re.MatchString(fields[0]) || !ppIPv4Re.MatchString(fields[2]) {
			continue
		}
		list = append(list, ActiveProxy{ListenAddr: fields[0], ListenPort: lp, ConnectAddr: fields[2], ConnectPort: cp})
	}
	return list
}

// ---- 持久化 ----

// ppLedgerMu 串行化账本的全部"读 → 改 → 写 → 标脏"路径：并发 AddPortRule/UpdatePortRule/
// RemovePortRule 各自读到同一份旧快照，后写者会把前者刚落的改动整体覆盖
// （表现为"加了一条规则，另一条莫名消失"）；账本是小 JSON 文件，串行代价可忽略。
// 锁序纪律：ppLedgerMu → s.mu（markPending/clearPending 内部取 s.mu），反向持锁禁止。
var ppLedgerMu sync.Mutex

// errLedgerBusy 账本被应用/清理流程持有时的快速失败信号：与 tryBegin* 一族同风格——
// 忙则如实拒绝而非排队挂死（Apply 在等 UAC 时可挂数分钟，绝不能让前端 RPC 无声悬停）。
var errLedgerBusy = errors.New("端口转发账本正被占用（增删改或应用流程在飞），请稍候再试")

func (s *WslService) ppLoad() ([]PortRule, error) {
	if s.ppPath == "" {
		return nil, fmt.Errorf("数据存储路径不可用，无法管理端口转发规则")
	}
	data, err := os.ReadFile(s.ppPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取端口转发规则失败: %w", err)
	}
	var store struct {
		Rules []PortRule `json:"rules"`
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("端口转发规则文件损坏（%s），已拒绝覆盖写入——请人工修复或删除该文件: %w", s.ppPath, err)
	}
	return store.Rules, nil
}

func (s *WslService) ppSave(rules []PortRule) error {
	if s.ppPath == "" {
		return fmt.Errorf("数据存储路径不可用，无法保存端口转发规则")
	}
	if err := os.MkdirAll(filepath.Dir(s.ppPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Rules []PortRule `json:"rules"`
	}{rules}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.ppPath), ".wsl-portproxy-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.ppPath)
}

// ---- 校验与查询 ----

func (s *WslService) validateRule(ctx context.Context, r PortRule) (PortRule, error) {
	if r.Port < 1 || r.Port > 65535 {
		return r, fmt.Errorf("监听端口 %d 不在 1-65535", r.Port)
	}
	if r.Guest == 0 {
		r.Guest = r.Port
	}
	if r.Guest < 1 || r.Guest > 65535 {
		return r, fmt.Errorf("guest 端口 %d 不在 1-65535", r.Guest)
	}
	if r.Listen == "" {
		r.Listen = "0.0.0.0"
	}
	if !validListenAddr(r.Listen) {
		return r, fmt.Errorf("监听地址 %q 不是合法 IPv4（0.0.0.0 表示所有网卡）", r.Listen)
	}
	if err := s.distroAllowed(ctx, r.Distro); err != nil {
		return r, err
	}
	if len(r.Note) > 120 {
		r.Note = r.Note[:120]
	}
	return r, nil
}

// ListPortRules 总览：账本规则逐条对照 netsh 现态 + 发行版运行态/IP 漂移。
func (s *WslService) ListPortRules() (PortProxyView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rules, err := s.ppLoad()
	if err != nil {
		return PortProxyView{}, err
	}
	active, err := netshShowAll(ctx)
	if err != nil {
		return PortProxyView{}, fmt.Errorf("读取系统端口转发现态失败: %w", err)
	}
	entries := parsePortproxyShow(active)
	runningSet, _ := s.quietSet(ctx, "--running")

	own := func(e ActiveProxy) bool {
		return slices.ContainsFunc(rules, func(r PortRule) bool {
			return strings.EqualFold(r.Listen, e.ListenAddr) && r.Port == e.ListenPort
		})
	}

	// guest IP 并行解析（每规则一次 wsl -d 探测，串行会拖开页速度；
	// 同一发行版只探一次，已停/已禁用不探——探测虽经 hostname 但也要求实例在跑）。
	needIP := map[string]string{} // 小写键 → 原名（guest 调用保持原名 casing）
	for _, r := range rules {
		if r.Enabled && runningSet[strings.ToLower(r.Distro)] {
			needIP[strings.ToLower(r.Distro)] = r.Distro
		}
	}
	var (
		ipMu  sync.Mutex
		ipMap = map[string]string{}
		wg    sync.WaitGroup
	)
	for key, distro := range needIP {
		wg.Go(func() {
			if ip, err := s.guestIPv4(ctx, distro); err == nil {
				ipMu.Lock()
				ipMap[key] = ip
				ipMu.Unlock()
			}
		})
	}
	wg.Wait()

	view := PortProxyView{NetworkMode: hostNetworkMode()}
	for _, r := range rules {
		pv := PortRuleView{PortRule: r, DistroRun: runningSet[strings.ToLower(r.Distro)]}
		for _, e := range entries {
			if strings.EqualFold(e.ListenAddr, r.Listen) && e.ListenPort == r.Port {
				pv.Applied, pv.ActiveIP = true, e.ConnectAddr
				break
			}
		}
		pv.TargetIP = ipMap[strings.ToLower(r.Distro)]
		view.Rules = append(view.Rules, pv)
	}
	for _, e := range entries {
		if !own(e) {
			view.Foreign = append(view.Foreign, e)
		}
	}
	s.mu.Lock()
	view.Pending = s.ppPending
	s.mu.Unlock()
	return view, nil
}

// AddPortRule 新增规则（只写账本；生效要点「应用」）。
func (s *WslService) AddPortRule(distro string, port, guest int, listen string, firewall bool, note string) (PortRuleView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, err := s.validateRule(ctx, PortRule{
		ID: fmt.Sprintf("pp-%d", time.Now().UnixNano()), Distro: strings.TrimSpace(distro),
		Port: port, Guest: guest, Listen: strings.TrimSpace(listen),
		Firewall: firewall, Note: strings.TrimSpace(note), Enabled: true,
	})
	if err != nil {
		return PortRuleView{}, err
	}
	if !ppLedgerMu.TryLock() {
		return PortRuleView{}, errLedgerBusy
	}
	defer ppLedgerMu.Unlock()
	rules, err := s.ppLoad()
	if err != nil {
		return PortRuleView{}, err
	}
	for _, old := range rules {
		if old.Port == r.Port && strings.EqualFold(old.Listen, r.Listen) {
			return PortRuleView{}, fmt.Errorf("监听 %s:%d 已有规则（%s），请先修改或删除它", r.Listen, r.Port, old.Distro)
		}
	}
	rules = append(rules, r)
	if err := s.ppSave(rules); err != nil {
		return PortRuleView{}, err
	}
	s.markPending()
	return PortRuleView{PortRule: r}, nil
}

// UpdatePortRule 按 ID 整条替换（改端口/防火墙开关/启停/备注）。
func (s *WslService) UpdatePortRule(rule PortRule) (PortRuleView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if !ppLedgerMu.TryLock() {
		return PortRuleView{}, errLedgerBusy
	}
	defer ppLedgerMu.Unlock()
	rules, err := s.ppLoad()
	if err != nil {
		return PortRuleView{}, err
	}
	idx := slices.IndexFunc(rules, func(x PortRule) bool { return x.ID == rule.ID })
	if idx < 0 {
		return PortRuleView{}, fmt.Errorf("规则 %s 不存在（可能已被删除）", rule.ID)
	}
	r, err := s.validateRule(ctx, rule)
	if err != nil {
		return PortRuleView{}, err
	}
	for i, old := range rules {
		if i != idx && old.Port == r.Port && strings.EqualFold(old.Listen, r.Listen) {
			return PortRuleView{}, fmt.Errorf("监听 %s:%d 已被另一条规则占用", r.Listen, r.Port)
		}
	}
	rules[idx] = r
	if err := s.ppSave(rules); err != nil {
		return PortRuleView{}, err
	}
	s.markPending()
	return PortRuleView{PortRule: r}, nil
}

// RemovePortRule 删除账本规则；若系统里还有对应转发，提醒去「应用」摘除。
func (s *WslService) RemovePortRule(id string) (OperationOutcome, error) {
	if !ppLedgerMu.TryLock() {
		return OperationOutcome{}, errLedgerBusy
	}
	defer ppLedgerMu.Unlock()
	rules, err := s.ppLoad()
	if err != nil {
		return OperationOutcome{}, err
	}
	idx := slices.IndexFunc(rules, func(x PortRule) bool { return x.ID == id })
	if idx < 0 {
		return OperationOutcome{}, fmt.Errorf("规则 %s 不存在（可能已被删除）", id)
	}
	removed := rules[idx]
	rules = slices.Delete(rules, idx, idx+1)
	if err := s.ppSave(rules); err != nil {
		return OperationOutcome{}, err
	}
	s.markPending()
	return OperationOutcome{Success: true, Message: fmt.Sprintf("已删除规则 %s:%d → %s；若它在系统里已生效，点「应用规则」即可一并摘除", removed.Listen, removed.Port, removed.Distro)}, nil
}

func (s *WslService) markPending() {
	s.mu.Lock()
	s.ppPending = true
	s.mu.Unlock()
}

// ---- 应用（唯一提权面） ----

// ApplyPortRules 把账本期望态同步到系统：目标解析 → 组装 netsh 批量脚本 →
// 单次 UAC 执行 → 退出码如实上报。停机/无 IP 的发行版跳过并在回执点名。
func (s *WslService) ApplyPortRules() (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// 挂重操作闸：netsh 批量与迁移/克隆/瘦身互斥，避免"边搬盘边改转发"。
	finish, ok := s.tryBeginHeavyOp()
	if !ok {
		return OperationOutcome{}, errDistroBusy
	}
	defer finish()
	// 账本锁全程持有：本方法"读账本 → 改系统 → 清待应用标记"是一整条读-改-写链，
	// 中途放进来的增删改会让刚清掉的 pending 标记与系统现态再次错位（宁可让前端
	// 收到"正在应用中"的快速失败，也不制造"改了却没生效"的假象）。
	if !ppLedgerMu.TryLock() {
		return OperationOutcome{}, errLedgerBusy
	}
	defer ppLedgerMu.Unlock()
	rules, err := s.ppLoad()
	if err != nil {
		return OperationOutcome{}, err
	}
	active, err := netshShowAll(ctx)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("读取系统端口转发现态失败: %w", err)
	}
	entries := parsePortproxyShow(active)

	type target struct {
		rule PortRule
		ip   string
	}
	var (
		wants   []target
		skipped []string
	)
	runningSet, _ := s.quietSet(ctx, "--running")
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if !runningSet[strings.ToLower(r.Distro)] {
			skipped = append(skipped, fmt.Sprintf("%s:%d(%s 未运行)", r.Listen, r.Port, r.Distro))
			continue
		}
		ip, err := s.guestIPv4(ctx, r.Distro)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s:%d(%s 未取到 IP)", r.Listen, r.Port, r.Distro))
			continue
		}
		wants = append(wants, target{r, ip})
	}

	var cmds []string
	var added, removed []string
	upsert := func(t target) {
		r := t.rule
		// 先删后加保幂等（IP 漂移时这一步就是"重同步"的本体）。
		cmds = append(cmds,
			fmt.Sprintf("netsh interface portproxy delete v4tov4 listenaddress=%s listenport=%d", r.Listen, r.Port),
			fmt.Sprintf("netsh interface portproxy add v4tov4 listenaddress=%s listenport=%d connectaddress=%s connectport=%d", r.Listen, r.Port, t.ip, r.Guest),
		)
		if r.Firewall {
			cmds = append(cmds,
				fmt.Sprintf("netsh advfirewall firewall delete rule name=%s", psQuote(ppFWName(r.Port))),
				fmt.Sprintf("netsh advfirewall firewall add rule name=%s dir=in action=allow protocol=TCP localport=%d", psQuote(ppFWName(r.Port)), r.Port),
			)
		}
	}
	for _, t := range wants {
		r := t.rule
		cur, found := findEntry(entries, r.Listen, r.Port)
		if found && cur.ConnectAddr == t.ip && cur.ConnectPort == r.Guest {
			continue // 现态已一致，不折腾
		}
		upsert(t)
		added = append(added, fmt.Sprintf("%s:%d→%s:%d", r.Listen, r.Port, t.ip, r.Guest))
	}
	for _, e := range entries {
		if !slices.ContainsFunc(rules, func(r PortRule) bool {
			return strings.EqualFold(r.Listen, e.ListenAddr) && r.Port == e.ListenPort
		}) {
			continue // 非本工具账本内监听：不碰
		}
		if slices.ContainsFunc(wants, func(t target) bool {
			return strings.EqualFold(t.rule.Listen, e.ListenAddr) && t.rule.Port == e.ListenPort
		}) {
			continue
		}
		cmds = append(cmds, fmt.Sprintf("netsh interface portproxy delete v4tov4 listenaddress=%s listenport=%d", e.ListenAddr, e.ListenPort))
		removed = append(removed, fmt.Sprintf("%s:%d", e.ListenAddr, e.ListenPort))
	}

	if len(cmds) == 0 {
		s.clearPending()
		msg := "系统端口转发已与账本一致，无需变更"
		if len(skipped) > 0 {
			msg += "；跳过的规则：" + strings.Join(skipped, "、") + "（启动发行版后再来应用）"
		}
		return OperationOutcome{Success: true, Message: msg}, nil
	}

	// 逐条传播退出码（#37 红线：netsh 半途失败不得谎报整体成功）。
	script := strings.Join(cmds, "; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; ") + "; exit 0"
	out, err := s.elevProc(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("应用端口转发规则失败: %w", err)
	}
	if !out.Success {
		return out, nil // UAC 取消：如实回执
	}
	s.clearPending()
	parts := []string{}
	if len(added) > 0 {
		parts = append(parts, fmt.Sprintf("生效 %d 条（%s）", len(added), strings.Join(added, "、")))
	}
	if len(removed) > 0 {
		parts = append(parts, fmt.Sprintf("摘除 %d 条（%s）", len(removed), strings.Join(removed, "、")))
	}
	if len(skipped) > 0 {
		parts = append(parts, "跳过 "+strings.Join(skipped, "、"))
	}
	if len(parts) == 0 {
		parts = append(parts, "无变更")
	}
	return OperationOutcome{Success: true, Message: "端口转发已同步：" + strings.Join(parts, "；") + "。注意：WSL 重启后 guest IP 会漂移，届时再点一次应用即可重同步"}, nil
}

func (s *WslService) clearPending() {
	s.mu.Lock()
	s.ppPending = false
	s.mu.Unlock()
}

// ClearPortLedgerFile 账本损坏/误拦时的逃生口：只删账本文件，不碰系统
// 现态（其中原属本工具的转发会转列为"外部转发"，需要摘除时手工 netsh delete）。
func (s *WslService) ClearPortLedgerFile() (OperationOutcome, error) {
	if s.ppPath == "" {
		return OperationOutcome{}, errors.New("数据存储路径不可用")
	}
	// 与账本读-改-写同锁：应用流程在飞时删文件，会让它按旧快照清掉一个"本不该清"的 pending
	if !ppLedgerMu.TryLock() {
		return OperationOutcome{}, errLedgerBusy
	}
	defer ppLedgerMu.Unlock()
	if err := os.Remove(s.ppPath); err != nil && !os.IsNotExist(err) {
		return OperationOutcome{}, fmt.Errorf("清空账本文件失败: %w", err)
	}
	s.clearPending()
	return OperationOutcome{Success: true, Message: "账本文件已清空；系统里的原转发转为「外部转发」列示，本工具不再认领它们"}, nil
}

// CleanupPortRules 一键清理：摘除账本全部规则的系统转发与防火墙规则并清空账本。
func (s *WslService) CleanupPortRules() (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	finish, ok := s.tryBeginHeavyOp()
	if !ok {
		return OperationOutcome{}, errDistroBusy
	}
	defer finish()
	if !ppLedgerMu.TryLock() {
		return OperationOutcome{}, errLedgerBusy
	}
	defer ppLedgerMu.Unlock()
	rules, err := s.ppLoad()
	if err != nil {
		return OperationOutcome{}, err
	}
	if len(rules) == 0 {
		return OperationOutcome{Success: true, Message: "账本里没有规则，无需清理"}, nil
	}
	var cmds []string
	for _, r := range rules {
		cmds = append(cmds,
			fmt.Sprintf("netsh interface portproxy delete v4tov4 listenaddress=%s listenport=%d", r.Listen, r.Port),
			fmt.Sprintf("netsh advfirewall firewall delete rule name=%s", psQuote(ppFWName(r.Port))),
		)
	}
	script := strings.Join(cmds, "; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; ") + "; exit 0"
	out, err := s.elevProc(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("清理端口转发失败: %w", err)
	}
	if !out.Success {
		return out, nil
	}
	if err := s.ppSave(nil); err != nil {
		return OperationOutcome{}, err
	}
	s.clearPending()
	return OperationOutcome{Success: true, Message: fmt.Sprintf("已清理 %d 条规则的系统转发与防火墙放行（netsh delete 对不存在的条目按成功处理）", len(rules))}, nil
}

// guestIPv4 解析发行版 NAT IPv4（复用取证通道；未运行返回错误）。
func (s *WslService) guestIPv4(ctx context.Context, name string) (string, error) {
	gctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := s.runWsl(gctx, "-d", name, "--", "hostname", "-I")
	if err != nil {
		return "", err
	}
	if ip := firstIPv4(out); ip != "" {
		return ip, nil
	}
	if out, err := s.runWsl(gctx, "-d", name, "--", "ip", "-4", "-o", "addr", "show", "scope", "global"); err == nil {
		if ip := firstIPv4(out); ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("未取到 %s 的 IPv4", name)
}

func ppFWName(port int) string { return "Hanxi WSL " + strconv.Itoa(port) }

func findEntry(entries []ActiveProxy, listen string, port int) (ActiveProxy, bool) {
	for _, e := range entries {
		if e.ListenPort == port && strings.EqualFold(e.ListenAddr, listen) {
			return e, true
		}
	}
	return ActiveProxy{}, false
}
