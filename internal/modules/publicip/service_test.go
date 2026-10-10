package publicip

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
	"hanxi/internal/platform/apppackage"

	"hanxi/packages/go/netx"
)

// 本组测试钉死 2026-09-27 两项如实性修复：
//  1. 公网 IP 全源失败必须如实报错且不写缓存（此前吞错+空结果入缓存钉死 2 分钟）；
//  2. Ping 的 TTL 不再编造 64（platform 契约不回 TTL，如实置零且 JSON 缺省）。

// ---------- 平台打桩（与 portkill/service_test.go 同款口径：只实装本包触达的路） ----------

// fakePlat 仅实装 Network()，其余恒 nil/不支持。
type fakePlat struct {
	net platform.NetworkAPI
}

func (f fakePlat) Network() platform.NetworkAPI               { return f.net }
func (f fakePlat) Port() platform.PortAPI                     { return nil }
func (f fakePlat) Process() platform.ProcessAPI               { return nil }
func (f fakePlat) Job() platform.JobAPI                       { return nil }
func (f fakePlat) KeepAwake() platform.KeepAwakeAPI           { return nil }
func (f fakePlat) AppPackage() apppackage.API                 { return nil }
func (f fakePlat) DesktopDir() (string, error)                { return "", platform.ErrNotSupported }
func (f fakePlat) CreateDesktopShortcut(_, _, _ string) error { return platform.ErrNotSupported }
func (f fakePlat) OpenURL(string) error                       { return nil }

// fakeNet：Adapters 回预置值；Ping 恒回预置三元组并计数。
type fakeNet struct {
	adapters    []platform.Adapter
	adaptersErr error
	pingRTT     time.Duration
	pingOK      bool
	pingErr     error
	pingCalls   atomic.Int32
}

func (n *fakeNet) Adapters() ([]platform.Adapter, error) { return n.adapters, n.adaptersErr }
func (n *fakeNet) DefaultAdapter() (*platform.Adapter, error) {
	return nil, platform.ErrNotSupported
}
func (n *fakeNet) NeighborTable() ([]platform.Neighbor, error) { return nil, nil }
func (n *fakeNet) Ping(context.Context, string, time.Duration) (time.Duration, bool, error) {
	n.pingCalls.Add(1)
	return n.pingRTT, n.pingOK, n.pingErr
}

// ---------- 测试装配 ----------

// newTestService：出网客户端换 netx.DirectClient——回环 httptest 靶过任何代理都失真
// （仓内铁律：访问 127.0.0.1 的 http.Client 必须 Proxy:nil）。调用门不注入即放行。
func newTestService(plat platform.Platform) *PublicIPService {
	svc := NewPublicIPService(plat, extapi.NewLeaseHolder(ID))
	svc.client = netx.DirectClient(2 * time.Second)
	return svc
}

// useTestProviders 覆盖包级查询源列表并在测试结束后还原（同包白盒，测试间串行执行）。
func useTestProviders(t *testing.T, v4, v6 []string) {
	t.Helper()
	oldV4, oldV6 := ipv4Providers, ipv6Providers
	ipv4Providers, ipv6Providers = v4, v6
	t.Cleanup(func() { ipv4Providers, ipv6Providers = oldV4, oldV6 })
}

// stubServer 起一个恒回固定 body 的查询源靶，返回地址与访问计数。
func stubServer(t *testing.T, body string) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

// deadServer 起后立即关闭：得到"连不上"的源地址，模拟源全站宕机。
func deadServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

// cachedState 读服务内部缓存状态（白盒断言"失败不写缓存"）。
func cachedState(svc *PublicIPService) (time.Time, NetworkOverview) {
	svc.cacheMu.RLock()
	defer svc.cacheMu.RUnlock()
	return svc.cachedTime, svc.cachedData
}

var okAdapters = []platform.Adapter{{Index: 1, Name: "以太网", IPv4: []string{"192.168.1.10"}, IsPhysical: true, IsUp: true}}

// ---------- (a) 全源失败：错误如实、不写缓存、二次调用重新探测 ----------

