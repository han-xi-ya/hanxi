package version

import (
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：远程列表逐行 portable
// （上游 win 线唯一发布物即 LiteMonitor_<ver>-win-x64.zip 便携归档，机械
// 判名因无 portable 字样保守降 archive，由解包直启的安装链事实纠正）。
// 缓存命中路径 get 交回共享切片，ListRemote 必须先拷贝再回填，缓存源
// Form 恒空。
func TestListRemoteHostedForm(t *testing.T) {
	old, oldAt := remoteCache.data, remoteCache.fetchedAt
	defer func() { remoteCache.data, remoteCache.fetchedAt = old, oldAt }()
	remoteCache.data = []LMRelease{{Version: "v1.3.6"}, {Version: "v1.3.5"}}
	remoteCache.fetchedAt = time.Now()

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
			t.Errorf("%s Form=%q want portable（win-x64 便携 zip 直启事实）", rel.Version, rel.Form)
		}
	}
	for _, rel := range remoteCache.data {
		if rel.Form != "" {
			t.Errorf("回填污染缓存源: %s Form=%q", rel.Version, rel.Form)
		}
	}
}
