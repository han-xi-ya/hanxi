package webapp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// TestTrayCommandsDynamicMapping 断言"每条目一命令"的动态映射：
// 聚合路径仅读 Store（不开窗、不依赖 OnInit），条目增删即时反映到候选目录。
func TestTrayCommandsDynamicMapping(t *testing.T) {
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mod := New(store)

	provider, ok := mod.(extapi.TrayCommandsProvider)
	if !ok {
		t.Fatal("webapp.Module 必须实现 extapi.TrayCommandsProvider")
	}

	cmds := provider.TrayCommands()
	if len(cmds) != 1 {
		t.Fatalf("应预置 1 条命令，实际 %+v", cmds)
	}
	if cmds[0].ID != "open:webapp_filehelper" || cmds[0].Label != "微信文件传输助手" || cmds[0].Run == nil {
		t.Errorf("预置命令映射错误: %+v", cmds[0])
	}

	if err := store.UpsertWebAppEntry(settings.WebAppEntry{ID: "webapp_42", Name: "测试站"}); err != nil {
		t.Fatal(err)
	}
	if cmds = provider.TrayCommands(); len(cmds) != 2 {
		t.Fatalf("新增条目后应聚合 2 条，实际 %+v", cmds)
	}

	if err := store.DeleteWebAppEntry("webapp_42"); err != nil {
		t.Fatal(err)
	}
	if cmds = provider.TrayCommands(); len(cmds) != 1 || cmds[0].ID != "open:webapp_filehelper" {
		t.Errorf("删除条目后候选应即净，实际 %+v", cmds)
	}
}

// TestTrayCommandsRunDispatchByDefaultOpen 命令执行 seam 的分派断言：轮盘/托盘
// 点击（Run 闭包）按条目 DefaultOpen 现读分派——browser 走系统浏览器通道；
// window/无键存量/盘上坏值走独立窗通道（headless 下报"应用内打开"错即命中
// 该通道的指纹）。Label 保持中性，不随形态变词。
func TestTrayCommandsRunDispatchByDefaultOpen(t *testing.T) {
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mod := New(store).(*Module)
	var opened string
	mod.svc.openURL = func(u string) error { opened = u; return nil }

	// 绕开服务层闸门直写条目，覆盖显式两态与盘上坏值（无键态由预置 filehelper 代表）
	seeds := []settings.WebAppEntry{
		{ID: "b1", Name: "浏览器站", URL: "https://b1.example.com", DefaultOpen: "browser"},
		{ID: "w1", Name: "窗口站", URL: "https://w1.example.com", DefaultOpen: "window"},
		{ID: "x1", Name: "坏值站", URL: "https://x1.example.com", DefaultOpen: "fullscreen"},
	}
	for _, e := range seeds {
		if err := store.UpsertWebAppEntry(e); err != nil {
			t.Fatal(err)
		}
	}

	byID := make(map[string]extapi.TrayCommand)
	for _, c := range mod.TrayCommands() {
		byID[c.ID] = c
	}
	if byID["open:b1"].Label != "浏览器站" {
		t.Errorf("Label 应为中性条目名（不带打开形态词），实际 %q", byID["open:b1"].Label)
	}

	ctx := context.Background()

	// browser → 浏览器通道命中定稿 URL，无错
	if err := byID["open:b1"].Run(ctx); err != nil || opened != "https://b1.example.com" {
		t.Errorf("browser 条目应经命令链走浏览器通道: err=%v opened=%q", err, opened)
	}

	for _, tc := range []struct{ id, name string }{
		{"open:w1", "显式 window"},
		{"open:webapp_filehelper", "无键存量"},
		{"open:x1", "盘上坏值"},
	} {
		opened = ""
		err := byID[tc.id].Run(ctx)
		if opened != "" {
			t.Errorf("%s 不得走浏览器通道，实际 opened=%q", tc.name, opened)
		}
		if err == nil || !strings.Contains(err.Error(), "应用内打开") {
			t.Errorf("%s 应走独立窗通道（headless 指纹错），实际 err=%v", tc.name, err)
		}
	}
}
