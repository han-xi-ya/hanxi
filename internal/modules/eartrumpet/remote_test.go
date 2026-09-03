package eartrumpet

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseAppInstallerAcceptsOfficial(t *testing.T) {
	rel, err := parseAppInstaller([]byte(sampleAppInstaller))
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "2.3.0.20" || !strings.HasSuffix(rel.BundleURL, ".appxbundle") {
		t.Fatalf("解析结果错误: %+v", rel)
	}
	if len(rel.Dependencies) != 1 || !strings.HasPrefix(rel.Dependencies[0], "https://") {
		t.Fatalf("依赖解析错误: %+v", rel.Dependencies)
	}
}

func TestParseAppInstallerRejectsSpoofedManifest(t *testing.T) {
	cases := map[string]string{
		"发布者不符":    strings.Replace(sampleAppInstaller, managedIdentity.Publisher, "CN=Evil", 1),
		"包名不符":     strings.Replace(sampleAppInstaller, `Name="40459File-New-Project.EarTrumpet"`, `Name="Other.App"`, 1),
		"包地址非官方":   strings.Replace(sampleAppInstaller, "install.eartrumpet.app", "evil.example.com", 2),
		"依赖非https": strings.Replace(sampleAppInstaller, "https://aka.ms/", "http://aka.ms/", 1),
	}
	for name, doc := range cases {
		if _, err := parseAppInstaller([]byte(doc)); err == nil {
			t.Fatalf("%s 的清单应被拒绝", name)
		}
	}
}

func TestRemoteCacheTTLAndStaleFallback(t *testing.T) {
	var calls int
	getter := func(_ context.Context, rawURL string) ([]byte, error) {
		calls++
		if rawURL != appInstallerURL {
			return nil, errors.New("unexpected url")
		}
		return []byte(sampleAppInstaller), nil
	}
	var cache remoteCache
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		rel, err := cache.fetch(ctx, getter)
		if err != nil || rel.Version != "2.3.0.20" {
			t.Fatalf("第 %d 次 fetch: %+v %v", i, rel, err)
		}
	}
	if calls != 1 {
		t.Fatalf("TTL 内应只请求一次，实际 %d", calls)
	}

	// 缓存过期且网络失败 → 回退上次核验成功的快照
	cache.cachedAt = time.Now().Add(-2 * remoteCacheTTL)
	failGetter := func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
	rel, err := cache.fetch(ctx, failGetter)
	if err != nil || rel.Version != "2.3.0.20" {
		t.Fatalf("应回退缓存快照: %+v %v", rel, err)
	}
	if calls != 1 {
		t.Fatalf("回退不应更新缓存时间戳后再次穿透，calls=%d", calls)
	}
	// 回退后仍在有效期内，不再穿透网络
	if _, err := cache.fetch(ctx, failGetter); err != nil || calls != 1 {
		t.Fatalf("stale 快照应可复用: calls=%d err=%v", calls, err)
	}

	// 无缓存且网络失败 → 报错
	var fresh remoteCache
	if _, err := fresh.fetch(ctx, failGetter); err == nil {
		t.Fatal("无缓存且离线时应报错")
	}
}
