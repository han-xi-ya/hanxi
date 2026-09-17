package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func touch(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestMigrateRootStateFiles(t *testing.T) {
	t.Run("正常迁移：状态文件与残骸进 state，锚点与目录不动", func(t *testing.T) {
		base := t.TempDir()
		state := filepath.Join(base, "state")

		touch(t, filepath.Join(base, "config.json"), `{}`)                        // 便携根标记：必须留
		touch(t, filepath.Join(base, "markeron.json"), `{"a":1}`)                 // 模块状态：搬
		touch(t, filepath.Join(base, "projects.json"), `[]`)                      // frpc 泛化名：同样搬
		touch(t, filepath.Join(base, "wsl-portproxy.json"), `{}`)                 // wsl：搬
		touch(t, filepath.Join(base, "bcu.json.tmp.4242"), `{}`)                  // 原子写残骸：搬
		touch(t, filepath.Join(base, ".wsl-portproxy-7.tmp"), `{}`)               // wsl 残骸：搬
		touch(t, filepath.Join(base, "config.json.corrupt-20260101-000000"), `x`) // 取证副本：留
		if err := os.MkdirAll(filepath.Join(base, "logs"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(base, "everything"), 0755); err != nil {
			t.Fatal(err)
		}

		moved, skipped, err := migrateRootStateFiles(base, state)
		if err != nil {
			t.Fatalf("迁移不应报错: %v", err)
		}
		if len(skipped) != 0 {
			t.Errorf("无冲突场景不应有跳过: %v", skipped)
		}
		wantMoved := map[string]bool{
			"markeron.json": true, "projects.json": true, "wsl-portproxy.json": true,
			"bcu.json.tmp.4242": true, ".wsl-portproxy-7.tmp": true,
		}
		if len(moved) != len(wantMoved) {
			t.Errorf("moved 清单异常: got %v", moved)
		}
		for _, m := range moved {
			if !wantMoved[m] {
				t.Errorf("意外迁移: %s", m)
			}
			if !exists(filepath.Join(state, m)) {
				t.Errorf("%s 应已落位 state/", m)
			}
			if exists(filepath.Join(base, m)) {
				t.Errorf("%s 不应残留在根目录", m)
			}
		}
		for _, keep := range []string{"config.json", "config.json.corrupt-20260101-000000"} {
			if !exists(filepath.Join(base, keep)) {
				t.Errorf("%s 必须留在数据根", keep)
			}
		}
		for _, d := range []string{"logs", "everything", "versions"} {
			fi, err := os.Stat(filepath.Join(base, d))
			if d == "versions" {
				continue // 本用例未建，跳过
			}
			if err != nil || !fi.IsDir() {
				t.Errorf("目录 %s 不应被迁移触碰", d)
			}
		}
		// 内容随迁完整
		b, err := os.ReadFile(filepath.Join(state, "markeron.json"))
		if err != nil || string(b) != `{"a":1}` {
			t.Errorf("迁移后内容应完整无损: %q, %v", b, err)
		}
	})

	t.Run("幂等：二跑 no-op", func(t *testing.T) {
		base := t.TempDir()
		state := filepath.Join(base, "state")
		touch(t, filepath.Join(base, "memo.json"), `[]`)

		moved, _, _ := migrateRootStateFiles(base, state)
		if len(moved) != 1 {
			t.Fatalf("首跑应迁移 1 个, got %v", moved)
		}
		moved2, skipped2, err := migrateRootStateFiles(base, state)
		if err != nil {
			t.Fatal(err)
		}
		if len(moved2) != 0 || len(skipped2) != 0 {
			t.Errorf("二跑应完全 no-op, got moved=%v skipped=%v", moved2, skipped2)
		}
	})

	t.Run("目标冲突：状态文件跳过保留双方，残骸同名则删根侧", func(t *testing.T) {
		base := t.TempDir()
		state := filepath.Join(base, "state")
		touch(t, filepath.Join(state, "ocr.json"), `{"new":true}`) // state 侧已有（回滚期写回场景）
		touch(t, filepath.Join(base, "ocr.json"), `{"old":true}`)  // 根侧同名冲突
		touch(t, filepath.Join(state, "keyviz.json.tmp.1"), "x")   // 残骸冲突：state 已有
		touch(t, filepath.Join(base, "keyviz.json.tmp.1"), "y")    // 根侧尸块

		moved, skipped, err := migrateRootStateFiles(base, state)
		if err != nil {
			t.Fatal(err)
		}
		if len(moved) != 0 {
			t.Errorf("冲突项不应被迁移: %v", moved)
		}
		if len(skipped) != 1 || skipped[0] != "ocr.json" {
			t.Errorf("ocr.json 应记为跳过: %v", skipped)
		}
		if !exists(filepath.Join(base, "ocr.json")) || !exists(filepath.Join(state, "ocr.json")) {
			t.Error("冲突状态文件应双方保留，不许静默覆盖")
		}
		if exists(filepath.Join(base, "keyviz.json.tmp.1")) {
			t.Error("同名残骸应删除根侧")
		}
		b, _ := os.ReadFile(filepath.Join(state, "ocr.json"))
		if string(b) != `{"new":true}` {
			t.Error("state 侧既有文件内容不得被触碰")
		}
	})

	t.Run("空根目录：只建 state 不报错", func(t *testing.T) {
		base := t.TempDir()
		state := filepath.Join(base, "state")
		moved, skipped, err := migrateRootStateFiles(base, state)
		if err != nil || len(moved) != 0 || len(skipped) != 0 {
			t.Fatalf("空根应安静通过: %v %v %v", moved, skipped, err)
		}
		if !exists(state) {
			t.Error("state 目录应被兜底创建")
		}
	})

	t.Run("非模式文件不误伤", func(t *testing.T) {
		base := t.TempDir()
		state := filepath.Join(base, "state")
		touch(t, filepath.Join(base, "portable.txt"), "keep me")
		touch(t, filepath.Join(base, "readme.md"), "keep me")

		moved, _, err := migrateRootStateFiles(base, state)
		if err != nil || len(moved) != 0 {
			t.Fatalf("无关文件不得迁移: %v %v", moved, err)
		}
		if !strings.Contains(readFileStr(t, filepath.Join(base, "portable.txt")), "keep me") {
			t.Error("portable.txt 应原样留在根目录")
		}
	})
}

func readFileStr(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
