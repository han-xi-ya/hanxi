// 真上游取证闸（默认 skip，须显式 HANXI_TEST_PIIK_REALDOWNLOAD=1）：
// 从上游真拉一个 release 走完整下载链，断言**布局不变式自检不再拒装**。
// 手动跑法（无窗口，不弹任何机主桌面痕迹）：
//
//	HANXI_TEST_PIIK_REALDOWNLOAD=1 go test ./internal/modules/piik/version -count=1 -run Real -v
//
// 可选 HANXI_TEST_PIIK_VERSION=v2.5.3 指定版本，缺省取远程列表最新一版。
package version

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDownloadRealUpstreamPassesLayout 断言的是**外部事实**（上游真包能装成），
// 不是任何字符串形状——这正是本包其余用例给不了的覆盖面。
//
// 为什么必须存在：#146 那次事故的形态是"下载链全绿、真机一个版本都装不上"——
// 其余用例的 zip 全部由测试自建（httptest 假源 + buildPiikZip），条目名清单
// 抄自实现常量，于是实现写 NOTICES、夹具照抄 NOTICES，测试与真实上游零交集，
// 错判被几百项断言一起保真（断言抄实现等于没测）。此用例是仓内唯一一道能证伪
// "我们对上游包内容的想象"的闸：真网、真摘要、真解包、真布局自检。
//
// 为什么不进默认门禁：要真网与真 46MB 级流量，且未认证 GitHub API 按 IP 限流
// （2026-11-04 实测本机超限、须走 gh-proxy 系镜像），让门禁红在一次网络抖动或
// 限流里没有信息量；追版节奏由上游决定，判据属"手动取证"而非"每次提交必过"。
func TestDownloadRealUpstreamPassesLayout(t *testing.T) {
	if os.Getenv("HANXI_TEST_PIIK_REALDOWNLOAD") != "1" {
		t.Skip("需真网与真包流量，默认不跑：设 HANXI_TEST_PIIK_REALDOWNLOAD=1 手动取证")
	}

	m := NewManager(t.TempDir())
	rels, err := m.ListRemote()
	if err != nil {
		t.Fatalf("远程列表获取失败（网络/限流？本用例是手动取证闸，红了先排这两样）: %v", err)
	}
	if len(rels) == 0 {
		t.Fatal("远程列表为空")
	}
	version := rels[0].Version // 列表按版本降序，[0] 即最新
	if v := os.Getenv("HANXI_TEST_PIIK_VERSION"); v != "" {
		version = v
	}

	// 完整生产链：Fetch（官方 digest 双核）→ UnpackZip（ZipSlip/CRC 闸）→
	// checkPiikLayout（本次事故的就死在这里）→ Tree.Commit 原子落位。
	if err := m.Download("real-probe", version, func(p DownloadProgress) {
		t.Logf("stage=%s done=%d total=%d msg=%s", p.Stage, p.Done, p.Total, p.Message)
	}); err != nil {
		t.Fatalf("真包 %s 应装得起来（下载链任一环节被拒都要如实暴露）: %v", version, err)
	}

	// 落位后的外部事实复核（不经内部判据自证）：主 exe 非空在场、可解析路径、
	// runtime 双 exe 在位——兄弟路径解析靠这三件吃饭。
	exe, err := m.ResolveExe(version)
	if err != nil {
		t.Fatalf("ResolveExe(%s): %v", version, err)
	}
	fi, statErr := os.Stat(exe)
	if statErr != nil || fi.Size() == 0 {
		t.Fatalf("主 exe 应为非空普通文件: %s", exe)
	}
	root := filepath.Dir(exe)
	t.Logf("真包落位: %s（主 exe %d 字节）", root, fi.Size())
	for _, rel := range []string{captureExeRel, cloudflaredExeRel} {
		if fi, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); statErr != nil || fi.Size() == 0 {
			t.Errorf("runtime 伴生件不在场（兄弟路径解析必炸）: %s", rel)
		}
	}
	if infos, lerr := m.ListInstalled(); lerr != nil || len(infos) == 0 {
		t.Errorf("装成后 ListInstalled 应能列出该版本: %v / %v", infos, lerr)
	}
}
