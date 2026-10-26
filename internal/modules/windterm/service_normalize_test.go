package windterm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWindTermDownloadInstalledDedupCaseInsensitive 已装 2.16.1 时含大写 V 的四种
// 入参都回 already-installed：名册零登记 + versions 根目录零新增 + active 未漂移。
// 每轮换一把"首个放行、之后一律拒接"的调用门——去重若落空，后台协程取不到租约即
// 早退，测试不触网络、不落位（TestWindTermDownloadBackgroundLeaseBookkeeping 同法），
// 断言链本身仍是同步判据，无赛跑假绿。
func TestWindTermDownloadInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir, _, _ := newTestWindTermService(t)
	seedWTInstalled(t, versionsDir, "2.16.1")

	for _, in := range []string{"2.16.1", "v2.16.1", "V2.16.1", " V2.16.1\t"} {
		svc.holder.SetGate(&firstAcceptWTGate{denied: make(chan struct{}, 4)})
		state, err := svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if state != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed（已装版本不得起后台下载）", in, state)
		}
		if len(svc.downloads) != 0 {
			t.Fatalf("DownloadVersion(%q) 已装早退不得登记在飞名册，实得 %v", in, svc.downloads)
		}
	}
	ents, err := os.ReadDir(versionsDir)
	if err != nil || len(ents) != 1 {
		t.Fatalf("去重路径不得新增版本目录，实得 %+v err=%v", ents, err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("去重路径不得改 active，实得 %q", got)
	}
}

// TestWindTermImportedTokenDedupAllSpellings 导入兜底令牌三种写法（裸 imported-、
// vimported-、Vimported-）都必须命中已装名册回 already-installed——本家名册展示与
// 目录令牌就是裸 imported-，旧形制把 vimported-/Vimported- 原样或双补放行，去重落空
// 即白起一路注定失败的后台下载。
func TestWindTermImportedTokenDedupAllSpellings(t *testing.T) {
	svc, _, _, _ := newTestWindTermService(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "WindTerm.exe"), []byte("MZ fake import"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := svc.manager.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	token := info.Version // ListInstalled 同口径取数，不断字符串形状

	for _, in := range []string{token, "v" + token, "V" + token, " " + token + "\t"} {
		svc.holder.SetGate(&firstAcceptWTGate{denied: make(chan struct{}, 4)})
		state, err := svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if state != "already-installed" {
			t.Fatalf("导入令牌 %q 应回 already-installed（名册实得 %q），实得 %q", in, token, state)
		}
		if len(svc.downloads) != 0 {
			t.Fatalf("DownloadVersion(%q) 已装早退不得登记在飞名册，实得 %v", in, svc.downloads)
		}
	}
}

// TestWindTermThreeEntriesSameValueConsistent 三入口同输入一致：V 形态设定使用版本
// 不再"未安装"，返回值与落账同为 v 规范形；随后换带空白的裸号形态卸载，仍认账
// （目录从磁盘消失）并清 active——归一在 Download/SetActive/Remove 三点贯通同一口径。
// 未装版本另断 V 形态与裸号形态话术逐字相同（旧形制下前者落 "vV…" 双补键，三入口互异）。
func TestWindTermThreeEntriesSameValueConsistent(t *testing.T) {
	svc, versionsDir, _, _ := newTestWindTermService(t)
	dir := filepath.Join(versionsDir, "windterm_2.16.1")
	seedWTInstalled(t, versionsDir, "2.16.1")

	got, err := svc.SetActiveVersion("V2.16.1")
	if err != nil {
		t.Fatalf("SetActiveVersion(V 形态): %v", err)
	}
	if got != "v2.16.1" || svc.store.GetActive() != "v2.16.1" {
		t.Fatalf("SetActiveVersion = %q active=%q，应同落 v2.16.1", got, svc.store.GetActive())
	}

	errV := setErr(svc, "V9.9.9")
	errPlain := setErr(svc, "9.9.9")
	if errV == nil || errPlain == nil {
		t.Fatalf("前置：未装版本不得可激活，V=%v 裸=%v", errV, errPlain)
	}
	if errV.Error() != errPlain.Error() {
		t.Fatalf("未装版本两种形态话术须一致，实得 V=%q 裸=%q", errV, errPlain)
	}

	// 唯一已装版本恰为使用中：本家守卫放行卸载并清 active（service_test 已钉该语义）。
	if err := svc.RemoveVersion(" 2.16.1 "); err != nil {
		t.Fatalf("RemoveVersion(裸号形态): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("卸载应移除版本目录，实得 err=%v", err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("卸掉在用版本应清 active，实得 %q", got)
	}
}

func setErr(svc *WindTermService, v string) error {
	_, err := svc.SetActiveVersion(v)
	return err
}
