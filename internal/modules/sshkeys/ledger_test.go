package sshkeys

// 账本 / 导出 / 钉定三层的回归锁。重点是那几条"错了不会立刻看得见"的路径：
// 损坏不覆盖、撤销不吞用户配置、缺结束标记时中止、幂等不重复写盘。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

func goodHost(alias string) HostEntry {
	return HostEntry{
		Alias: alias, Host: "10.0.0.8", Port: 4090, User: "ops",
		KeyPath: "~/.ssh/" + alias, Note: "测试机", Enabled: true,
	}
}

// useFakeHome 把**家目录接缝本身**换成临时目录，返回其中的 `.ssh` 目录。
//
// 为什么替换 userHomeDir 而不是 sshHomeDir：台账扫描走 sshHomeDir，而"钥匙在不在本机"
// 的 `~` 展开走 expandHome——两处都源自 userHomeDir。只换一个就会出现"扫到钥匙、
// 展开到别处"的假阴性（本仓今天真就这么漏判过一次"被谁在用"）。
func useFakeHome(t *testing.T) string {
	t.Helper()
	fakeHome := t.TempDir()
	sshDir := filepath.Join(fakeHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("造假 .ssh 失败: %v", err)
	}
	prev := userHomeDir
	userHomeDir = func() (string, error) { return fakeHome, nil }
	t.Cleanup(func() { userHomeDir = prev })
	return sshDir
}

func TestLedgerRoundTripAndGuards(t *testing.T) {
	l := newLedger(t.TempDir())

	if got, err := l.list(); err != nil || len(got) != 0 {
		t.Fatalf("新账本应为空且无错，得 %+v err=%v", got, err)
	}
	if err := l.add(goodHost("srv-a")); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	// 别名唯一：重复别名会破坏渲染层"段头计数==项数"的自证前提。
	if err := l.add(goodHost("srv-a")); !errors.Is(err, ErrAliasExists) {
		t.Errorf("重复别名应报 ErrAliasExists，实得 %v", err)
	}
	// 校验必须真的委托给 sshconf（不是只在界面上挡一层）。
	for _, bad := range []HostEntry{
		{Alias: "bad name", Host: "1.1.1.1", Port: 22, User: "u", KeyPath: "~/k"},
		{Alias: "ok1", Host: "", Port: 22, User: "u", KeyPath: "~/k"},
		{Alias: "ok2", Host: "1.1.1.1", Port: 70000, User: "u", KeyPath: "~/k"},
		{Alias: "-lead", Host: "1.1.1.1", Port: 22, User: "u", KeyPath: "~/k"},
	} {
		if err := l.add(bad); err == nil {
			t.Errorf("非法记录应被拒：%+v", bad)
		}
	}
	if err := l.update(goodHost("not-there")); !errors.Is(err, ErrAliasNotFound) {
		t.Errorf("改不存在的别名应报 ErrAliasNotFound，实得 %v", err)
	}

	u := goodHost("srv-a")
	u.Note = "改过备注"
	if err := l.update(u); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	got, err := l.list()
	if err != nil || len(got) != 1 || got[0].Note != "改过备注" {
		t.Fatalf("更新未生效，得 %+v err=%v", got, err)
	}

	// 落盘后重开必须读得到同一份（jsonstore 原子写真的落到了盘）。
	l2 := newLedger(filepath.Dir(l.filePath))
	if again, err := l2.list(); err != nil || len(again) != 1 || again[0].Note != "改过备注" {
		t.Fatalf("重开账本读不回上次写入，得 %+v err=%v", again, err)
	}

	if err := l.remove("srv-a"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if err := l.remove("srv-a"); !errors.Is(err, ErrAliasNotFound) {
		t.Errorf("重复删除应报 ErrAliasNotFound，实得 %v", err)
	}
}

func TestLedgerCorruptRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	l := newLedger(dir)
	path := filepath.Join(dir, ledgerFileName)
	if err := os.WriteFile(path, []byte("{ 这不是合法 JSON"), 0o600); err != nil {
		t.Fatalf("布坏文件失败: %v", err)
	}
	before, _ := os.ReadFile(path)

	if _, err := l.list(); err == nil {
		t.Fatal("损坏账本应报错，不能当空账本放行")
	}
	if err := l.add(goodHost("srv-a")); err == nil {
		t.Fatal("损坏状态下新增必须被拒")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("损坏账本被覆盖了——用户会以为清单被清空，实为程序写坏")
	}
}

