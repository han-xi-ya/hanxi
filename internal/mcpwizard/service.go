package mcpwizard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"hanxi/internal/settings"
)

// McpWizardService 是「AI 接入」分区的 Wails 绑定服务（装配根直挂，先例 snapshot）。
// 无常驻业务状态：每次配置方法调用即时读盘分析，天然与外部工具并发改配置对齐。
// 唯一的例外是安装前自检（selfcheck.go）的短 TTL 结果缓存与串行锁——它只是
// 一次性握手的新鲜度备忘，不持有任何文件句柄或后台协程。
type McpWizardService struct {
	receiptPath string // <DataDir>/mcp/install.json（本包所有）
	accessPath  string // <DataDir>/mcp/access.json（读方契约归 F4a；写引擎见 access_write.go，R6）
	home        string
	env         func(string) string
	command     string // hanxi exe 绝对路径（os.Executable，写入配置的 command 值）
	writeFn     func(path string, data []byte) error
	// afterWriteCheck 是仅供回归测试放大“最终条件检查→原子替换”窗口的钩子。
	// 生产恒为 nil；钩子返回后 writeIfUnchanged 会再次检查 expected，故外部改写不会被覆盖。
	afterWriteCheck func(path string)
	now             func() time.Time

	// —— 安装前自检（PLAN §2.5，R2）——
	checkMu      sync.Mutex    // 串行化自检：同刻至多一个握手子进程
	probe        probeRunner   // 注入缝：生产=spawnProbe，测试=剧本假件
	probeTimeout time.Duration // 握手硬预算（0 → selfCheckTimeout 默认；测试缩短用）
	check        *SelfCheckInfo
	checkAt      time.Time
}

// NewService 构造向导服务。paths.DataDir() 锚定 mcp 子目录（PLAN §2.5/§6 字面路径）。
func NewService(paths *settings.Paths) *McpWizardService {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	s := &McpWizardService{
		receiptPath: filepath.Join(paths.DataDir(), "mcp", "install.json"),
		accessPath:  filepath.Join(paths.DataDir(), "mcp", "access.json"),
		home:        home,
		env:         os.Getenv,
		command:     exe,
		writeFn:     atomicWrite,
		now:         time.Now,
	}
	s.probe = s.spawnProbe // 生产接线（一行真实 exec；单测覆写此字段）
	return s
}

// —— 绑定 DTO（前端可见）——

// ServerInfo MCP server 启动命令（写入客户端配置的 command/args）。
type ServerInfo struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Ready   bool     `json:"ready"` // exe 路径可解析才可安装
}

// ClientState 单个客户端配置文件的状态行。State 字面量：not-installed /
// installed / needs-repair / conflict / blocked。
type ClientState struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Format       string `json:"format"`
	ConfigPath   string `json:"configPath"`
	DirExists    bool   `json:"dirExists"`
	ConfigExists bool   `json:"configExists"`
	State        string `json:"state"`
	Detail       string `json:"detail"`
	CanInstall   bool   `json:"canInstall"`
	CanUninstall bool   `json:"canUninstall"`
	InstalledAt  string `json:"installedAt"`
}

// AccessTools 授权九开关（PLAN §6 固定键名 + N32/N34 扩充批 + AI 接入批扫描族
// portscan/lan + 端口查杀批 portkill）。扫描键授权的是"有界主动网络探测"（见
// internal/mcp/tools_scan.go 的收口口径），与纯查询工具分键、默认关。
// Portkill 是破坏族授权键（MCP 四道闸之 A1）：**「设置 → AI 接入」面板刻意不
// 为它呈现开关行**（基建既定决策——破坏性键不进逐键 UI 防误操作，开启=机主
// 手动 access.json+destructive.json 两文件），但字段在册：总览呈现要如实反映
// 读者采信结果，且整档回写的 round-trip 依赖它不吞键。
type AccessTools struct {
	Envcheck   bool `json:"envcheck"`
	Everything bool `json:"everything"`
	Ocr        bool `json:"ocr"`
	Memo       bool `json:"memo"`
	Sysinfo    bool `json:"sysinfo"`
	Logs       bool `json:"logs"`
	Portscan   bool `json:"portscan"`
	Lan        bool `json:"lan"`
	Portkill   bool `json:"portkill"`
}

