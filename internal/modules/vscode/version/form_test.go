package version

import (
	"testing"
)

// TestManagerListRemoteFormAnnotation N13 形态标注回填（双形态方言行）：
// manager.ListRemote 按查询形态逐行回填 Release.Form（portable/installer
// 词值与 hostfeed.Form 全家族词表逐字同名）；未知形态与 platformOf 同口径
// 归便携链，标注随之归一不写脏词。缓存共享切片先拷贝再回填——包级缓存行
// Form 恒空（resolveDownloadable 直读缓存，不得被展示字段污染）。
func TestManagerListRemoteFormAnnotation(t *testing.T) {
	newFakeUpstream(t, []string{"1.136.1", "1.136.0"}, fakeSHA)
	resetRemoteCaches()
	t.Cleanup(resetRemoteCaches)

	m := NewManager(t.TempDir())

	portable, err := m.ListRemote(FormPortable)
	if err != nil {
		t.Fatal(err)
	}
	if len(portable) == 0 {
		t.Fatal("便携列表为空")
	}
	for _, rel := range portable {
		if rel.Form != FormPortable {
			t.Errorf("便携行 %s Form=%q want portable", rel.Version, rel.Form)
		}
	}

	installer, err := m.ListRemote(FormInstaller)
	if err != nil {
		t.Fatal(err)
	}
	if len(installer) == 0 {
		t.Fatal("安装器列表为空")
	}
	for _, rel := range installer {
		if rel.Form != FormInstaller {
			t.Errorf("安装器行 %s Form=%q want installer", rel.Version, rel.Form)
		}
	}

	// 未知形态入参：取数走便携链，标注同口径归一为 portable（不透传脏词）
	weird, err := m.ListRemote(Form("weird"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range weird {
		if rel.Form != FormPortable {
			t.Errorf("未知形态应归一 portable，Got %q", rel.Form)
		}
	}

	if portableCache.data[0].Form != "" || installerCache.data[0].Form != "" {
		t.Error("回填污染缓存源（共享切片未断开引用）")
	}
}
