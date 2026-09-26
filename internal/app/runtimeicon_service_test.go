// runtimeicon_service_test.go 钉死集中图标服务的门语义与缓存纪律：
// 停用/未装/未知/无提取源四类拒绝各回各的错；正/负缓存按源文件指纹
// （路径+size+mtime）失效重取，绝不每次渲染都撞解析。
package app

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/packages/go/peicon/petest"
)

// fakeIconModule 是最小 extapi.Module + IconSourceProvider 替身。
type fakeIconModule struct {
	id        string
	servesExe bool
	exe       string
	calls     int
	err       error
}

func (m *fakeIconModule) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{ID: m.id, Name: m.id, Level: extapi.LevelBuiltin}
}
func (m *fakeIconModule) Nav() []extapi.NavEntry       { return nil }
func (m *fakeIconModule) Services() []extapi.Service   { return nil }
func (m *fakeIconModule) OnInit(context.Context) error { return nil }
func (m *fakeIconModule) OnDestroy() error             { return nil }
func (m *fakeIconModule) IsInitialized() bool          { return true }
func (m *fakeIconModule) IconSourceExe() (string, error) {
	m.calls++
	if !m.servesExe {
		return "", errors.New("未挂提取源")
	}
	return m.exe, m.err
}

// plainModule 不实现 IconSourceProvider（对照组）。
type plainModule struct{ id string }

func (m *plainModule) Info() extapi.ModuleInfo      { return extapi.ModuleInfo{ID: m.id} }
func (m *plainModule) Nav() []extapi.NavEntry       { return nil }
func (m *plainModule) Services() []extapi.Service   { return nil }
func (m *plainModule) OnInit(context.Context) error { return nil }
func (m *plainModule) OnDestroy() error             { return nil }
func (m *plainModule) IsInitialized() bool          { return true }

func validIconPE(t *testing.T) (dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "tool.exe")
	rsrc := petest.BuildRsrc([]petest.Type{
		{ID: petest.IconType, Names: []petest.Name{{ID: 1, Langs: []petest.Data{{Payload: petest.PNG(16, color.NRGBA{R: 1, G: 2, B: 3, A: 255})}}}}},
	})
	if err := os.WriteFile(file, petest.BuildPE(rsrc, false), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, file
}

func newIconSvc(t *testing.T, mods ...extapi.Module) (*RuntimeIconService, *extapi.Registry) {
	t.Helper()
	reg := extapi.NewRegistry(nil)
	if err := reg.Register(mods...); err != nil {
		t.Fatal(err)
	}
	return NewRuntimeIconService(reg), reg
}

func TestRuntimeIconServiceGate(t *testing.T) {
	_, exe := validIconPE(t)
	provider := &fakeIconModule{id: "rammap", servesExe: true, exe: exe}
	plain := &plainModule{id: "memo"}
	svc, reg := newIconSvc(t, provider, plain)

	t.Run("未知模块回ErrUnknownModule", func(t *testing.T) {
		_, err := svc.IconPNG("nope")
		if !errors.Is(err, extapi.ErrUnknownModule) {
			t.Fatalf("期望 ErrUnknownModule, got %v", err)
		}
	})

	t.Run("未挂提取源如实拒绝", func(t *testing.T) {
		_, err := svc.IconPNG("memo")
		if err == nil || !strings.Contains(err.Error(), "未提供运行期图标提取源") {
			t.Fatalf("期望未挂源错误, got %v", err)
		}
	})

	t.Run("停用模块拒绝且不打源", func(t *testing.T) {
		if err := reg.SetEnabled("rammap", false); err != nil {
			t.Fatal(err)
		}
		before := provider.calls
		_, err := svc.IconPNG("rammap")
		if !errors.Is(err, extapi.ErrModuleDisabled) {
			t.Fatalf("期望 ErrModuleDisabled, got %v", err)
		}
		if provider.calls != before {
			t.Fatal("停用裁决必须先于源解析——绝不给停用模块的 UI 触活业务路径")
		}
		if err := reg.SetEnabled("rammap", true); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("未安装凭据拒绝", func(t *testing.T) {
		reg.SetReceiptStorage(fakeReceipts{installed: map[string]bool{"rammap": false}})
		defer reg.SetReceiptStorage(nil)
		_, err := svc.IconPNG("rammap")
		if !errors.Is(err, extapi.ErrModuleNotInstalled) {
			t.Fatalf("期望 ErrModuleNotInstalled, got %v", err)
		}
	})
}

func TestRuntimeIconServiceCache(t *testing.T) {
	t.Run("正缓存指纹失效与负缓存", func(t *testing.T) {
		dir, exe := validIconPE(t)
		provider := &fakeIconModule{id: "rammap", servesExe: true, exe: exe}
		svc, _ := newIconSvc(t, provider)

		png1, err := svc.IconPNG("rammap")
		if err != nil || len(png1) == 0 {
			t.Fatalf("首提取失败: %v", err)
		}
		// 同 size 同 mtime 的等长扰动（改坏图标负载区）：若命中缓存必回旧图；
		// 若重解析必报错。
		orig, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		bad := bytes.Clone(orig)
		// 打坏资源根目录计数（节偏移 0x400+14 = NumberOfIdEntries）：
		// 重解析必炸"目录条目数异常"，但保持文件 size 不变。
		bad[0x414], bad[0x415] = 0xff, 0xff
		fi, _ := os.Stat(exe)
		if err := os.WriteFile(exe, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(exe, fi.ModTime(), fi.ModTime()); err != nil {
			t.Fatal(err)
		}
		png2, err := svc.IconPNG("rammap")
		if err != nil || string(png2) != string(png1) {
			t.Fatalf("指纹未变应吃正缓存, got err=%v len=%d", err, len(png2))
		}
		// mtime 前移 → 作废重取 → 坏 PE 报错并落负缓存（源未变化不再重试解析）。
		if err := os.Chtimes(exe, fi.ModTime().Add(time.Second), fi.ModTime().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.IconPNG("rammap"); err == nil {
			t.Fatal("源变化后应重解析并如实报错")
		}
		_, err = svc.IconPNG("rammap")
		if err == nil || !strings.Contains(err.Error(), "此前已失败") {
			t.Fatalf("期望负缓存短路, got %v", err)
		}
		_ = dir
	})

	t.Run("源路径解析失败不落负缓存", func(t *testing.T) {
		provider := &fakeIconModule{id: "rammap", servesExe: true, err: errors.New("尚未安装")}
		svc, _ := newIconSvc(t, provider)
		if _, err := svc.IconPNG("rammap"); err == nil {
			t.Fatal("期望失败")
		}
		if _, err := svc.IconPNG("rammap"); err == nil {
			t.Fatal("源解析失败属瞬时态，每次调用都该如实上抛")
		}
	})
}

// fakeReceipts 最小 ReceiptStorage 替身。
type fakeReceipts struct{ installed map[string]bool }

func (f fakeReceipts) IsInstalled(moduleID string) bool { return f.installed[moduleID] }
func (f fakeReceipts) Installed() map[string]bool       { return f.installed }
func (f fakeReceipts) MarkInstalled(moduleID string, kind extapi.ReceiptKind) error {
	f.installed[moduleID] = true
	return nil
}
func (f fakeReceipts) MarkAbsent(moduleID string) error {
	f.installed[moduleID] = false
	return nil
}
