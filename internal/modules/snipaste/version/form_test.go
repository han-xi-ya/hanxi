package version

import (
	"net/http"
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：官网免安装版列表逐行 portable
// （Snipaste-X.Y.Z-x64.zip 即官网"免安装版"，解压直用；机械判名保守降
// archive 由事实纠正）。get 各路径交回克隆切片，回填不触碰缓存源。
func TestListRemoteHostedForm(t *testing.T) {
	cache := newReleaseCache(remoteSource{
		client: &http.Client{Timeout: time.Second}, downloadPage: "http://127.0.0.1:1", manifestURL: "http://127.0.0.1:1",
	})
	cache.data = []SnipasteRelease{{Version: "2.11.3"}, {Version: "2.9.2-Beta", IsPre: true}}
	cache.fetchedAt = time.Now()

	m := NewManager(t.TempDir())
	m.cache = cache
	list, err := m.ListRemote()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("TTL 命中行数=%d want 2", len(list))
	}
	for _, rel := range list {
		if rel.Form != hostfeed.FormPortable {
			t.Errorf("%s Form=%q want portable（官网免安装版事实）", rel.Version, rel.Form)
		}
	}
	for _, rel := range cache.data {
		if rel.Form != "" {
			t.Errorf("回填污染缓存源: %s Form=%q", rel.Version, rel.Form)
		}
	}
}
