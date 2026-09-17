package mcpwizard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestService 以临时目录搭 fake home 与 fake DataDir，exe 路径固定假值。
func newTestService(t *testing.T, envs map[string]string) (*McpWizardService, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	dataDir := filepath.Join(root, "hanxidata")
	if err := os.MkdirAll(home, 0o755); err != nil { // claude 的配置目录即 home 本身
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "mcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &McpWizardService{
		receiptPath: filepath.Join(dataDir, "mcp", "install.json"),
		accessPath:  filepath.Join(dataDir, "mcp", "access.json"),
		home:        home,
		env:         func(k string) string { return envs[k] },
		command:     filepath.Join(root, "hanxi.exe"),
		writeFn:     atomicWrite,
		now:         func() time.Time { return time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local) },
	}, home
}

func clientState(t *testing.T, svc *McpWizardService, id string) ClientState {
	t.Helper()
	st, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range st.Clients {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("状态里缺客户端 %s", id)
	return ClientState{}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func countBackups(t *testing.T, target string) int {
	t.Helper()
	matches, err := filepath.Glob(target + ".hanxi-bak-*")
	if err != nil {
		t.Fatal(err)
	}
	return len(matches)
}

func installFlow(t *testing.T, svc *McpWizardService, id string) OpResult {
	t.Helper()
	pv, err := svc.PreviewInstall(id)
	if err != nil {
		t.Fatalf("%s 预览失败: %v", id, err)
	}
	if !pv.Allowed {
		t.Fatalf("%s 预览被拒: %s", id, pv.Reason)
	}
	res, err := svc.ConfirmInstall(id, pv.Token)
	if err != nil {
		t.Fatalf("%s 确认失败: %v", id, err)
	}
	if !res.Success {
		t.Fatalf("%s 写入未成功: %s", id, res.Message)
	}
	return res
}

func TestGetStatusShape(t *testing.T) {
	svc, _ := newTestService(t, nil)
	st, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Clients) != 3 {
		t.Fatalf("应固定呈现 3 客户端，得 %d", len(st.Clients))
	}
	if !st.Server.Ready || len(st.Server.Args) != 1 || st.Server.Args[0] != "mcp" {
		t.Errorf("server 信息异常: %+v", st.Server)
	}
	for _, c := range st.Clients {
		switch c.ID {
		case "claude": // 目录即 home：可凭空新建
			if c.State != stateNotInstalled || !c.CanInstall || c.CanUninstall {
				t.Errorf("claude 初始态异常: %+v", c)
			}
		default: // codex/cursor 目录缺失 = 客户端无踪迹，拒装
			if c.State != stateNotInstalled || c.CanInstall || c.CanUninstall || !strings.Contains(c.Detail, "未检测到") {
				t.Errorf("%s 未检测到态异常: %+v", c.ID, c)
			}
		}
	}
}

func TestClaudeInstallIdempotentUninstall(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	orig := "{\n  \"theme\": \"dark\",\n  \"mcpServers\": {\n    \"other\": { \"command\": \"x\" }\n  }\n}"
	writeFile(t, target, orig)

	if cs := clientState(t, svc, "claude"); cs.State != stateNotInstalled || !cs.ConfigExists {
		t.Fatalf("初始态异常: %+v", cs)
	}

	installFlow(t, svc, "claude")
	got := readFile(t, target)
	if !strings.Contains(got, `"hanxi"`) || !strings.Contains(got, `"theme"`) || !strings.Contains(got, `"other"`) {
		t.Errorf("写入结果异常:\n%s", got)
	}
	if n := countBackups(t, target); n != 1 {
		t.Errorf("应产生 1 份备份，得 %d", n)
	}
	cs := clientState(t, svc, "claude")
	if cs.State != stateInstalled || !cs.CanUninstall || cs.InstalledAt == "" {
		t.Errorf("安装后态异常: %+v", cs)
	}

	// 幂等：重复安装零 diff、零重写、零新备份
	pv, err := svc.PreviewInstall("claude")
	if err != nil || !pv.Allowed || !pv.ZeroDiff {
		t.Fatalf("二次预览应 ZeroDiff: %+v %v", pv, err)
	}
	res, err := svc.ConfirmInstall("claude", pv.Token)
	if err != nil || !res.Success {
		t.Fatalf("二次确认失败: %+v %v", res, err)
	}
	if readFile(t, target) != got {
		t.Error("幂等确认改动了文件")
	}
	if n := countBackups(t, target); n != 1 {
		t.Errorf("幂等确认不应新增备份，得 %d", n)
	}

	// 卸载：精确摘除自家条目，他项原样
	pvu, err := svc.PreviewUninstall("claude")
	if err != nil || !pvu.Allowed {
		t.Fatalf("卸载预览失败: %+v %v", pvu, err)
	}
	if rmu, err := svc.ConfirmUninstall("claude", pvu.Token); err != nil || !rmu.Success {
		t.Fatalf("卸载失败: %+v %v", rmu, err)
	}
	after := readFile(t, target)
	if strings.Contains(after, "hanxi") {
		t.Errorf("卸载后 hanxi 残留:\n%s", after)
	}
	var top map[string]any
	if err := json.Unmarshal([]byte(after), &top); err != nil {
		t.Fatalf("卸载后 JSON 损坏: %v", err)
	}
	if top["theme"] != "dark" {
		t.Error("卸载丢失无关键")
	}
	if len(top["mcpServers"].(map[string]any)) != 1 {
		t.Error("mcpServers.other 应保留")
	}
	if cs := clientState(t, svc, "claude"); cs.State != stateNotInstalled {
		t.Errorf("卸载后应回 not-installed: %+v", cs)
	}
}

func TestClaudeCreateWhenFileMissing(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	pv, err := svc.PreviewInstall("claude")
	if err != nil || !pv.Allowed || !pv.WillCreate {
		t.Fatalf("凭空新建预览异常: %+v %v", pv, err)
	}
	installFlow(t, svc, "claude")
	if !strings.Contains(readFile(t, target), `"hanxi"`) {
		t.Error("新建文件缺 hanxi 条目")
	}
}

func TestCodexManagedBlock(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".codex", "config.toml")
	orig := "sandbox_mode = \"workspace-write\"\n"
	writeFile(t, target, orig)

	installFlow(t, svc, "codex")
	got := readFile(t, target)
	if !strings.HasPrefix(got, orig) {
		t.Error("用户原内容被重排")
	}
	if strings.Count(got, tomlBegin) != 1 || !strings.Contains(got, "command = ") {
		t.Errorf("托管区块异常:\n%s", got)
	}
	if cs := clientState(t, svc, "codex"); cs.State != stateInstalled {
		t.Errorf("安装后态异常: %+v", cs)
	}
	pv, err := svc.PreviewInstall("codex")
	if err != nil || !pv.ZeroDiff {
		t.Errorf("codex 重复安装应 ZeroDiff: %+v %v", pv, err)
	}
	if _, err := svc.ConfirmInstall("codex", pv.Token); err != nil {
		t.Fatal(err)
	}

	pvu, err := svc.PreviewUninstall("codex")
	if err != nil || !pvu.Allowed {
		t.Fatalf("codex 卸载预览失败: %+v %v", pvu, err)
	}
	if r, err := svc.ConfirmUninstall("codex", pvu.Token); err != nil || !r.Success {
		t.Fatalf("codex 卸载失败: %+v %v", r, err)
	}
	if back := readFile(t, target); back != orig {
		t.Errorf("codex 卸载未回到原字节:\n%q", back)
	}
}

