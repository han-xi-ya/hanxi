// check_update_test.go 钉死 extapi.UpdateChecker 契约面：日更风暴下
// "远程失败上抛 ≠ 无更新"的红线、稳定通道取首条、未安装走交付路径。
// 全程复用 remoteCache 注入（fetchedAt 置现在 = TTL 内零真网）。
package version

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/packages/go/artifact"
)

// installFakeVersion 直接向版本树落一个布局完整、账本可信的"已装版本"
// （与远程缓存解耦——CheckUpdate 用例里"已装版本可能不在远程列表"的异常态
// 必须能表达）。
func installFakeVersion(t *testing.T, m *Manager, token string) {
	t.Helper()
	staging, discard, err := m.tree.StageDir("fake-" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer discard()
	for _, rel := range []string{exeName, "LICENSE", revisionFileName, "NOTICES", captureExeRel, cloudflaredExeRel} {
		p := filepath.Join(staging, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("fake-"+rel+"-bytes"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.tree.Commit(staging, token, artifact.Meta{Entry: exeName, Source: artifact.SourceRemote}); err != nil {
		t.Fatal(err)
	}
}

func seedUpdateFixture(t *testing.T, remoteVersions []string, installVersion string) *Manager {
	t.Helper()
	body := buildPiikZip(t, "fake-piik-app")
	m := newChainManager(t, newZipSource(t))

	var list []PiikRelease
	for _, v := range remoteVersions {
		r := seedReleaseFor(body)
		r.Version = v
		list = append(list, r)
	}
	seedRemote(t, list)
	if installVersion != "" {
		installFakeVersion(t, m, strings.TrimPrefix(installVersion, "v"))
	}
	return m
}

func TestCheckUpdateMatrix(t *testing.T) {
	cases := []struct {
		name       string
		remote     []string
		install    string
		wantLocal  string
		wantRemote string
		wantHasUpd bool
	}{
		{"远程更新→报更新", []string{"v1.9.0", "v1.2.0"}, "v1.2.0", "v1.2.0", "v1.9.0", true},
		{"已最新→无更新", []string{"v1.9.0"}, "v1.9.0", "v1.9.0", "v1.9.0", false},
		{"本机更新（异常态）→无更新", []string{"v1.2.0"}, "v1.9.0", "v1.9.0", "v1.2.0", false},
		{"未安装→交付路径不谎报", []string{"v1.9.0"}, "", "", "v1.9.0", false},
		{"远程空列表→无法判定", nil, "v1.2.0", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := seedUpdateFixture(t, c.remote, c.install)
			local, remote, has, err := m.CheckUpdate(context.Background())
			if err != nil {
				t.Fatalf("CheckUpdate: %v", err)
			}
			if local != c.wantLocal || remote != c.wantRemote || has != c.wantHasUpd {
				t.Errorf("got (%q,%q,%v) want (%q,%q,%v)", local, remote, has, c.wantLocal, c.wantRemote, c.wantHasUpd)
			}
		})
	}
}

func TestCheckUpdateCtxCancelPropagates(t *testing.T) {
	m := seedUpdateFixture(t, []string{"v1.9.0"}, "v1.2.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, has, err := m.CheckUpdate(ctx)
	if err == nil || has {
		t.Fatalf("ctx 取消必须上抛且不谎报更新，得 err=%v has=%v", err, has)
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("错误形态漂移: %v", err)
	}
}

func TestCheckUpdateUsesStableChannelHead(t *testing.T) {
	// parseReleasesBody 已把预发布/低限/无摘要挡在门外——列表首条即稳定通道
	// 最新；语义序钉死（1.10 > 1.9，非字典序）
	m := seedUpdateFixture(t, []string{"v1.9.0", "v1.10.0"}, "v1.9.0")
	local, remote, has, err := m.CheckUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// seedRemote 灌的是注入序（CheckUpdate 消费 ListRemote[0]），钉"首条即通道头"
	if remote != "v1.9.0" || local != "v1.9.0" || has {
		t.Errorf("稳定通道头判定漂移: (%q,%q,%v)", local, remote, has)
	}
	// 经 parse 全链则应为 v1.10.0 居首（TestParseReleasesBody 已钉），此处验证
	// 契约对"列表序"的消费纪律：首条即头，不二次挑选。
}
