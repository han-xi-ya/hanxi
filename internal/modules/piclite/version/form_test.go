package version

import (
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：远程列表逐行 installer
// （findMSIAsset 判据恒收 PicLite_<ver>_x64_en-US.msi，机械判名按 .msi
// 扩展归 package，由选包判据与 msiexec 管理提取的安装链事实纠正为
// installer；上游现行并发的 Windows 便携 zip 不进收口）。缓存命中路径
// get 交回共享切片，ListRemote 必须先拷贝再回填，缓存源 Form 恒空。
func TestListRemoteHostedForm(t *testing.T) {
	old, oldAt := remoteCache.data, remoteCache.fetchedAt
	defer func() { remoteCache.data, remoteCache.fetchedAt = old, oldAt }()
	remoteCache.data = []PicRelease{{Version: "v1.8.6"}, {Version: "v1.8.5"}}
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
		if rel.Form != hostfeed.FormInstaller {
			t.Errorf("%s Form=%q want installer（MSI 管理提取事实）", rel.Version, rel.Form)
		}
	}
	for _, rel := range remoteCache.data {
		if rel.Form != "" {
			t.Errorf("回填污染缓存源: %s Form=%q", rel.Version, rel.Form)
		}
	}
}
