package wsl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/modules/wsl/readiness"
)

// ---- 测试脚手架：wsl.exe 外呼桩 ----

type wslStub struct {
	calls [][]string
	resp  func(args []string) (string, error)
}

func (w *wslStub) run(_ context.Context, args ...string) (string, error) {
	w.calls = append(w.calls, args)
	return w.resp(args)
}

func joined(args []string) string { return strings.Join(args, " ") }

func joinedCalls(stub *wslStub) []string {
	var out []string
	for _, c := range stub.calls {
		out = append(out, joined(c))
	}
	return out
}

// quietStub：`-l -q` 名单返回 names（NUL 分隔），其余参数组合走 fallback。
func quietStub(names []string, fallback func(args []string) (string, error)) func([]string) (string, error) {
	return func(args []string) (string, error) {
		switch joined(args) {
		case "-l -q":
			return strings.Join(names, "\x00") + "\x00", nil
		case "-l -q --running":
			return "", errors.New("未安装") // 默认名单为空：running 集取不到 → 列表走回退
		}
		if fallback != nil {
			return fallback(args)
		}
		return "", fmt.Errorf("意外的 wsl 调用: %v", args)
	}
}

// ---- 解析小件 ----

func TestParseQuietList(t *testing.T) {
	got := parseQuietList("Ubuntu-24.04\x00Debian\x00")
	if len(got) != 2 || got[0] != "Ubuntu-24.04" || got[1] != "Debian" {
		t.Fatalf("NUL 名单拆分错误: %q", got)
	}
	if n := parseQuietList(""); n != nil {
		t.Fatalf("空串应得空名单: %q", n)
	}
}

func TestStateLooksRunning(t *testing.T) {
	for _, s := range []string{"Running", "正在运行"} {
		if !stateLooksRunning(s) {
			t.Fatalf("%q 应判运行", s)
		}
	}
	for _, s := range []string{"Stopped", "已停止"} {
		if stateLooksRunning(s) {
			t.Fatalf("%q 不应判运行", s)
		}
	}
}

// ---- 列表聚合 ----

func TestListInstancesMergesSources(t *testing.T) {
	svc, _, _ := newTestService()
	svc.wslDistros = func(context.Context) []readiness.Distro {
		// 状态列原文与 -q 名单故意矛盾：必须以 -q 名单为准（跨语言判据红线）。
		return []readiness.Distro{
			{Name: "Ubuntu", State: "正在运行", Version: "2", Default: false},
			{Name: "Debian", State: "Stopped", Version: "1", Default: true},
		}
	}
	stub := &wslStub{resp: func(args []string) (string, error) {
		switch joined(args) {
		case "-l -q":
			return "Ubuntu\x00Debian\x00", nil
		case "-l -q --running":
			return "Debian\x00", nil
		}
		return "", fmt.Errorf("意外调用 %v", args)
	}}
	svc.runWsl = stub.run
	svc.localPS = func(_ context.Context, script string) (string, error) {
		if strings.HasPrefix(script, "$items") {
			return `[{"name":"Ubuntu","basePath":"C:\\lxss\\u","vhdx":"C:\\lxss\\u\\ext4.vhdx","size":1024}]`, nil
		}
		return "", nil
	}

	list, err := svc.ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("列表长度错误: %+v", list)
	}
	u, d := list[0], list[1]
	if u.Running || !d.Running {
		t.Fatalf("运行态必须采信 -q --running 名单: %+v", list)
	}
	if !u.Default || d.Default {
		t.Fatalf("默认必须采信 -q 首行: %+v", list)
	}
	if u.BasePath != `C:\lxss\u` || u.SizeBytes != 1024 {
		t.Fatalf("注册表信息未合并: %+v", u)
	}
	if d.BasePath != "" || d.SizeBytes != 0 {
		t.Fatalf("注册表缺失项应留空不编造: %+v", d)
	}
}

func TestListInstancesDegradesWithoutQuiet(t *testing.T) {
	svc, _, _ := newTestService()
	svc.wslDistros = func(context.Context) []readiness.Distro {
		return []readiness.Distro{{Name: "Ubuntu", State: "Running", Version: "2", Default: true}}
	}
	svc.runWsl = (&wslStub{resp: func(args []string) (string, error) {
		return "", errors.New("wsl.exe 不可用")
	}}).run
	svc.localPS = func(context.Context, string) (string, error) { return "", errors.New("ps blocked") }

	list, err := svc.ListInstances() // 辅助通道全灭不是错误：列表本体还在就如实降级
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Running || !list[0].Default {
		t.Fatalf("降级回退（状态原文 + `*` 标记）失效: %+v", list)
	}
}

