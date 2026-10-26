package snipaste

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/snipaste/version"
)

// ---------- 测试替身与夹具 ----------

type snipToggleGate struct{ deny atomic.Bool }

func (g *snipToggleGate) Acquire(string) (func(), error) {
	if g.deny.Load() {
		return nil, errors.New("模块已停用（测试门拒）")
	}
	return func() {}, nil
}

type snipFixture struct {
	svc         *SnipasteService
	versionsDir string
	stateDir    string
}

// newSnipFixture engine 留 nil（RemoveVersion 对 nil 引擎有显式防护；本文件的
// 用例全部走静止语义）。manager 用真实 PE 读取注入——ListInstalled 的 FileVersion
// 核验无法以伪 exe 通过，故此处只依赖 ResolveExe/Remove 的目录形状闸。
func newSnipFixture(t *testing.T) *snipFixture {
	t.Helper()
	versionsDir := t.TempDir()
	stateDir := t.TempDir()
	return &snipFixture{
		svc: &SnipasteService{
			manager:   version.NewManager(versionsDir),
			store:     newSnipasteStore(stateDir),
			downloads: map[string]struct{}{},
			holder:    extapi.NewLeaseHolder(ID),
		},
		versionsDir: versionsDir,
		stateDir:    stateDir,
	}
}

func mkSnipVersion(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "snipaste_v"+ver)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Snipaste.exe"), []byte("MZ载荷"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ---------- SetActiveVersion ----------

// TestSnipSetActiveValidatesBeforePersisting 设定使用版本先校验后落账：未装
// 版本号被拒且落盘账本原样保留；合法输入剥 v/去空白归一后返回并持久化
// （判据：同目录重载 store，即 snipaste.json 实际内容）。
func TestSnipSetActiveValidatesBeforePersisting(t *testing.T) {
	f := newSnipFixture(t)
	mkSnipVersion(t, f.versionsDir, "2.11.3")
	if err := f.svc.store.SetActive("2.10.8"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.SetActiveVersion("2.9.9"); err == nil {
		t.Fatal("未安装版本应被拒绝")
	}
	if got := newSnipasteStore(f.stateDir).GetActive(); got != "2.10.8" {
		t.Fatalf("拒绝后账本应保持 2.10.8，实得 %q", got)
	}

	got, err := f.svc.SetActiveVersion(" v2.11.3 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2.11.3" {
		t.Fatalf("应返回剥 v 归一的 2.11.3，实得 %q", got)
	}
	if reloaded := newSnipasteStore(f.stateDir).GetActive(); reloaded != "2.11.3" {
		t.Fatalf("落盘重载应为 2.11.3，实得 %q", reloaded)
	}
}

// ---------- RemoveVersion 放行分支 ----------

// TestSnipRemoveNonActiveVersion 卸载非在用版本：目录经 rename 隔离删除、
// active 账本不动；未装版本被拒不伤及在场目录。（在用版本不可卸与"唯一已装
// 放行"依赖 ListInstalled 的真实 PE 核验，伪载荷无法构造，如实不在本层钉死。）
func TestSnipRemoveNonActiveVersion(t *testing.T) {
	f := newSnipFixture(t)
	keep := mkSnipVersion(t, f.versionsDir, "2.11.3")
	gone := mkSnipVersion(t, f.versionsDir, "2.10.8")
	if err := f.svc.store.SetActive("2.11.3"); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.RemoveVersion(" v2.10.8 "); err != nil {
		t.Fatalf("卸载非在用版本: %v", err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatal("snipaste_v2.10.8 目录应已删除")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("在用版本目录不应被波及: %v", err)
	}
	if got := f.svc.store.GetActive(); got != "2.11.3" {
		t.Fatalf("卸非在用版本不应动 active，实得 %q", got)
	}

	if err := f.svc.RemoveVersion("9.9.9"); err == nil {
		t.Fatal("未安装版本卸载应被拒")
	}
}

// ---------- resolveActiveVersion 自愈 ----------

// TestSnipResolveActiveSelfHealWhenNothingValid 设定版本失效且无任何可列版本
// （ListInstalled 空）：如实报引导错误，且失效设定被清空自愈落盘——防止坏
// active 反复卡冷启动。判据：错误文案 + 重载 store 读出空值。
func TestSnipResolveActiveSelfHealWhenNothingValid(t *testing.T) {
	f := newSnipFixture(t)
	if err := f.svc.store.SetActive("2.9.9"); err != nil { // 目录不存在
		t.Fatal(err)
	}
	_, _, err := f.svc.resolveActiveVersion()
	if err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("应给引导错误，实得 %v", err)
	}
	if got := newSnipasteStore(f.stateDir).GetActive(); got != "" {
		t.Fatalf("失效设定应已清空自愈（落盘），实得 %q", got)
	}

	// 在场但 PE 核验不过的版本同样不被误当已装（ListInstalled 白名单外）：
	// 冷启动仍如实报未装，绝不把来路不明的目录当可用版本端出去。
	mkSnipVersion(t, f.versionsDir, "2.11.3")
	if _, _, err := f.svc.resolveActiveVersion(); err == nil {
		t.Fatal("伪载荷目录不应被当作可用已装版本")
	}
}

// ---------- OpenDir ----------

// TestSnipOpenDirRejectsInvalidTargets 空串/不存在/文件目标均在 explorer
// 拉起前被拒（成功分支真起 explorer.exe，无可取判据且不该产生桌面副作用，
// 如实不测）。
func TestSnipOpenDirRejectsInvalidTargets(t *testing.T) {
	f := newSnipFixture(t)
	if err := f.svc.OpenDir("  "); err == nil {
		t.Fatal("空路径应被拒")
	}
	file := filepath.Join(f.versionsDir, "note.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.OpenDir(file); err == nil {
		t.Fatal("文件目标应被拒（OpenDir 语义恒为目录）")
	}
}

// ---------- 调用门 ----------

// TestSnipCallGateDenyRejectsBusinessEntries 门拒时业务 RPC 如实上抛，
// 不得静默回空值/空列表。
func TestSnipCallGateDenyRejectsBusinessEntries(t *testing.T) {
	f := newSnipFixture(t)
	g := &snipToggleGate{}
	g.deny.Store(true)
	f.svc.holder.SetGate(g)

	if _, err := f.svc.ListInstalledVersions(); err == nil {
		t.Fatal("ListInstalledVersions 应被门拒")
	}
	if _, err := f.svc.GetActiveVersion(); err == nil {
		t.Fatal("GetActiveVersion 应被门拒")
	}
	if _, err := f.svc.SetActiveVersion("2.11.3"); err == nil {
		t.Fatal("SetActiveVersion 应被门拒")
	}
	if err := f.svc.RemoveVersion("2.11.3"); err == nil {
		t.Fatal("RemoveVersion 应被门拒")
	}

	g.deny.Store(false)
	got, err := f.svc.GetActiveVersion()
	if err != nil || got != "" {
		t.Fatalf("门恢复后应正常放行，实得 %q %v", got, err)
	}
}
