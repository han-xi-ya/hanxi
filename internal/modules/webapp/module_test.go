package webapp

import (
	"path/filepath"
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
