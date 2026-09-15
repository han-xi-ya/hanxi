package wsl

import (
	"os"
	"path/filepath"
	"testing"
)

func newPrefService(t *testing.T) (*WslService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wsl-install-pref.json")
	return &WslService{installPrefPath: path}, path
}

// 三态语义：未设置（回退前端默认）与"显式系统默认（空串）"必须可区分——
// 前者的反面教材正是 localStorage 把两者混进同一个空串，导致默认值一去不回。
func TestInstallDirPrefThreeStates(t *testing.T) {
	svc, path := newPrefService(t)

	if got := svc.GetDistroInstallDir(); got.Set || got.Dir != "" {
		t.Fatalf("文件缺失应为未设置: %+v", got)
	}

	if err := svc.SetDistroInstallDir(`D:\wsl`); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetDistroInstallDir(); !got.Set || got.Dir != `D:\wsl` {
		t.Fatalf("往返不一致: %+v", got)
	}

	// 显式留空=系统默认：Set=true 且 Dir=""，与未设置判然有别。
	if err := svc.SetDistroInstallDir("   "); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetDistroInstallDir(); !got.Set || got.Dir != "" {
		t.Fatalf("显式系统默认应为 Set=true+Dir=\"\": %+v", got)
	}

	// 损坏文件降级为未设置（不拦页面），且写回通道不受影响。
	if err := os.WriteFile(path, []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetDistroInstallDir(); got.Set {
		t.Fatalf("损坏偏好文件应降级为未设置: %+v", got)
	}
	if err := svc.SetDistroInstallDir(`E:\wsl`); err != nil {
		t.Fatalf("损坏后应可覆写修复: %v", err)
	}
}

func TestSetDistroInstallDirValidates(t *testing.T) {
	svc, _ := newPrefService(t)
	for _, bad := range []string{`wsl\relative`, `D:wsl`, `D:\a<>b`} {
		if err := svc.SetDistroInstallDir(bad); err == nil {
			t.Fatalf("非法路径须拒: %q", bad)
		}
	}

	// 盘符根目录合法（IsAbs 对 D:\ 成立，勿被过度校验误杀）。
	if err := svc.SetDistroInstallDir(`D:\`); err != nil {
		t.Fatalf("盘根目录应可记: %v", err)
	}

	// 路径不可用（单测/装配前）：如实报错不静默吞。
	orphan := &WslService{}
	if err := orphan.SetDistroInstallDir(`D:\wsl`); err == nil {
		t.Fatal("存储路径不可用须报错")
	}
	if got := orphan.GetDistroInstallDir(); got.Set {
		t.Fatal("存储路径不可用应读为未设置")
	}
}