func TestCursorJSONCFailClosed(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".cursor", "mcp.json")
	jsonc := "{\n  // 手工维护的配置\n  \"mcpServers\": {}\n}"
	writeFile(t, target, jsonc)

	cs := clientState(t, svc, "cursor")
	if cs.State != stateBlocked || cs.CanInstall || cs.CanUninstall {
		t.Fatalf("JSONC 应 blocked 拒动: %+v", cs)
	}
	pv, err := svc.PreviewInstall("cursor")
	if err != nil || pv.Allowed {
		t.Fatalf("JSONC 预览必须 Allowed=false: %+v %v", pv, err)
	}
	if !strings.Contains(pv.ManualSnippet, "hanxi") || !strings.Contains(pv.ManualSnippet, "mcpServers") {
		t.Errorf("应给出手动片段: %s", pv.ManualSnippet)
	}
	if _, err := svc.ConfirmInstall("cursor", pv.Token); err == nil {
		t.Error("ConfirmInstall 必须拒绝 JSONC")
	}
	if readFile(t, target) != jsonc {
		t.Error("拒绝路径不得改动文件")
	}
}

func TestClientNotDetected(t *testing.T) {
	svc, _ := newTestService(t, nil) // home 存在但无 .cursor 目录
	cs := clientState(t, svc, "cursor")
	if cs.CanInstall || cs.CanUninstall || !strings.Contains(cs.Detail, "未检测到") {
		t.Fatalf("目录缺失应拒装: %+v", cs)
	}
	pv, err := svc.PreviewInstall("cursor")
	if err != nil || pv.Allowed || !strings.Contains(pv.Reason, "未检测到") {
		t.Errorf("预览应拒绝并说明: %+v %v", pv, err)
	}
}

