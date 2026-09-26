package version

import (
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：远程列表逐行 portable
// （findZipAsset 判据收 electron-builder win zip target——资产名 "Setup" 只是
// artifactName 模板字样，机械判名据此误标 installer，由解包直启、无 NSIS
// 卸载语义的安装链事实纠正；并列的 Paseo-Setup-<ver>.exe 才是安装器，本包
// 不收）。双通道各测一轮；缓存出口两分支均产出新切片，就地回填不触碰缓存源。
func TestListRemoteHostedForm(t *testing.T) {
	old, oldAt := remoteCache.data, remoteCache.fetchedAt
	defer func() { remoteCache.data, remoteCache.fetchedAt = old, oldAt }()
	remoteCache.data = []PaseoRelease{
		{Version: "0.9.1"},
		{Version: "0.9.0-beta.1", IsPre: true},
	}
	remoteCache.fetchedAt = time.Now()

	m := NewManager(t.TempDir())
	for _, c := range []struct {
		includePre bool
		want       int
	}{{false, 1}, {true, 2}} {
		got, err := m.ListRemote(c.includePre)
		if err != nil {
			t.Fatalf("ListRemote(%v): %v", c.includePre, err)
		}
		if len(got) != c.want {
			t.Fatalf("ListRemote(%v) 行数=%d want %d", c.includePre, len(got), c.want)
		}
		for _, rel := range got {
			if rel.Form != hostfeed.FormPortable {
				t.Errorf("%s Form=%q want portable（win zip 解包直启事实）", rel.Version, rel.Form)
			}
		}
	}
	for _, rel := range remoteCache.data {
		if rel.Form != "" {
			t.Errorf("回填污染缓存源: %s Form=%q", rel.Version, rel.Form)
		}
	}
}