func TestListInstancesEmptyIsNotError(t *testing.T) {
	svc, _, _ := newTestService()
	list, err := svc.ListInstances() // 默认 fake wslDistros 返回 nil
	if err != nil || len(list) != 0 {
		t.Fatalf("未安装任何发行版应返回空列表非错误: %+v %v", list, err)
	}
}

// ---- 白名单与互斥 ----

func TestDistroOpsRejectUnknownName(t *testing.T) {
	for _, tc := range []struct {
		label  string
		invoke func(svc *WslService) error
	}{
		{"terminate", func(s *WslService) error { _, e := s.TerminateDistro("evil"); return e }},
		{"set-default", func(s *WslService) error { _, e := s.SetDefaultDistro("evil"); return e }},
		{"unregister", func(s *WslService) error { _, e := s.UnregisterDistro("evil"); return e }},
		{"export", func(s *WslService) error { _, e := s.ExportDistro("evil", false); return e }},
		{"move", func(s *WslService) error { _, e := s.MoveDistro("evil", `D:\x`); return e }},
		{"terminal", func(s *WslService) error { _, e := s.OpenTerminal("evil"); return e }},
	} {
		svc, ev, _ := newTestService()
		stub := &wslStub{resp: quietStub([]string{"Ubuntu"}, nil)}
		svc.runWsl = stub.run
		started := false
		svc.startTerm = func(context.Context, string) error { started = true; return nil }
		if err := tc.invoke(svc); err == nil {
			t.Fatalf("%s: 名单外发行版必须拒绝", tc.label)
		}
		if started || len(ev.calls) != 0 {
			t.Fatalf("%s: 被拒绝的操作不得触达提权/唤端通道", tc.label)
		}
		for _, c := range stub.calls {
			if !strings.HasPrefix(joined(c), "-l -q") {
				t.Fatalf("%s: 白名单校验前不得执行其它 wsl 命令: %v", tc.label, c)
			}
		}
	}
}

func TestDistroGuardsMutex(t *testing.T) {
	svc, _, _ := newTestService()
	finish, ok := svc.tryBeginDistroOp("Ubuntu", "export")
	if !ok {
		t.Fatal("首个操作应获准入闸")
	}
	if _, ok := svc.tryBeginDistroOp("ubuntu", "terminate"); ok {
		t.Fatal("同名（大小写归一）操作必须单飞")
	}
	if _, ok := svc.tryBeginDistroOp("Debian", "export"); !ok {
		t.Fatal("不同发行版操作互不干扰")
	}
	if _, ok := svc.tryBeginHeavyOp(); ok {
		t.Fatal("有发行版操作在飞时迁移必须拒入")
	}
	finish()
	svc.mu.Lock()
	delete(svc.distroOps, "debian") // 清掉上一个获准未收尾的闸
	svc.mu.Unlock()
	hf, ok := svc.tryBeginHeavyOp()
	if !ok {
		t.Fatal("闸门清空后迁移应获准")
	}
	if _, ok := svc.tryBeginDistroOp("Ubuntu", "terminate"); ok {
		t.Fatal("迁移在飞时任何发行版操作必须拒入")
	}
	hf()
}

// ---- 操作happy path 与命令面形态 ----

func TestTerminateAndSetDefaultUseExactArgs(t *testing.T) {
	svc, _, _ := newTestService()
	stub := &wslStub{resp: quietStub([]string{"Ubuntu"}, func(args []string) (string, error) {
		return "", nil
	})}
	svc.runWsl = stub.run

	if out, err := svc.TerminateDistro(" Ubuntu "); err != nil || !out.Success {
		t.Fatalf("terminate 失败: %+v %v", out, err)
	}
	if out, err := svc.SetDefaultDistro("Ubuntu"); err != nil || !out.Success {
		t.Fatalf("set-default 失败: %+v %v", out, err)
	}
	want := []string{"-l -q", "--terminate Ubuntu", "-l -q", "--set-default Ubuntu"}
	if got := joinedCalls(stub); strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("命令面漂移: %v", got)
	}
}