func TestStaleTokenRejected(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	writeFile(t, target, `{"a":1}`)
	pv, err := svc.PreviewInstall("claude")
	if err != nil || !pv.Allowed {
		t.Fatal(pv, err)
	}
	// 预览后第三方改文件 → 确认整体拒绝
	writeFile(t, target, `{"a":1,"b":2}`)
	if _, err := svc.ConfirmInstall("claude", pv.Token); err == nil || !strings.Contains(err.Error(), "预览后") {
		t.Fatalf("陈旧令牌应被拒: %v", err)
	}
	if strings.Contains(readFile(t, target), "hanxi") {
		t.Error("拒绝路径不得写入")
	}
}

func TestConflictAfterUserEdit(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	writeFile(t, target, `{"mcpServers":{}}`)
	installFlow(t, svc, "claude")

	// 用户改了自家条目（加了 env 键）
	userEdited := `{"mcpServers":{"hanxi":{"command":"` + strings.ReplaceAll(svc.command, `\`, `\\`) + `","args":["mcp"],"env":{"K":"V"}}}}`
	writeFile(t, target, userEdited)
	cs := clientState(t, svc, "claude")
	if cs.State != stateConflict || cs.CanInstall || cs.CanUninstall {
		t.Fatalf("改过的条目应冲突拒动: %+v", cs)
	}
	if _, err := svc.ConfirmInstall("claude", "any"); err == nil {
		t.Error("冲突态 ConfirmInstall 必须拒绝")
	}
	pvu, err := svc.PreviewUninstall("claude")
	if err != nil || pvu.Allowed {
		t.Errorf("冲突态卸载预览必须 Allowed=false: %+v %v", pvu, err)
	}
	if readFile(t, target) != userEdited {
		t.Error("冲突拒动不得改文件")
	}
}

func TestExternalIdenticalEntryAdopted(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	// 用户手工贴入与预期完全一致的条目（键序不同也算一致）
	manual := fmt.Sprintf(`{"mcpServers":{"hanxi":{"args":["mcp"],"command":%q}}}`, svc.command)
	writeFile(t, target, manual)
	cs := clientState(t, svc, "claude")
	if cs.State != stateInstalled || !strings.Contains(cs.Detail, "非本向导") {
		t.Fatalf("外部一致条目应 installed: %+v", cs)
	}
	pv, err := svc.PreviewInstall("claude")
	if err != nil || !pv.ZeroDiff {
		t.Fatalf("应 ZeroDiff: %+v %v", pv, err)
	}
	res, err := svc.ConfirmInstall("claude", pv.Token)
	if err != nil || !res.Success {
		t.Fatal(res, err)
	}
	if readFile(t, target) != manual {
		t.Error("采纳路径不得改文件")
	}
	if n := countBackups(t, target); n != 0 {
		t.Errorf("采纳路径不应备份，得 %d", n)
	}
	if cs := clientState(t, svc, "claude"); cs.InstalledAt == "" {
		t.Error("采纳应登记回执时间")
	}
}

func TestNeedsRepairReinstallAndUninstall(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	writeFile(t, target, `{"mcpServers":{}}`)
	installFlow(t, svc, "claude")
	// 用户删掉整个文件
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	cs := clientState(t, svc, "claude")
	if cs.State != stateNeedsRepair || !cs.CanInstall || !cs.CanUninstall {
		t.Fatalf("回执在、文件没了 → needs-repair: %+v", cs)
	}
	// 卸载走零写链：只清回执，不重建文件
	pv, err := svc.PreviewUninstall("claude")
	if err != nil || !pv.Allowed || !pv.ZeroDiff {
		t.Fatalf("needs-repair 卸载应 ZeroDiff: %+v %v", pv, err)
	}
	if _, err := svc.ConfirmUninstall("claude", pv.Token); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Error("卸载不得重建文件")
	}
	if cs := clientState(t, svc, "claude"); cs.State != stateNotInstalled {
		t.Errorf("清回执后应 not-installed: %+v", cs)
	}
	// 修复路径：重新安装凭空建文件
	installFlow(t, svc, "claude")
	if cs := clientState(t, svc, "claude"); cs.State != stateInstalled {
		t.Errorf("修复后应 installed: %+v", cs)
	}
}