// AccessInfo access.json 的读方视角呈现：Tools 恒等于 MCP 读者此刻的采信结果
// （缺文件/损坏/超纲都呈现为九 false——读者 fail-closed 语义），不呈现读者不认的
// "字面值"。写入口在本分区（SetToolAccess/ResetAccess，R6）。
type AccessInfo struct {
	Path     string      `json:"path"`
	Exists   bool        `json:"exists"`
	Readable bool        `json:"readable"`
	Version  int         `json:"version"`
	Tools    AccessTools `json:"tools"`
	Note     string      `json:"note"`
}

// WizardStatus 分区页初始状态整包。
type WizardStatus struct {
	Server  ServerInfo    `json:"server"`
	Clients []ClientState `json:"clients"`
	Access  AccessInfo    `json:"access"`
}

// WizardPreview 安装/卸载预览。Allowed=false 时看 Reason 与 ManualSnippet；
// Token 供确认调用回传（预览后文件被改则确认整体拒绝）。
type WizardPreview struct {
	Client        string     `json:"client"`
	ClientName    string     `json:"clientName"`
	ConfigPath    string     `json:"configPath"`
	Allowed       bool       `json:"allowed"`
	ZeroDiff      bool       `json:"zeroDiff"`
	WillCreate    bool       `json:"willCreate"`
	Reason        string     `json:"reason"`
	ManualSnippet string     `json:"manualSnippet"`
	Diff          []DiffLine `json:"diff"`
	Token         string     `json:"token"`
}

// OpResult 写操作结果（含回滚信息）。
type OpResult struct {
	Success    bool   `json:"success"`
	RolledBack bool   `json:"rolledBack"`
	BackupPath string `json:"backupPath"`
	Message    string `json:"message"`
}

var (
	// ErrStalePreview 表示预览所依据的目标字节已经变化；调用方必须重新预览，
	// 不得把它降级为普通写失败后盲目重试。
	ErrStalePreview = errors.New("预览已过期")

	confirmPathLocks sync.Map // map[规范化路径]*sync.Mutex；同一路径的最终确认链在进程内严格串行
)