func TestExportIdempotentAndDrift(t *testing.T) {
	home := useFakeHome(t)
	l := newLedger(t.TempDir())
	if err := l.add(goodHost("srv-a")); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	off := goodHost("srv-off")
	off.Enabled = false
	if err := l.add(off); err != nil {
		t.Fatalf("新增失败: %v", err)
	}

	sum, err := l.exportConf()
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	confPath := filepath.Join(home, confFileName)
	raw, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("产物未落盘: %v", err)
	}
	if !strings.Contains(string(raw), "Host srv-a") || strings.Contains(string(raw), "Host srv-off") {
		t.Errorf("产物应只含启用项，实得：%s", raw)
	}
	if !strings.Contains(string(raw), "IdentitiesOnly yes") {
		t.Error("每段应自带 IdentitiesOnly（防客户端乱试其它钥匙）")
	}
	if sum.Rendered != 1 {
		t.Errorf("应渲染 1 段，实得 %d", sum.Rendered)
	}
	if sum.KeyMissing != 1 {
		t.Errorf("那把钥匙在本机不存在，KeyMissing 应为 1，实得 %d", sum.KeyMissing)
	}
	configRaw, err := os.ReadFile(filepath.Join(home, "config"))
	if err != nil || !strings.Contains(string(configRaw), includeLine) {
		t.Fatalf("顶层 config 应含 Include 行，得 %q err=%v", configRaw, err)
	}

	// 再点一次按钮：内容一致就不该重写（不产生新备份、不动 mtime）。
	infoBefore, _ := os.Stat(confPath)
	sum2, err := l.exportConf()
	if err != nil {
		t.Fatalf("二次导出失败: %v", err)
	}
	infoAfter, _ := os.Stat(confPath)
	if sum2.Written {
		t.Error("产物未变却报告了「写了」——反复点按钮会不断产生备份")
	}
	if !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Error("产物未变却重写了文件")
	}

	// 账本改动后必须报漂移。
	b := goodHost("srv-b")
	if err := l.add(b); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	st, err := l.exportStatus()
	if err != nil || !st.Drift {
		t.Fatalf("账本已改动，应报 Drift=true，得 %+v err=%v", st, err)
	}
}

// TestRemoveExportKeepsUserConfig 是本层最要紧的一条：撤销只许动自己的东西。
func TestRemoveExportKeepsUserConfig(t *testing.T) {
	home := useFakeHome(t)
	l := newLedger(t.TempDir())
	if err := l.add(goodHost("srv-a")); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	if _, err := l.exportConf(); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	// 在 config 里前后各塞一段用户自己的内容，模拟"你已经写过了"。
	configPath := filepath.Join(home, "config")
	userRaw, _ := os.ReadFile(configPath)
	withUser := "# 我自己的全局设定\nHost *\n    ServerAliveInterval 30\n\n" +
		string(userRaw) + "\n# 我自己手写的条目\nHost personal\n    User me\n"
	if err := os.WriteFile(configPath, []byte(withUser), 0o600); err != nil {
		t.Fatalf("写用户 config 失败: %v", err)
	}

	if _, err := l.removeExport(); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, confFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Error("撤销后产物应已删除")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("撤销竟然删掉了用户的 config: %v", err)
	}
	text := string(after)
	if strings.Contains(text, includeLine) || strings.Contains(text, includeMarkStart) {
		t.Error("Include 那段没摘干净")
	}
	for _, keep := range []string{"ServerAliveInterval 30", "Host personal", "User me"} {
		if !strings.Contains(text, keep) {
			t.Errorf("撤销吞掉了用户自己的配置片段 %q", keep)
		}
	}
}