func TestUnregisterTerminatesFirstEvenIfItFails(t *testing.T) {
	// 参考实现的卡死教训：对运行中发行版直接 unregister 可能长挂，
	// 必须先 terminate 收敛运行态；terminate 失败（本就停止）不拦路。
	svc, _, _ := newTestService()
	stub := &wslStub{resp: quietStub([]string{"Ubuntu"}, func(args []string) (string, error) {
		if joined(args) == "--terminate Ubuntu" {
			return "not running", errors.New("exit 1")
		}
		return "", nil
	})}
	svc.runWsl = stub.run
	if out, err := svc.UnregisterDistro("Ubuntu"); err != nil || !out.Success {
		t.Fatalf("terminate 预步骤失败不应拦注销: %+v %v", out, err)
	}
	if !strings.Contains(joinedCalls(stub)[len(stub.calls)-1], "--unregister Ubuntu") {
		t.Fatalf("注销命令未最后执行: %v", joinedCalls(stub))
	}

	// unregister 本身失败必须如实报错。
	svc2, _, _ := newTestService()
	svc2.runWsl = (&wslStub{resp: quietStub([]string{"Ubuntu"}, func(args []string) (string, error) {
		return "错误: 正被使用", errors.New("exit 5")
	})}).run
	if _, err := svc2.UnregisterDistro("Ubuntu"); err == nil {
		t.Fatal("unregister 失败必须报错而非假成功")
	}
}

func TestOpenTerminalPassesValidatedName(t *testing.T) {
	svc, _, _ := newTestService()
	svc.runWsl = (&wslStub{resp: quietStub([]string{"Ubuntu"}, nil)}).run
	var got string
	svc.startTerm = func(_ context.Context, name string) error { got = name; return nil }
	if out, err := svc.OpenTerminal("Ubuntu"); err != nil || !out.Success {
		t.Fatal(err)
	}
	if got != "Ubuntu" {
		t.Fatalf("透传名错误: %s", got)
	}
}

// ---- 导出 ----

func TestExportDistro(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "WSL 导出")
	old := exportDir
	exportDir = func() string { return dir }
	defer func() { exportDir = old }()

	svc, _, _ := newTestService()
	var exportArgs []string
	svc.runWsl = (&wslStub{resp: quietStub([]string{"Ubuntu-24.04"}, func(args []string) (string, error) {
		if strings.HasPrefix(joined(args), "--export") {
			exportArgs = args
			path := args[len(args)-1]
			return "", os.WriteFile(path, []byte("tar"), 0o644)
		}
		return "", fmt.Errorf("意外调用 %v", args)
	})}).run

	out, err := svc.ExportDistro("Ubuntu-24.04", true)
	if err != nil || !out.Success {
		t.Fatalf("导出失败: %+v %v", out, err)
	}
	if joined(exportArgs) != "--export Ubuntu-24.04 --format tar.gz "+exportArgs[len(exportArgs)-1] {
		t.Fatalf("gzip 导出命令面错误: %v", exportArgs)
	}
	if !strings.HasSuffix(out.ID, ".tar.gz") || !exportIDRe.MatchString(out.ID) {
		t.Fatalf("工件名不合白名单形态: %s", out.ID)
	}
	if _, err := os.Stat(out.Path); err != nil {
		t.Fatalf("产物应存在: %v", err)
	}
	if !strings.Contains(out.Message, "已导出") {
		t.Fatalf("回执文案错误: %s", out.Message)
	}

	// Reveal 白名单：只认本会话登记的 ID。
	var revealed string
	oldReveal := revealInExplorer
	revealInExplorer = func(p string) error { revealed = p; return nil }
	defer func() { revealInExplorer = oldReveal }()
	if err := svc.RevealDistroExport(out.ID); err != nil || revealed != out.Path {
		t.Fatalf("Reveal 应直达登记路径: %v %s", err, revealed)
	}
	if err := svc.RevealDistroExport("../../evil.tar"); err == nil {
		t.Fatal("非法 ID 必须拒绝")
	}
	if err := svc.RevealDistroExport("ghost-20260101-000000.tar"); err == nil {
		t.Fatal("未登记 ID 必须拒绝")
	}
}

func TestExportDistroFailureCleansPartial(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "WSL 导出")
	old := exportDir
	exportDir = func() string { return dir }
	defer func() { exportDir = old }()

	svc, _, _ := newTestService()
	var partial string
	svc.runWsl = (&wslStub{resp: quietStub([]string{"Ubuntu"}, func(args []string) (string, error) {
		partial = args[len(args)-1]
		_ = os.WriteFile(partial, []byte("半截"), 0o644) // 模拟 wsl 失败时留下的残档
		return "导出中途炸裂", errors.New("exit 1")
	})}).run

	if _, err := svc.ExportDistro("Ubuntu", false); err == nil {
		t.Fatal("导出失败必须报错")
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("失败半成品不得留在用户下载目录")
	}
	if !exportIDRe.MatchString(filepath.Base(partial)) {
		t.Fatalf("文件名清洗形态异常: %s", partial)
	}
}

