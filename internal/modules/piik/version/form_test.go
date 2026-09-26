package version

import (
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// seedRemote 注入伪远程列表（remoteCache 是包内全局缓存，生产语义即进程级
// 单例；本包测试串行执行，互相隔离靠各用例重灌，fetchedAt 置现在即 TTL 内，
// 全链零真网）。
func seedRemote(t *testing.T, list []PiikRelease) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = list
	remoteCache.fetchedAt = time.Now()
}

// TestListRemoteHostedForm N13 形态标注回填：远程列表逐行 portable，且
// 回填走"先拷贝再动"纪律——缓存源永不被就地污染（并发读互踩防线）。
func TestListRemoteHostedForm(t *testing.T) {
	seedRemote(t, []PiikRelease{
		{Version: "v1.2.0", AssetName: assetName, SHA256: strings.Repeat("a", 64)},
		{Version: "v1.1.0", AssetName: assetName, SHA256: strings.Repeat("b", 64)},
	})
	m := NewManager(t.TempDir())

	list, err := m.ListRemote()
	if err != nil {
		t.Fatalf("ListRemote: %v", err)
	}
	for _, r := range list {
		if r.Form != hostfeed.FormPortable {
			t.Errorf("%s 应回填 Form=portable，得 %q", r.Version, r.Form)
		}
	}

	// 缓存源不被污染
	remoteCache.mu.Lock()
	for _, r := range remoteCache.data {
		if r.Form != "" {
			remoteCache.mu.Unlock()
			t.Fatalf("回填就地污染了缓存源: %+v", r)
		}
	}
	remoteCache.mu.Unlock()

	// 调用方改写返回值也不回灌缓存
	list[0].Version = "mutated"
	again, _ := m.ListRemote()
	if again[0].Version != "v1.2.0" {
		t.Errorf("返回值改写回灌了缓存: %s", again[0].Version)
	}
}
