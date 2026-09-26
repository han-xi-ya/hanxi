package version

import (
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：远程列表逐行 portable
// （五资产并列的 QuickLook-<ver>.zip 是唯一免安装便携包，portable.lock
// 实测在场；资产名无便携字样，机械判名 Classify 保守降 archive，由保布局
// 解包直启的安装链事实纠正）。缓存命中路径 get 交回共享切片，ListRemote
// 必须先拷贝再回填，缓存源 Form 恒空。
func TestListRemoteHostedForm(t *testing.T) {
	old, oldAt := remoteCache.data, remoteCache.fetchedAt
	defer func() { remoteCache.data, remoteCache.fetchedAt = old, oldAt }()
	remoteCache.data = []QuickLookRelease{{Version: "4.5.0"}, {Version: "4.5.0.1"}}
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
			t.Errorf("%s Form=%q want portable（zip 解压直启事实）", rel.Version, rel.Form)
		}
	}
	for _, rel := range remoteCache.data {
		if rel.Form != "" {
			t.Errorf("回填污染缓存源: %s Form=%q", rel.Version, rel.Form)
		}
	}
}