// TestRemoveExportAbortsOnHandEditedBlock 验证"宁可不动"的护栏：
// 起始标记还在、结束标记被手删时，按"删到结尾"处理会连带吞掉用户配置。
func TestRemoveExportAbortsOnHandEditedBlock(t *testing.T) {
	home := useFakeHome(t)
	l := newLedger(t.TempDir())
	if err := l.add(goodHost("srv-a")); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	if _, err := l.exportConf(); err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	configPath := filepath.Join(home, "config")
	raw, _ := os.ReadFile(configPath)
	tampered := strings.Replace(string(raw), includeMarkEnd, "# 结束标记被手工删掉了", 1) + "\nHost mine\n    User me\n"
	if err := os.WriteFile(configPath, []byte(tampered), 0o600); err != nil {
		t.Fatalf("改文件失败: %v", err)
	}

	if _, err := l.removeExport(); err == nil {
		t.Fatal("缺结束标记时应中止而不是猜范围")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("中止时不该动文件: %v", err)
	}
	if !strings.Contains(string(after), "Host mine") {
		t.Error("护栏失效——用户配置被误删了")
	}
}

func TestPinsStore(t *testing.T) {
	dir := t.TempDir()
	p := newPinsStore(dir)
	addr := "10.0.0.8:4090"

	if got, err := p.Lookup(addr); err != nil || got != "" {
		t.Fatalf("首次应无记录，得 %q err=%v", got, err)
	}
	// 形态不合格的钉定必须拒收：脏数据进钉库会让 TOFU 变成摆设。
	if err := p.Record(addr, "md5:旧格式"); err == nil {
		t.Error("非 SHA256 指纹不该被钉下")
	}
	if err := p.Record(addr, "SHA256:abcdef"); err != nil {
		t.Fatalf("钉下失败: %v", err)
	}
	if got, _ := p.Lookup(addr); got != "SHA256:abcdef" {
		t.Errorf("读回不一致，得 %q", got)
	}

	reopened := newPinsStore(dir)
	if got, err := reopened.Lookup(addr); err != nil || got != "SHA256:abcdef" {
		t.Fatalf("重开读不回钉定，得 %q err=%v", got, err)
	}
	rows, err := reopened.All()
	if err != nil || len(rows) != 1 {
		t.Fatalf("All 应返回 1 条，得 %+v err=%v", rows, err)
	}
	if err := reopened.Reset(addr); err != nil {
		t.Fatalf("清钉失败: %v", err)
	}
	if got, _ := reopened.Lookup(addr); got != "" {
		t.Error("清钉后仍读得到")
	}
	if err := reopened.Reset("没钉过的地址"); err != nil {
		t.Errorf("清一个不存在的钉不该报错（撤销要能重复点）: %v", err)
	}
}

func TestValidateEntryRejectsRenderHostileInput(t *testing.T) {
	l := newLedger(t.TempDir())
	// 换行别名 = ssh 配置注入面。这一条走的是真渲染校验，不是界面层自欺。
	if err := l.add(HostEntry{Alias: "a\n    ProxyCommand rm -rf", Host: "1.1.1.1", Port: 22, User: "u", KeyPath: "~/k", Enabled: true}); err == nil {
		t.Fatal("含换行的别名必须被拒")
	}
	// 拒了就不该留下半条记录。
	if got, _ := l.list(); len(got) != 0 {
		t.Errorf("非法新增后账本应为空，实得 %+v", got)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("本机取不到用户主目录: %v", err)
	}
	if got := expandHome("~/keys/id"); got != filepath.Join(home, "keys", "id") {
		t.Errorf("~ 应展开成家目录，实得 %q", got)
	}
	if got := expandHome("/abs/id"); got != "/abs/id" {
		t.Errorf("绝对路径不该被改写，实得 %q", got)
	}
	if got := expandHome("~other/id"); got != "~other/id" {
		t.Errorf("~user 形态不该当成家目录展开，实得 %q", got)
	}
}

