package mangodisk

import (
	"os"
	"testing"
)

// TestDownloadVersionInstalledDedupCaseInsensitive 已装 1.0.7 时含大写 V 的四种
// 入参都回 already-installed，且不放行任何后台下载。
//
// 判据不是返回值一句话：本模块的下载单飞闸 downloadMu 由 DownloadVersion 同步段
// TryLock 取得、由后台 goroutine 的 defer 释放——"started" 一路返回后锁必然仍在
// 手上，故 TryLock 失败即"后台下载确实起了"的硬证据；再加 versions 根目录零新增
// 与 active 未漂移两道磁盘/账目实况。归一退回只剥小写 v 时 V 形态先去重落空、
// 再取锁起下载，断言链逐条变红（行序同步，无赛跑假绿）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir, _ := newTestMangoDiskService(t)
	seedMDInstalled(t, versionsDir, "1.0.7")

	for _, in := range []string{"1.0.7", "v1.0.7", "V1.0.7", " V1.0.7\t"} {
		state, err := svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if state != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed（已装版本不得起后台下载）", in, state)
		}
		if !svc.downloadMu.TryLock() {
			t.Fatalf("DownloadVersion(%q) 已装早退不得占用单飞下载闸（占用说明后台下载已放行）", in)
		}
		svc.downloadMu.Unlock()
	}

	ents, err := os.ReadDir(versionsDir)
	if err != nil || len(ents) != 1 {
		t.Fatalf("去重路径不得新增版本目录，实得 %+v err=%v", ents, err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("去重路径不得改 active，实得 %q", got)
	}
}

// TestThreeEntriesSameValueConsistent 三入口对同一 V 形态值口径一致（审查批次
// 在 papertodo/rufus/recordly 复现的"同一值三种下场"在本家的对照面）：
//   - SetActiveVersion：V 形态与裸号必须走到同一道闸门、给同一句话术——假 PE 资产
//     过不了身份核验，两者都判"安装无效"；归一退回只剥小写时 V 形态连版本目录都
//     找不到（"未安装"），与裸号的"安装无效"互异，即本断言的红样。
//   - RemoveVersion：V 形态卸载真实生效（目录从磁盘消失），不再报"未安装"。
//     active 全程为空，避开本模块"使用中版本不可卸载"守卫干扰本断言。
func TestThreeEntriesSameValueConsistent(t *testing.T) {
	svc, versionsDir, _ := newTestMangoDiskService(t)
	dir := seedMDInstalled(t, versionsDir, "1.0.7")

	errV, errPlain := setErr(svc, "V1.0.7"), setErr(svc, "1.0.7")
	if errV == nil || errPlain == nil {
		t.Fatalf("前置：假 PE 资产不得可激活，V=%v 裸=%v", errV, errPlain)
	}
	if errV.Error() != errPlain.Error() {
		t.Fatalf("同一版本 V 形态与裸号形态话术须一致，实得 V=%q 裸=%q", errV, errPlain)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("被拒激活不得落账，实得 %q", got)
	}

	if err := svc.RemoveVersion(" V1.0.7\t"); err != nil {
		t.Fatalf("RemoveVersion(V 形态带空白): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("V 形态卸载应移除版本目录，实得 err=%v", err)
	}
}

// setErr 调 SetActiveVersion 只取错误（本家假资产恒被拒，返回值无意义）。
func setErr(svc *MangoDiskService, v string) error {
	_, err := svc.SetActiveVersion(v)
	return err
}