func confirmPathLock(path string) *sync.Mutex {
	key := filepath.Clean(path)
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	lock, _ := confirmPathLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func stalePreviewError(action string) error {
	return fmt.Errorf("%w：预览后目标文件已被改动，本次%s整体取消——请重新预览确认", ErrStalePreview, action)
}

func readCurrent(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

func matchesExpected(path string, expected []byte, expectedExists bool) (bool, error) {
	current, exists, err := readCurrent(path)
	if err != nil {
		return false, err
	}
	return exists == expectedExists && bytes.Equal(current, expected), nil
}

// writeIfUnchanged 在真正替换前以 expected/new 作为条件写契约。
// afterWriteCheck 仅为测试钩子；钩子后再做一次最终比较，确保测试能稳定证明
// “最终检查后发生的外部改写”不会被本次写入覆盖。
func (s *McpWizardService) writeIfUnchanged(path string, expected []byte, expectedExists bool, newData []byte) error {
	matched, err := matchesExpected(path, expected, expectedExists)
	if err != nil {
		return fmt.Errorf("最终条件检查失败: %w", err)
	}
	if !matched {
		return ErrStalePreview
	}
	if s.afterWriteCheck != nil {
		s.afterWriteCheck(path)
		matched, err = matchesExpected(path, expected, expectedExists)
		if err != nil {
			return fmt.Errorf("最终条件复查失败: %w", err)
		}
		if !matched {
			return ErrStalePreview
		}
	}
	return s.writeFn(path, newData)
}

// —— 服务方法（Wails 绑定面）——

// GetStatus 探测三客户端状态 + access.json 呈现，供分区页首屏。
func (s *McpWizardService) GetStatus() (WizardStatus, error) {
	r := loadReceipt(s.receiptPath)
	st := WizardStatus{
		Server: ServerInfo{Command: s.command, Args: serverArgs, Ready: s.command != ""},
		Access: s.accessInfo(),
	}
	for _, spec := range clientSpecs {
		a := s.analyze(spec, r)
		st.Clients = append(st.Clients, a.public())
	}
	return st, nil
}

// PreviewInstall 生成对某客户端的写入预览（只读操作，不落任何盘）。
func (s *McpWizardService) PreviewInstall(clientID string) (WizardPreview, error) {
	return s.preview(clientID, false)
}

// PreviewUninstall 生成摘除预览。
func (s *McpWizardService) PreviewUninstall(clientID string) (WizardPreview, error) {
	return s.preview(clientID, true)
}

// ConfirmInstall 按预览令牌执行写入链：备份 → 原子写 → 复验 → 失败回滚。
func (s *McpWizardService) ConfirmInstall(clientID, token string) (OpResult, error) {
	return s.confirm(clientID, token, false)
}

// ConfirmUninstall 逆向操作：同样走备份/原子写/复验/回滚链，精确摘除自家条目。
func (s *McpWizardService) ConfirmUninstall(clientID, token string) (OpResult, error) {
	return s.confirm(clientID, token, true)
}

// GetAccessOverview 单独刷新授权总览（R6）：九键当前态按读方视角即时重读盘呈现，
// 缺文件 = 九 false 的合法默认态。授权文件 ≤16 KiB，读放大无虞。
func (s *McpWizardService) GetAccessOverview() (AccessInfo, error) {
	return s.accessInfo(), nil
}

// —— 内部：分析与规划 ——

// plan 一次完整规划：当前文件 → 目标字节 + 可否自动写入的裁定。
type plan struct {
	a              clientAnalysis
	allowed        bool
	zeroDiff       bool
	reason         string
	newData        []byte
	newFingerprint string
	snippet        string
	token          string
}

func (s *McpWizardService) planFor(clientID string, uninstall bool) (plan, error) {
	spec, ok := clientSpecByID(clientID)
	if !ok {
		return plan{}, fmt.Errorf("未知客户端: %s", clientID)
	}
	r := loadReceipt(s.receiptPath)
	a := s.analyze(spec, r)
	p := plan{a: a, snippet: s.manualSnippet(spec)}
	switch {
	case a.blocked:
		p.reason = a.detail
		return p, nil
	case a.state == stateConflict:
		p.reason = a.detail
		return p, nil
	}
	if s.command == "" && !uninstall {
		p.reason = "无法解析 hanxi 可执行文件路径（os.Executable 失败），写入中止"
		return p, nil
	}
	var (
		newData []byte
		changed bool
		fp      string
		err     error
	)
	switch {
	case uninstall && spec.Format == fmtJSON:
		newData, changed, err = removeJSONEntry(a.data)
		fp = ""
	case uninstall && spec.Format == fmtTOML:
		newData, changed, err = removeTomlBlock(a.data)
		fp = ""
	case spec.Format == fmtJSON:
		newData, changed, fp, err = mergeJSONEntry(a.data, s.command, serverArgs)
	case spec.Format == fmtTOML:
		newData, changed, fp, err = appendTomlBlock(a.data, s.command, serverArgs)
	}
	if err != nil {
		if errors.Is(err, errUnsafeJSON) || errors.Is(err, errTomlSentinels) {
			p.reason = fmt.Sprintf("不可安全合并，已拒绝自动修改：%v", err)
			return p, nil
		}
		return p, err
	}
	p.allowed = true
	p.zeroDiff = !changed
	p.newData = newData
	p.newFingerprint = fp
	p.token = fingerprint(fmt.Sprintf("%s|%t|%s|%s", clientID, uninstall, string(a.data), string(newData)))
	return p, nil
}

func (s *McpWizardService) preview(clientID string, uninstall bool) (WizardPreview, error) {
	p, err := s.planFor(clientID, uninstall)
	if err != nil {
		return WizardPreview{}, err
	}
	pv := WizardPreview{
		Client: p.a.spec.ID, ClientName: p.a.spec.Name, ConfigPath: p.a.path,
		Allowed: p.allowed, ZeroDiff: p.zeroDiff, WillCreate: p.allowed && !p.a.exists,
		Reason: p.reason, ManualSnippet: p.snippet, Token: p.token,
	}
	if p.allowed {
		pv.Diff = lineDiff(string(p.a.data), string(p.newData))
		if pv.ZeroDiff {
			pv.Reason = "目标条目已与期望一致，无需改动文件（幂等）"
		}
	} else {
		pv.Diff = []DiffLine{{Kind: diffKeep, Text: "已拒绝自动修改，不展示差异"}}
	}
	return pv, nil
}

func (s *McpWizardService) confirm(clientID, token string, uninstall bool) (OpResult, error) {
	// 首次规划只用于解析规范路径；真正的状态/令牌裁定必须在同路径锁内重做，
	// 防止两个确认调用都拿着旧计划穿过检查后相互覆盖。
	initial, err := s.planFor(clientID, uninstall)
	if err != nil {
		return OpResult{}, err
	}
	if initial.a.path == "" {
		if !initial.allowed {
			return OpResult{}, fmt.Errorf("当前状态拒绝%s：%s", actionWord(uninstall), initial.reason)
		}
		return OpResult{}, errors.New("目标配置路径为空")
	}
	pathLock := confirmPathLock(initial.a.path)
	pathLock.Lock()
	defer pathLock.Unlock()

	p, err := s.planFor(clientID, uninstall)
	if err != nil {
		return OpResult{}, err
	}
	if !p.allowed {
		return OpResult{}, fmt.Errorf("当前状态拒绝%s：%s", actionWord(uninstall), p.reason)
	}
	act := actionWord(uninstall)
	if p.token != token {
		return OpResult{}, stalePreviewError(act)
	}
	r := loadReceipt(s.receiptPath)
	if p.zeroDiff {
		matched, merr := matchesExpected(p.a.path, p.a.data, p.a.exists)
		if merr != nil {
			return OpResult{}, fmt.Errorf("零差异确认的最终条件检查失败: %w", merr)
		}
		if !matched {
			return OpResult{}, stalePreviewError(act)
		}
		// 幂等路径：文件一个字节不动，只校准回执（不制造备份、不触发写入链）
		if uninstall {
			r.remove(clientID)
		} else {
			r.set(clientID, p.a.path, p.newFingerprint, s.now())
		}
		if serr := saveReceipt(s.receiptPath, r); serr != nil {
			return OpResult{}, fmt.Errorf("配置文件无需改动，但回执写入失败: %w", serr)
		}
		return OpResult{Success: true, Message: "条目状态已一致，未改动文件（回执已登记）"}, nil
	}
	res, err := s.applyChain(p, uninstall, r)
	if errors.Is(err, ErrStalePreview) {
		return res, stalePreviewError(act)
	}
	return res, err
}

// applyChain 红线写链：备份 expected → expected/new 条件写 → 读回复验 →
// 不一致则回滚（仅当文件仍等于我们写的）。
func (s *McpWizardService) applyChain(p plan, uninstall bool, r *receipt) (OpResult, error) {
	act := actionWord(uninstall)
	path := p.a.path
	bak := ""
	if p.a.exists {
		bak = path + ".hanxi-bak-" + s.now().Format("20060102-150405")
		if werr := os.WriteFile(bak, p.a.data, 0o600); werr != nil {
			return OpResult{BackupPath: bak, Message: fmt.Sprintf("备份原文件失败，%s中止（目标文件未动）: %v", act, werr)}, nil
		}
	}
	if werr := s.writeIfUnchanged(path, p.a.data, p.a.exists, p.newData); werr != nil {
		if errors.Is(werr, ErrStalePreview) {
			return OpResult{BackupPath: bak}, ErrStalePreview
		}
		msg := fmt.Sprintf("写入失败，目标文件未改动: %v", werr)
		if bak != "" {
			msg += fmt.Sprintf("（备份在 %s）", bak)
		}
		return OpResult{BackupPath: bak, Message: msg}, nil
	}
	if vmsg := s.verify(path, p, uninstall); vmsg != "" {
		// 复验失败：仅当文件仍等于我们写的内容才回滚（避免覆盖第三方改动）
		rolled := false
		if cur, rerr := os.ReadFile(path); rerr == nil && string(cur) == string(p.newData) {
			if p.a.exists {
				if rb := s.writeFn(path, p.a.data); rb == nil {
					rolled = true
				}
			} else if rb := os.Remove(path); rb == nil {
				rolled = true
			}
		}
		return OpResult{RolledBack: rolled, BackupPath: bak, Message: fmt.Sprintf("写入后复验不通过：%s；%s", vmsg, rollbackWord(rolled, bak))}, nil
	}
	if uninstall {
		r.remove(p.a.spec.ID)
	} else {
		r.set(p.a.spec.ID, path, p.newFingerprint, s.now())
	}
	receiptNote := ""
	if serr := saveReceipt(s.receiptPath, r); serr != nil {
		receiptNote = fmt.Sprintf("（注意：所有权回执写入失败 %v，下次状态判定可能保守化为冲突拒动）", serr)
	}
	return OpResult{Success: true, BackupPath: bak, Message: fmt.Sprintf("%s成功：%s%s", act, path, receiptNote)}, nil
}

// verify 读回复验：目标文件可解析、条目指纹符合期望（安装=指纹一致；卸载=条目消失）。
func (s *McpWizardService) verify(path string, p plan, uninstall bool) string {
	back, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("读回失败: %v", err)
	}
	switch p.a.spec.Format {
	case fmtJSON:
		fp, ferr := jsonEntryFingerprint(back)
		if ferr != nil {
			return fmt.Sprintf("读回后不再是合法 JSON: %v", ferr)
		}
		if uninstall && fp != "" {
			return "hanxi 条目仍残留在配置中"
		}
		if !uninstall && fp != p.newFingerprint {
			return "读回的 hanxi 条目与写入意图不一致"
		}
	case fmtTOML:
		if verr := validateToml(back); verr != nil {
			return fmt.Sprintf("读回后 TOML 解析失败: %v", verr)
		}
		fp, ferr := tomlEntryFingerprint(back)
		if ferr != nil {
			return fmt.Sprintf("托管区块哨兵异常: %v", ferr)
		}
		if uninstall && fp != "" {
			return "托管区块仍残留在配置中"
		}
		if !uninstall && fp != p.newFingerprint {
			return "读回的托管区块与写入意图不一致"
		}
	}
	return ""
}

func actionWord(uninstall bool) string {
	if uninstall {
		return "卸载"
	}
	return "安装"
}

func rollbackWord(rolled bool, bak string) string {
	if rolled {
		return "已自动回滚到写入前状态"
	}
	if bak != "" {
		return fmt.Sprintf("未能自动回滚，请从备份恢复: %s", bak)
	}
	return "未能自动回滚，且新建文件无备份可恢复——请手工检查该路径"
}

// —— access.json 读方视角呈现 ——

// accessInfo 按读方严格规则（strictLoadAccess，与 internal/mcp/access.go 同款判定）
// 呈现"读者此刻看到什么"：采信→真实九键；不采信（损坏/超纲/未知键/版本≠1）或
// 缺文件→九 false。呈现与判定永不分家，杜绝"界面显示已授权、读者实际全拒"的口径裂缝。
func (s *McpWizardService) accessInfo() AccessInfo {
	st := strictLoadAccess(s.accessPath)
	info := AccessInfo{Path: s.accessPath, Exists: st.exists}
	switch {
	case st.missing:
		info.Note = "授权文件尚未生成——MCP 读者对此默认全关（fail-closed），这是合法默认态；拨动下方任一开关即自动建档并保存即生效"
	case !st.legal:
		info.Note = fmt.Sprintf("授权文件损坏或超纲（%s）——MCP 读者对其 fail-closed，视同全部未授权。"+
			"可点「修复（覆盖重置）」：旧档先另存 .bak，再重写标准全关档，随后逐项重新授权", st.reason)
	default:
		info.Readable = true
		info.Version = accessSchemaVer
		info.Tools = AccessTools{
			Envcheck:   st.tools["envcheck"],
			Everything: st.tools["everything"],
			Ocr:        st.tools["ocr"],
			Memo:       st.tools["memo"],
			Sysinfo:    st.tools["sysinfo"],
			Logs:       st.tools["logs"],
			Portscan:   st.tools["portscan"],
			Lan:        st.tools["lan"],
			Portkill:   st.tools["portkill"],
		}
	}
	return info
}

// —— 工具函数 ——

// atomicWrite 同目录 tmp + rename 原子替换（jsonstore 范式；目标存在则整体替换，
// 绝不就地截断重写——读侧永远看到完整旧版或完整新版）。
func atomicWrite(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