// TestRemoveExportLeavesNoResidue 钉 2026-10-23 机主实测到的两件事：
// 撤销后 ~/.ssh 里留着 config.hanxi.bak 与 hanxi.conf.hanxi.bak，且那个 config
// 本就是「同步给 ssh」时代码创建的（用户原本没有）——撤销后留下一个 0 字节壳。
// 现在的判据：我们建的、且摘完不剩任何用户内容的 config 一并移除，垃圾 .bak 清空。
func TestRemoveExportLeavesNoResidue(t *testing.T) {
	home := useFakeHome(t)
	l := newLedger(t.TempDir())
	if err := l.add(goodHost("srv-a")); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	if _, err := l.exportConf(); err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, confFileName)); err != nil {
		t.Fatalf("导出后产物应存在: %v", err)
	}

	if _, err := l.removeExport(); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	for _, leftover := range []string{
		confFileName,
		confFileName + backupSuffix,
		"config",
		"config" + backupSuffix,
	} {
		p := filepath.Join(home, leftover)
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("撤销后仍残留 %s（撤销的语义是回到没碰过的状态）", leftover)
		}
	}
}

// TestRemoveExportKeepsUserConfigWithBackup 是对侧：用户自己写过东西时，
// config 与改动前的快照都必须留着——"不残留垃圾"不能变成"顺手删别人的东西"。
func TestRemoveExportKeepsUserConfigWithBackup(t *testing.T) {
	home := useFakeHome(t)
	l := newLedger(t.TempDir())
	if err := l.add(goodHost("srv-a")); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	if _, err := l.exportConf(); err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	configPath := filepath.Join(home, "config")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读 config 失败: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("Host personal\n    User me\n\n"+string(raw)), 0o600); err != nil {
		t.Fatalf("写用户内容失败: %v", err)
	}

	sum, err := l.removeExport()
	if err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("用户写过内容的 config 被删了——不可接受")
	}
	if !strings.Contains(string(after), "Host personal") {
		t.Error("用户自己的段丢了")
	}
	if strings.Contains(string(after), includeMarkStart) {
		t.Error("我们那段没摘掉")
	}
	if _, err := os.Stat(configPath + backupSuffix); err != nil {
		t.Error("改动用户文件前的快照应当保留（这是唯一的回滚凭据）")
	}
	if !strings.Contains(sum.Message, "Include") {
		t.Errorf("回执应说明摘掉了 Include，实得 %q", sum.Message)
	}
}

// TestExportSummaryNamesMissingKeysExactly 锁"由后端给确切名单"这件事。
// 三种行必须区分开：指向不存在文件（缺失）、指向 .ssh 之外且真实存在（**不缺失**，
// 而前端只看台账基名会把它误标成找不到）、没填密钥路径（语义不同，也不算缺失）。
func TestExportSummaryNamesMissingKeysExactly(t *testing.T) {
	useFakeHome(t)
	elsewhere := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(elsewhere, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("写盘外钥匙失败: %v", err)
	}

	l := newLedger(t.TempDir())
	mk := func(alias, keyPath string) {
		h := goodHost(alias)
		h.KeyPath = keyPath
		if err := l.add(h); err != nil {
			t.Fatalf("入册 %s 失败: %v", alias, err)
		}
	}
	mk("gone", "~/.ssh/nope")
	mk("elsewhere", elsewhere)
	mk("nokey", "")

	sum, err := l.exportStatus()
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if sum.KeyMissing != 1 {
		t.Errorf("只应有 1 条真缺失，实得 %d（名单 %v）", sum.KeyMissing, sum.MissingKeyAliases)
	}
	if len(sum.MissingKeyAliases) != 1 || sum.MissingKeyAliases[0] != "gone" {
		t.Errorf("名单应只含 gone，实得 %v", sum.MissingKeyAliases)
	}
}

