package version

import (
	"testing"
	"time"

	"hanxi/packages/go/hostfeed"
)

// TestListRemoteHostedForm N13 形态标注回填：远程列表逐行 installer
// （托管链为 NSIS /S /D 静默安装；机械判名 Recordly-windows-x64.exe 无
// setup 字样保守降 binary，由安装链事实纠正）。双通道各测一轮；回填只
// 作用于缓存出口新切片，缓存源 Form 恒空。
func TestListRemoteHostedForm(t *testing.T) {
	old, oldAt := remoteCache.data, remoteCache.fetchedAt
	defer func() { remoteCache.data, remoteCache.fetchedAt = old, oldAt }()

	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	remoteCache.data = list
	remoteCache.fetchedAt = time.Now()

	m := NewManager(t.TempDir())
	stableWant := 0
	for _, r := range list {
		if !r.IsPre {
			stableWant++
		}
	}

	for _, c := range []struct {
		includePre bool
		want       int
	}{{false, stableWant}, {true, len(list)}} {
		got, err := m.ListRemote(c.includePre)
		if err != nil {
			t.Fatalf("ListRemote(%v): %v", c.includePre, err)
		}
		if len(got) != c.want {
			t.Fatalf("ListRemote(%v) 行数=%d want %d", c.includePre, len(got), c.want)
		}
		for _, rel := range got {
			if rel.Form != hostfeed.FormInstaller {
				t.Errorf("%s Form=%q want installer（NSIS 安装链事实）", rel.Version, rel.Form)
			}
		}
	}
	for _, rel := range remoteCache.data {
		if rel.Form != "" {
			t.Errorf("回填污染缓存源: %s Form=%q", rel.Version, rel.Form)
		}
	}
}
