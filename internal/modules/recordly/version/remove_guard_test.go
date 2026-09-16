package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveRejectsVersionMismatch 锁 BUG-024 变体回归：托管为单目录单版本，
// Remove 删的就是那一份固定目录——旧实现完全忽略 version 参数，调用方传任意
// 不匹配版本号即可绕过 service 层按参数比对的"运行中禁删"保护。
// 现要求请求版本与实装版本核心一致才放行（传空 = 接受任何版本，与 ResolveExe 契约同构）。
func TestRemoveRejectsVersionMismatch(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)
	inst := m.InstallDir()
	mkInstall(t, inst)
	meta, _ := json.Marshal(map[string]any{"tag": "v1.3.5"})
	if err := os.WriteFile(filepath.Join(inst, "hanxi-meta.json"), meta, 0644); err != nil {
		t.Fatal(err)
	}

	if err := m.Remove("v9.9.9"); err == nil {
		t.Fatal("不匹配版本的卸载应被拒绝")
	}
	if _, err := os.Stat(inst); err != nil {
		t.Fatalf("拒绝后安装目录应完好: %v", err)
	}

	if err := m.Remove("v1.3.5"); err != nil {
		t.Fatalf("匹配版本的卸载应成功: %v", err)
	}
	if _, err := os.Stat(inst); !os.IsNotExist(err) {
		t.Fatal("卸载后目录应消失")
	}
}