// TestLedgerAutoAlias 锁"别名留空即由地址派生"，以及重名去重后仍是合法别名。
func TestLedgerAutoAlias(t *testing.T) {
	l := newLedger(t.TempDir())

	blank := goodHost("")
	blank.Alias = ""
	if err := l.add(blank); err != nil {
		t.Fatalf("空别名应自动派生: %v", err)
	}
	got, _ := l.list()
	if len(got) != 1 || got[0].Alias != "10-0-0-8" {
		t.Fatalf("派生别名不对：%+v", got)
	}

	again := goodHost("")
	again.Alias = ""
	if err := l.add(again); err != nil {
		t.Fatalf("同地址第二台应派生出去重名: %v", err)
	}
	got, _ = l.list()
	if len(got) != 2 || got[1].Alias != "10-0-0-8-2" {
		t.Fatalf("去重结果不对：%+v", got)
	}

	// 派生不出合法值时要求显式命名，且不许留下半条记录。
	bad := goodHost("")
	bad.Alias, bad.Host = "", "服务器"
	if err := l.add(bad); err == nil {
		t.Fatal("纯中文地址应要求显式命名")
	}
	got, _ = l.list()
	if len(got) != 2 {
		t.Errorf("失败的新增不该进账，实有 %+v", got)
	}

	// 显式给了别名就照给的走，重名仍然拒——两条路径共用同一份规则。
	if err := l.add(goodHost("10-0-0-8")); err == nil {
		t.Error("显式别名重名必须被拒")
	}
}

// TestSuggestAliasRPCMatchesLedger 锁"预览与落账同源"：预览说什么，保存后就该是什么
// （含冲突时的 -2 后缀由账本决定，所以只断言无冲突那一档）。
func TestSuggestAliasRPCMatchesLedger(t *testing.T) {
	svc := &SshKeysService{prober: newProber(), holder: extapi.NewLeaseHolder(ID), ledger: newLedger(t.TempDir())}
	pre, err := svc.SuggestAlias("10.0.0.8")
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if pre != "10-0-0-8" {
		t.Fatalf("预览值不对: %q", pre)
	}
	h := goodHost("")
	h.Alias = ""
	if _, err := svc.AddHost(h); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	list, err := svc.ListHosts()
	if err != nil || len(list) != 1 || list[0].Alias != pre {
		t.Fatalf("落账别名与预览不一致：%+v err=%v", list, err)
	}
	if _, err := svc.SuggestAlias("服务器"); err == nil {
		t.Error("派生不出时应报错要求显式命名，前端据此提示")
	}
}

// TestExportDoesNotLitterProductBackup 钉 2026-10-24 那条：产物是可重生成的渲染结果，
// 给它留 .bak 只是在用户 ~/.ssh 里多放一份清单副本（别名/地址/用户/密钥路径都在里面），
// 既不是退路又可能外泄。新版写完还要顺带清掉旧版留下的那份。
func TestExportDoesNotLitterProductBackup(t *testing.T) {
	home := useFakeHome(t)
	l := newLedger(t.TempDir())
	if err := l.add(goodHost("srv-a")); err != nil {
		t.Fatalf("新增失败: %v", err)
	}
	if _, err := l.exportConf(); err != nil {
		t.Fatalf("首次导出失败: %v", err)
	}

	// 伪造旧版本留下的产物备份，验证新版写入时会一并清掉。
	stale := filepath.Join(home, confFileName+backupSuffix)
	if err := os.WriteFile(stale, []byte("旧版留下的产物备份"), 0o600); err != nil {
		t.Fatalf("伪造残留失败: %v", err)
	}

	// 改账本逼一次真实写入（内容一致时 exportConf 会短路，不产文件动作）。
	updated := goodHost("srv-a")
	updated.Note = "改过"
	if err := l.update(updated); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	sum, err := l.exportConf()
	if err != nil {
		t.Fatalf("二次导出失败: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Error("旧版残留的产物 .bak 应被清掉")
	}
	if sum.BackupPath != "" {
		t.Errorf("产物不该再产生备份，实得 %q", sum.BackupPath)
	}
}
