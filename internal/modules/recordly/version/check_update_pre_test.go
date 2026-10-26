package version

import (
	"context"
	"testing"
)

// TestCheckUpdatePreReleaseSemantics 锁 CheckUpdate 的对外结论（hasUpdate 布尔）
// 在"本机装的是 beta 通道 tag（hanxi-meta 权威，含 -beta.N 后缀）"时的正确性。
// 外部事实依据 semver 2.0 与上游真实发布形态（v1.3.5-beta.2 双通道并存，见
// remote.go semverTag 注释）：
//   - 正式版发布即超越同核心 beta：装 v1.3.5-beta.9、远程 stable v1.3.5 → 必须报"有更新"；
//   - 本机 beta 核心领先远程 stable：装 v1.10.0-beta.3、远程 stable v1.9.0 → 必须"无更新"
//     （否则红点引导用户点击"更新"实为降级）。
//
// 修复前 CheckUpdate 走 platform/versioncmp：其 split 只认"数字+纯字母尾"段，
// "-beta.9"含连字符即整串退化字典序——上面两结论一漏报一误报。
func TestCheckUpdatePreReleaseSemantics(t *testing.T) {
	ctx := context.Background()

	t.Run("stable追平beta核心必须报更新_防漏报", func(t *testing.T) {
		seedRemoteList(t,
			RecordlyRelease{Version: "v1.3.5-beta.10", IsPre: true},
			RecordlyRelease{Version: "v1.3.5"},
		)
		dir := t.TempDir()
		installRecordly(t, dir, "v1.3.5-beta.9")
		m := NewManager(dir)
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil {
			t.Fatalf("CheckUpdate 出错: %v", err)
		}
		if !up {
			t.Fatalf("装 v1.3.5-beta.9 遇 stable v1.3.5 必须判定有更新（semver: stable>同核心pre），实际漏报 local=%q remote=%q", local, remote)
		}
	})

	t.Run("beta核心领先stable必须无更新_防降级引导", func(t *testing.T) {
		seedRemoteList(t,
			RecordlyRelease{Version: "v1.10.0-beta.3", IsPre: true},
			RecordlyRelease{Version: "v1.9.0"},
		)
		dir := t.TempDir()
		installRecordly(t, dir, "v1.10.0-beta.3")
		m := NewManager(dir)
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil {
			t.Fatalf("CheckUpdate 出错: %v", err)
		}
		if up {
			t.Fatalf("本机 v1.10.0-beta.3 核心已领先 stable v1.9.0，不得报更新（点击即降级），实际 local=%q remote=%q", local, remote)
		}
	})
}
