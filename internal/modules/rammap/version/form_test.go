package version

import (
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：单条"最新版"行 Form=portable
// （官方 RAMMap.zip 为绿色单文件 exe 投递容器，解压平铺直用；机械判名保守
// 降 other/archive 由托管安装链事实纠正）。TTL 命中时 remoteCache.get 交回
// 缓存共享切片，ListRemote 必须先拷贝再回填——缓存源 Form 恒空即克隆纪律
// 成立（Download 降级链同读该缓存，不得被展示字段污染）。
func TestListRemoteHostedForm(t *testing.T) {
	old, oldAt, oldSize := remoteCache.data, remoteCache.fetchedAt, remoteCache.size
	defer func() { remoteCache.data, remoteCache.fetchedAt, remoteCache.size = old, oldAt, oldSize }()
	remoteCache.data = []RammapRelease{{Version: "2026-03-26", AssetName: "RAMMap.zip"}}
	remoteCache.size = 1024
	remoteCache.fetchedAt = time.Now()

	m := NewManager(t.TempDir())
	list, err := m.ListRemote()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("TTL 命中行数=%d want 1", len(list))
	}
	if list[0].Form != hostfeed.FormPortable {
		t.Errorf("Form=%q want portable（绿色 exe 解压直用事实）", list[0].Form)
	}
	if remoteCache.data[0].Form != "" {
		t.Errorf("回填污染缓存源: Form=%q", remoteCache.data[0].Form)
	}
}