func TestSanitizeFileNamePart(t *testing.T) {
	if got := sanitizeFileNamePart(`a<b>:c"/d\e|f?g*h`); strings.ContainsAny(got, `<>:"/\|?*`) {
		t.Fatalf("非法字符未清洗: %s", got)
	}
	if got := sanitizeFileNamePart("  "); got != "distro" {
		t.Fatalf("空白名应兜底: %s", got)
	}
}

// ---- 迁移 ----

func TestMoveTargetValidation(t *testing.T) {
	base := t.TempDir()
	current := filepath.Join(base, "cur")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}
	nonEmpty := filepath.Join(base, "nonempty")
	if err := os.MkdirAll(nonEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "x.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []struct{ target, base string }{
		{`relative\path`, ""},                   // 非绝对
		{`Q:\不存在盘\x`, ""},                       // 盘符不存在
		{`C:\Windows\System32\drivers`, ""},     // 非空系统目录（存在且非空）
		{nonEmpty, ""},                          // 存在但非空
		{current, current},                      // 自我迁移
		{filepath.Join(current, "in"), current}, // 嵌套进自身
		{filepath.Join(base, "a<b"), ""},        // 非法字符
		{`C:\fo:o\bar`, ""},                     // 盘符后再冒号
	} {
		if _, err := moveTarget(bad.target, bad.base); err == nil {
			t.Fatalf("应拒绝: %q (base %q)", bad.target, bad.base)
		}
	}
	good := filepath.Join(base, "brand-new-dir")
	if got, err := moveTarget(good, current); err != nil || filepath.Clean(got) != filepath.Clean(good) {
		t.Fatalf("不存在的新目录应放行: %q %v", got, err)
	}
}

func TestMoveDistroFlow(t *testing.T) {
	target := filepath.Join(t.TempDir(), "moved")
	svc, ev, _ := newTestService()
	svc.runWsl = (&wslStub{resp: quietStub([]string{"Ubuntu"}, nil)}).run
	// 第一问：还在老地方；第二问（移动后复验）：已在新位置。
	var psCalls int
	svc.localPS = func(_ context.Context, script string) (string, error) {
		if !strings.HasPrefix(script, "$items") {
			return "", nil
		}
		psCalls++
		bp := filepath.Clean(`C:\old\u`)
		if psCalls > 1 {
			bp = filepath.Clean(target)
		}
		return fmt.Sprintf(`[{"name":"Ubuntu","basePath":%q,"vhdx":"","size":0}]`, bp), nil
	}

	out, err := svc.MoveDistro("Ubuntu", target)
	if err != nil || !out.Success {
		t.Fatalf("迁移失败: %+v %v", out, err)
	}
	if !strings.Contains(out.Message, "迁移完成") {
		t.Fatalf("复验通过应报完成: %s", out.Message)
	}
	last := ev.calls[len(ev.calls)-1]
	if last.file != "powershell.exe" {
		t.Fatalf("迁移必须走提权 powershell 单会话: %s", last.file)
	}
	inner := joined(last.args)
	for _, want := range []string{
		"wsl --shutdown",
		fmt.Sprintf("wsl --manage 'Ubuntu' --move '%s'", filepath.Clean(target)),
		"if ($LASTEXITCODE -eq 0) { exit 0 }",
		"$tries -ge 5",
	} {
		if !strings.Contains(inner, want) {
			t.Fatalf("提权脚本缺少 %q:\n%s", want, inner)
		}
	}

	// 复验不一致：如实降级报告，不硬报成功文案。
	svc2, _, _ := newTestService()
	svc2.runWsl = svc.runWsl
	svc2.localPS = func(context.Context, string) (string, error) {
		return `[{"name":"Ubuntu","basePath":"C:\\still\\old","vhdx":"","size":0}]`, nil
	}
	out2, err := svc2.MoveDistro("Ubuntu", target)
	if err != nil || !out2.Success || !strings.Contains(out2.Message, "不一致") {
		t.Fatalf("注册表复验失呼应如实说明: %+v %v", out2, err)
	}

	// UAC 取消：回执不成功也不报错（与提权通道既有语义一致）。
	svc3, ev3, _ := newTestService()
	svc3.runWsl = svc.runWsl
	svc3.localPS = svc2.localPS
	ev3.out = OperationOutcome{Success: false, Message: "已取消 UAC 授权，操作未执行"}
	out3, err := svc3.MoveDistro("Ubuntu", target)
	if err != nil || out3.Success || !strings.Contains(out3.Message, "UAC") {
		t.Fatalf("UAC 取消应如实回执: %+v %v", out3, err)
	}
}
