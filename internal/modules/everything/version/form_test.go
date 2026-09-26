package version

import (
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：远程槽位逐行 portable（官网 x64
// 资产即 "Download Portable ZIP" 便携 zip，解压直用）。本模块 releaseCache.get
// 命中路径交回共享切片（区别于 snipaste 的克隆切片），ListRemote 必须先 copy
// 再回填——钉缓存源与内置快照恒空 Form，回填不落共享底层数组。
// 本包测试串行执行（不使用 t.Parallel），remoteCache 全局注入沿 seedRemote 口径。
func TestListRemoteHostedForm(t *testing.T) {
	remoteCache.mu.Lock()
	remoteCache.data = []EverythingRelease{
		{Version: "1.4.1.1032", Channel: "stable"},
		{Version: "1.5.0.1422b", Channel: "beta"},
	}
	remoteCache.fetchedAt = time.Now() // TTL 命中，不触网
	remoteCache.mu.Unlock()

	m := NewManager(t.TempDir())
	list, err := m.ListRemote()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("TTL 命中行数=%d want 2", len(list))
	}
	for _, rel := range list {
		if rel.Form != hostfeed.FormPortable {
			t.Errorf("%s Form=%q want portable（官网便携 zip 事实）", rel.Version, rel.Form)
		}
	}
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	for _, rel := range remoteCache.data {
		if rel.Form != "" {
			t.Errorf("回填污染缓存源: %s Form=%q", rel.Version, rel.Form)
		}
	}
	for _, rel := range snapshotReleases {
		if rel.Form != "" {
			t.Errorf("回填污染内置快照: %s Form=%q", rel.Version, rel.Form)
		}
	}
}