func TestGetNetworkOverviewAllSourcesFail(t *testing.T) {
	garbageURL, _ := stubServer(t, "this is definitely not an ip")
	dead := deadServer(t)
	useTestProviders(t, []string{garbageURL, dead}, []string{dead})

	svc := newTestService(fakePlat{net: &fakeNet{adapters: okAdapters}})

	ov, err := svc.GetNetworkOverview(false)
	if err == nil {
		t.Fatalf("全源失败必须返回错误，实际返回了成功假象: %+v", ov)
	}
	if !strings.Contains(err.Error(), "公网 IP 探测全部失败") {
		t.Errorf("错误口径不符，期望含『公网 IP 探测全部失败』，实际: %v", err)
	}
	// 各源最后错误如实保留：聚合里必须点得出是哪个源怎么挂的
	if !strings.Contains(err.Error(), garbageURL) {
		t.Errorf("聚合错误应包含失败源 URL %s 以供定位，实际: %v", garbageURL, err)
	}
	if ov.PublicIPv4 != "" || ov.SourceV4 != "" || ov.Adapters != nil {
		t.Errorf("全源失败应回零值概览（错误路径不半给数据），实际: %+v", ov)
	}

	// 失败结果不得入缓存
	if ct, cd := cachedState(svc); !ct.IsZero() {
		t.Errorf("全源失败后 cachedTime 应仍为零值，实际已写入缓存: %v %+v", ct, cd)
	}

	// 下一次调用正常重试：换成好源必须立刻探得到（若空结果曾被缓存，此处会读回缓存而永不重试）
	good4, hits4 := stubServer(t, "1.2.3.4\n")
	good6, hits6 := stubServer(t, "Current IP: 2001:db8::1\n")
	ipv4Providers, ipv6Providers = []string{good4}, []string{good6}

	ov2, err := svc.GetNetworkOverview(false)
	if err != nil {
		t.Fatalf("失败未缓存，二次调用应重新探测成功，实际: %v", err)
	}
	if ov2.PublicIPv4 != "1.2.3.4" || ov2.SourceV4 != good4 {
		t.Errorf("IPv4 探测结果不符: %+v", ov2)
	}
	if ov2.PublicIPv6 != "2001:db8::1" || ov2.SourceV6 != good6 {
		t.Errorf("IPv6 探测结果不符: %+v", ov2)
	}
	if hits4.Load() != 1 || hits6.Load() != 1 {
		t.Errorf("重试应各源恰好访问一次，实际 v4=%d v6=%d", hits4.Load(), hits6.Load())
	}
}

// ---------- (b) 部分源成功：按降级成功如实缓存 ----------

func TestGetNetworkOverviewPartialSuccessCaches(t *testing.T) {
	good4, hits4 := stubServer(t, "9.9.9.9")
	dead6 := deadServer(t)
	garbage6, _ := stubServer(t, "no ipv6 available here")
	useTestProviders(t, []string{good4}, []string{dead6, garbage6})

	svc := newTestService(fakePlat{net: &fakeNet{adapters: okAdapters}})

	ov, err := svc.GetNetworkOverview(false)
	if err != nil {
		t.Fatalf("IPv4 成功+IPv6 全挂属降级成功（家宽无 IPv6 是常态），不应报错: %v", err)
	}
	if ov.PublicIPv4 != "9.9.9.9" || ov.SourceV4 != good4 {
		t.Errorf("IPv4 结果不符: %+v", ov)
	}
	if ov.PublicIPv6 != "" || ov.SourceV6 != "" {
		t.Errorf("IPv6 全挂应如实留空（前端按空值表达未获得）: %+v", ov)
	}
	if len(ov.Adapters) != 1 {
		t.Errorf("网卡详情应随概览带回，实际: %+v", ov.Adapters)
	}

	// 降级成功必须写缓存
	ct, _ := cachedState(svc)
	if ct.IsZero() {
		t.Fatal("部分成功的正常结果应写入缓存")
	}

	// 缓存命中：不重复出网
	if _, err := svc.GetNetworkOverview(false); err != nil {
		t.Fatalf("TTL 内应命中缓存: %v", err)
	}
	if hits4.Load() != 1 {
		t.Errorf("命中缓存不应再次访问源，实际访问 %d 次", hits4.Load())
	}

	// 强制刷新重新探测
	if _, err := svc.GetNetworkOverview(true); err != nil {
		t.Fatalf("forceRefresh 应重新探测: %v", err)
	}
	if hits4.Load() != 2 {
		t.Errorf("forceRefresh 后应重新访问源，实际访问 %d 次", hits4.Load())
	}
}

// ---------- (c) Ping TTL 不再恒 64：如实置零且 JSON 缺省 ----------

func TestPingTargetTTLNotFabricated(t *testing.T) {
	fn := &fakeNet{pingRTT: 12 * time.Millisecond, pingOK: true}
	svc := newTestService(fakePlat{net: fn})

	// 目标用 IP 字面量，避免测试触达真实 DNS；Ping 走 fakeNet 不出网
	summary, err := svc.PingTarget("127.0.0.1", 2)
	if err != nil {
		t.Fatalf("PingTarget 失败: %v", err)
	}
	if summary.Received != 2 || fn.pingCalls.Load() != 2 {
		t.Fatalf("应收到 2 个模拟回包，实际 received=%d calls=%d", summary.Received, fn.pingCalls.Load())
	}
	for _, r := range summary.Results {
		if !r.Success {
			t.Fatalf("模拟回包应为成功: %+v", r)
		}
		if r.TTL != 0 {
			t.Errorf("TTL=%d，platform 不回 TTL 必须如实置零（旧口径编造 64）", r.TTL)
		}
	}

	// JSON 形态：零值 TTL 经 omitempty 缺省——前端 TTL 列成功行留白，不出现假数据
	b, err := json.Marshal(summary.Results[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"ttl"`) {
		t.Errorf("ttl 零值应从 JSON 缺省，实际: %s", b)
	}
}
