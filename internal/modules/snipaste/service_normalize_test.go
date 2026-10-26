package snipaste

import (
	"os"
	"testing"
)

// TestNormalizeVersion 版本入参归一词表：本模块名册与目录令牌口径是裸号
// （snipaste_v<x.y.z> 的 v 由 dirPrefix 承载），故四种写法同落同一个裸号。
// 大写 V 形态是 67f3bd3 批次的病灶本体，旧形制只剥小写 v，"V2.11.3" 原样留下
// 即在下游形状闸上被判"非法版本号"。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2.11.3", "2.11.3"},
		{"v2.11.3", "2.11.3"},
		{"V2.11.3", "2.11.3"},
		{"  v2.11.3 ", "2.11.3"},
		{" V2.11.3\t", "2.11.3"},
		{"v 2.11.3", "2.11.3"}, // 旧口径靠外层 TrimSpace 兜住"v 与号之间带空格"，不回归
		{"imported-0123456789ab", "imported-0123456789ab"},
		{"vimported-0123456789ab", "imported-0123456789ab"},
		{"Vimported-0123456789ab", "imported-0123456789ab"},
		{"", ""}, // 空串旧口径即回空（下游形状闸拒），本修复不改此行为
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestSetActiveRemoveSameValueConsistent 三入口一致性中可造的两入口（SetActive/
// Remove）：V 形态设定使用版本不再"非法版本号"，返回值与落账同为裸号规范形；
// 随后换另一种形态卸载仍认账（目录从磁盘消失）。
//
// 未装版本另断"同一版本 V 形态与裸号形态话术逐字相同"——旧形制下 V 形态过不了
// resolveVersionDir 的 plainVersionRe（报"非法版本号"），裸号形态报"未安装"，正是
// 审查批次在 papertodo/rufus/recordly 复现的三入口互异。
func TestSetActiveRemoveSameValueConsistent(t *testing.T) {
	f := newSnipFixture(t)
	dir := mkSnipVersion(t, f.versionsDir, "2.11.3")

	got, err := f.svc.SetActiveVersion("V2.11.3")
	if err != nil {
		t.Fatalf("SetActiveVersion(V 形态): %v", err)
	}
	if got != "2.11.3" || f.svc.store.GetActive() != "2.11.3" {
		t.Fatalf("SetActiveVersion = %q active=%q，应同落裸号 2.11.3", got, f.svc.store.GetActive())
	}

	// 未装版本两形态话术必须一致（本模块无 imported- 兜底，形状判定即最终口径）。
	errV := setErr(f, "V9.9.9")
	errPlain := setErr(f, "9.9.9")
	if errV == nil || errPlain == nil {
		t.Fatalf("前置：未装版本不得可激活，V=%v 裸=%v", errV, errPlain)
	}
	if errV.Error() != errPlain.Error() {
		t.Fatalf("未装版本两种形态话术须一致，实得 V=%q 裸=%q", errV, errPlain)
	}

	// Remove 与 SetActive 同一口径：卸在用版本受"唯一/使用中不可卸"守卫干扰，
	// 先把 active 归零再验 V 形态卸载真实生效（守卫语义由 service_flow_test.go 钉）。
	if err := f.svc.store.SetActive(""); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveVersion(" V2.11.3\t"); err != nil {
		t.Fatalf("RemoveVersion(V 形态带空白): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("V 形态卸载应移除版本目录，实得 err=%v", err)
	}
}

func setErr(f *snipFixture, v string) error {
	_, err := f.svc.SetActiveVersion(v)
	return err
}

// TestDownloadDedupObservableLimit 如实钉死本家"已装去重"面的可测边界，不 t.Skip、
// 不造假覆盖面：snipaste 的 ListInstalled 对每个候选目录强制真实 PE FileVersion
// 核验（version/manager.go:104），且 fileVersion 缝是 version 包私有字段，service
// 包测试注入不了——伪 exe 目录一律不入名册，故 DownloadVersion 的"已装 V 形态回
// already-installed"在本包内构造不出已装前提（硬造只会让四种形态全走 started 起真
// 下载，那是假覆盖）。本用例断的正是"伪目录确实不入名册"这一外部事实：去重用例
// 缺席是边界所致，不是漏测（litemonitor 在 67f3bd3 同口径声明）。
//
// 去重判据本身（strings.EqualFold(item.Version, normalizeVersion(入参))）的两条腿
// 已由上两条用例分别钉住：名册口径 = 裸号（TestNormalizeVersion）、V 形态入参归一
// 后 = 同一裸号（TestSetActiveRemoveSameValueConsistent 走的就是同一归一函数与同一
// 形状闸），不再大小写互异。
func TestDownloadDedupObservableLimit(t *testing.T) {
	f := newSnipFixture(t)
	mkSnipVersion(t, f.versionsDir, "2.11.3")

	if list, err := f.svc.manager.ListInstalled(); err != nil || len(list) != 0 {
		t.Fatalf("前置边界已失效（伪 PE 目录竟入名册，去重面应改测真调用）：%+v err=%v", list, err)
	}
	// 名册为空时 DownloadVersion 的已装判定恒不可能命中——用可观察的名册实况把
	// "本包无法构造已装前提"落成断言，而不是默默不测。
	if installed, err := f.svc.manager.ListInstalled(); err == nil && len(installed) > 0 {
		t.Fatalf("边界断言自证失败： %+v", installed)
	}
}
