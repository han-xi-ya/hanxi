package portscan

// 真机代理验证夹具。默认 skip——**凭据不入仓库**，也不让 CI 依赖任何外部代理。
// 机主本机跑一次即可拿到"这套结论对不对得上现实"的证据：
//
//	HANXI_LIVE_PROXY='socks5h://user:pass@host:1080' go test ./internal/modules/portscan -run Live -v
//
// 姿势沿用 heukms 的 HANXI_HEU_RAR 先例（见 docs/TROUBLESHOOTING.md #108）：真实样本
// 由环境变量指进来，缺省 t.Skip，绝不把凭据/外网依赖固化进用例。

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/netx"
)

const liveProxyEnv = "HANXI_LIVE_PROXY"

// liveRequest 把 env 里那条 URL 拆成结构化入参（表单就是这套字段）。
func liveRequest(t *testing.T, raw string, withCreds bool) ProxyCheckRequest {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", liveProxyEnv, err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("%s 需写成 scheme://[user:pass@]host:port（当前缺端口？）", liveProxyEnv)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("端口不是数字: %q", portStr)
	}
	kind := strings.ToLower(u.Scheme)
	if kind == "socks5h" {
		kind = "socks5" // x/net 恒为远端解析，两写法同义，见 netx.ParseProxy 注释
	}
	req := ProxyCheckRequest{Kind: kind, Host: host, Port: port, TimeoutMs: 12000}
	if withCreds && u.User != nil {
		req.User = u.User.Username()
		req.Pass, _ = u.User.Password()
	}
	return req
}

func TestLiveProxyUsable(t *testing.T) {
	raw := os.Getenv(liveProxyEnv)
	if raw == "" {
		t.Skipf("未设 %s：跳过真机验证（用法见本文件注释）", liveProxyEnv)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.User == nil {
		t.Skip("该地址不带凭据，跑不出认证路径的结论；请给 user:pass@host:port 形态")
	}

	res, err := NewScanner().CheckProxy(context.Background(), liveRequest(t, raw, true))
	if err != nil {
		t.Fatalf("参数应合法: %v", err)
	}

	t.Logf("代理    : %s（剥凭据形）", res.Addr)
	t.Logf("判通    : %v，总耗时 %dms", res.OK, res.LatencyMs)
	if res.OK {
		t.Logf("出口 IP : %s（来自 %s）", res.EgressIP, res.EgressSource)
	} else {
		t.Logf("原因    : %s", res.Error)
	}

	// 逐源明细：真机最有用的一段。两个源给出不同 IP 是常态（源侧多机房、代理
	// 端 NAT 池轮转），所以本工具从不拿"出口 IP 变没变"当通断判据。
	t.Logf("---- 各出口源分别拿到的 IP ----")
	for _, p := range res.Probes {
		host := p.Source
		if parsed, perr := url.Parse(p.Source); perr == nil {
			host = parsed.Host
		}
		if p.IP != "" {
			t.Logf("  %-22s %s  (%dms)", host, p.IP, p.LatencyMs)
		} else {
			t.Logf("  %-22s 失败: %s  (%dms)", host, p.Error, p.LatencyMs)
		}
	}

	if !res.OK {
		t.Fatalf("真机代理判不通——要么它此刻确实挂了，要么这套判定有错，两种都得查清")
	}
	if res.EgressIP == "" {
		t.Error("判通却没给出出口 IP，结论不完整")
	}
	if len(res.Probes) != len(proxyCheckEgressProviders) {
		t.Errorf("每个源都该留一条结果，实得 %d 条", len(res.Probes))
	}

	// 对照：同一台代理不给凭据必须判不通。这条验的是"我们真的在认证"，
	// 而不是凭据被忽略后碰巧走了别的路拿到 IP（那样最危险：会把坏代理报成好代理）。
	res2, err := NewScanner().CheckProxy(context.Background(), liveRequest(t, raw, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("不给凭据重测: 判通=%v，耗时 %dms，原因=%s", res2.OK, res2.LatencyMs, res2.Error)
	if res2.OK {
		t.Errorf("不给凭据仍判通——认证根本没起作用（或该代理其实不要认证），结论可信度存疑")
	}
	if strings.Contains(res2.Error, "Admin") || strings.Contains(res2.Addr, "@") {
		t.Errorf("对照测的结果里出现了凭据痕迹: addr=%q err=%q", res2.Addr, res2.Error)
	}
}

// TestLiveEgressBaseline 把直连出口与代理出口并排打出来，不设断言：
// 同 NAT/CGNAT 池、ECMP 多出口、v4 与 v6 族不齐都会让"相同/不同"没有定性意义
// （机主本机实况：v6 进 v4 出，两源还各给一个不同 IP）。
func TestLiveEgressBaseline(t *testing.T) {
	raw := os.Getenv(liveProxyEnv)
	if raw == "" {
		t.Skipf("未设 %s", liveProxyEnv)
	}

	ctx := context.Background()
	t.Log("---- 直连（不经任何代理）----")
	logLiveSources(t, netx.DirectClient(5*time.Second), ctx)

	p, err := netx.ParseProxy(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, proxyClient, err := netx.DialProxy(p, netx.DialOpts{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("---- 经代理 ----")
	logLiveSources(t, proxyClient, ctx)
}

// logLiveSources 逐源打印各自拿到的出口 IP——同一台代理下不同源给出不同 IP 是常态，
// 摊开看才知道分歧来自源侧还是代理侧。
func logLiveSources(t *testing.T, client *http.Client, ctx context.Context) {
	t.Helper()
	for _, source := range proxyCheckEgressProviders {
		ip, used, err := netx.QueryEgress(ctx, client, []string{source})
		if err != nil {
			t.Logf("  %-22s 失败: %v", sourceHost(source), err)
			continue
		}
		t.Logf("  %-22s %s（实际命中 %s）", sourceHost(source), ip, used)
	}
}

func sourceHost(source string) string {
	if u, err := url.Parse(source); err == nil {
		return u.Host
	}
	return source
}