func TestWriteFailureLeavesFileUntouched(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	orig := `{"a":1}`
	writeFile(t, target, orig)
	// 错误注入：预占 atomicWrite 的 tmp 路径（同名目录），OpenFile 必失败
	blockedTmp := fmt.Sprintf("%s.tmp.%d", target, os.Getpid())
	if err := os.Mkdir(blockedTmp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(blockedTmp) })
	pv, err := svc.PreviewInstall("claude")
	if err != nil || !pv.Allowed {
		t.Fatal(pv, err)
	}
	res, err := svc.ConfirmInstall("claude", pv.Token)
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !strings.Contains(res.Message, "写入失败") {
		t.Fatalf("应报写入失败: %+v", res)
	}
	if readFile(t, target) != orig {
		t.Error("写失败不得动目标文件")
	}
	if countBackups(t, target) != 1 {
		t.Error("备份应已先行落盘（供排查）")
	}
}

func TestApplyChainRollbackRestore(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	orig := "{\"a\":1}\n"
	writeFile(t, target, orig)
	newData, _, _, err := mergeJSONEntry([]byte(orig), svc.command, serverArgs)
	if err != nil {
		t.Fatal(err)
	}
	// 指纹故意错配（等价"写盘内容语义校验不过"），走回滚恢复支
	p := plan{
		a: clientAnalysis{spec: clientSpecs[0], dir: home, path: target, data: []byte(orig),
			dirExists: true, exists: true},
		allowed: true, newData: newData, newFingerprint: "bogus",
	}
	res := svc.applyChain(p, false, newReceipt())
	if res.Success || !res.RolledBack {
		t.Fatalf("应自动回滚: %+v", res)
	}
	if readFile(t, target) != orig {
		t.Errorf("回滚未恢复原内容:\n%s", readFile(t, target))
	}
	if res.BackupPath == "" {
		t.Error("回滚场景必须报告备份路径")
	}
}

func TestApplyChainRollbackRemovesCreatedFile(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	newData, _, _, err := mergeJSONEntry(nil, svc.command, serverArgs)
	if err != nil {
		t.Fatal(err)
	}
	p := plan{
		a:       clientAnalysis{spec: clientSpecs[0], dir: home, path: target, dirExists: true, exists: false},
		allowed: true, newData: newData, newFingerprint: "bogus",
	}
	res := svc.applyChain(p, false, newReceipt())
	if res.Success || !res.RolledBack {
		t.Fatalf("新建文件复验失败应删除回滚: %+v", res)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Error("回滚后不得残留半成品文件")
	}
}

