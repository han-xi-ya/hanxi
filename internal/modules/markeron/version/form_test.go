package version

import (
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：远程列表逐行 portable
// （上游 win 侧安装器并立 -setup.exe / _x64-setup.nsis.zip / _zh-CN.msi，
// findPortableAsset 判据恒收 _x64_portable.zip，机械判名与安装链事实一致）。
// 缓存命中路径 get 交回共享切片，ListRemote 必须先拷贝再回填，缓存源
// Form 恒空。
func TestListRemoteHostedForm(t *testing.T) {
	old, oldAt := remoteCache.data, remoteCache.fetchedAt
	defer func() { remoteCache.data, remoteCache.fetchedAt = old, oldAt }()
	remoteCache.data = []MarkerRelease{{Version: "v2.10.1"}, {Version: "v2.9.4"}}
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
			t.Errorf("%s Form=%q want portable（_x64_portable.zip 直启事实）", rel.Version, rel.Form)
		}
	}
	for _, rel := range remoteCache.data {
		if rel.Form != "" {
			t.Errorf("回填污染缓存源: %s Form=%q", rel.Version, rel.Form)
		}
	}
}