func TestThirdPartyRaceAfterWriteSkipsRollback(t *testing.T) {
	svc, home := newTestService(t, nil)
	target := filepath.Join(home, ".claude.json")
	orig := `{"a":1}` + "\n"
	writeFile(t, target, orig)
	// 模拟我们写完后毫秒级第三方又改（盘上 ≠ 我们写的字节）：
	// 按 PLAN 裁定"仅当文件仍等于我们写的"才回滚——此处必须不覆盖第三方内容
	svc.writeFn = func(path string, data []byte) error {
		if err := atomicWrite(path, data); err != nil {
			return err
		}
		return atomicWrite(path, append(append([]byte{}, data...), []byte("\n!!!")...))
	}
	pv, _ := svc.PreviewInstall("claude")
	res, err := svc.ConfirmInstall("claude", pv.Token)
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || res.RolledBack {
		t.Fatalf("第三方竞态不得自动回滚: %+v", res)
	}
	if !strings.Contains(res.Message, "备份") {
		t.Errorf("应指引从备份恢复: %s", res.Message)
	}
	if !strings.Contains(readFile(t, target), "!!!") {
		t.Error("第三方改动被覆盖了（违反仅等于我们写的才回滚）")
	}
}

func TestEnvOverridePaths(t *testing.T) {
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-alt")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(codexHome, "config.toml"), "x = 1\n")
	svc, _ := newTestService(t, map[string]string{"CODEX_HOME": codexHome})
	installFlow(t, svc, "codex")
	if !strings.Contains(readFile(t, filepath.Join(codexHome, "config.toml")), tomlBegin) {
		t.Error("CODEX_HOME 覆盖未生效")
	}
}

func TestUnknownClient(t *testing.T) {
	svc, _ := newTestService(t, nil)
	if _, err := svc.PreviewInstall("vscode"); err == nil {
		t.Error("未知客户端必须报错")
	}
}

func TestAccessInfoReadOnlyPresentation(t *testing.T) {
	svc, _ := newTestService(t, nil)
	info := svc.accessInfo()
	if info.Exists || info.Readable || !strings.Contains(info.Note, "尚未生成") {
		t.Fatalf("缺文件呈现异常: %+v", info)
	}
	// 写入 PLAN §6 样例后只读呈现
	writeFile(t, svc.accessPath, `{"version":1,"tools":{"envcheck":true,"everything":false,"ocr":false,"memo":false}}`)
	info = svc.accessInfo()
	if !info.Exists || !info.Readable || info.Version != 1 {
		t.Fatalf("正常文件呈现异常: %+v", info)
	}
	if !info.Tools.Envcheck || info.Tools.Everything || info.Tools.Ocr || info.Tools.Memo {
		t.Errorf("工具开关解析错: %+v", info.Tools)
	}
	// 损坏：fail-closed 文案，且向导绝不代写修复
	writeFile(t, svc.accessPath, `{"version":1,,}`)
	info = svc.accessInfo()
	if !info.Exists || info.Readable || !strings.Contains(info.Note, "损坏") {
		t.Fatalf("损坏文件呈现异常: %+v", info)
	}
	before := readFile(t, svc.accessPath)
	if _, err := svc.GetStatus(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, svc.accessPath) != before {
		t.Error("呈现路径不得触碰 access.json")
	}
}

func TestReceiptSurvivesRoundTrip(t *testing.T) {
	svc, home := newTestService(t, nil)
	writeFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{}}`)
	installFlow(t, svc, "claude")
	r := loadReceipt(svc.receiptPath)
	e, ok := r.get("claude")
	if !ok || e.Fingerprint == "" || e.ConfigPath == "" {
		t.Fatalf("回执内容异常: %+v", r)
	}
}
