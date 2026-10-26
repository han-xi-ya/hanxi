// meta_atomic_test.go 锁 Snipaste 版本账本落盘的对外形态（writeJSON 收口
// jsonstore.Save 后的回归门）。
//
// 断言的是外部事实，不读实现：
//   - 重写后目录里**只剩 meta.json**（原子写的临时件必须被 rename 收走，不留残骸）；
//   - 盘上字节仍是"两空格缩进的 JSON"（前端/后续读账逻辑依赖此形态，改实现不得改格式）；
//   - 重读得到的字段值与最后一次写入一致（不得出现半截或被旧值覆盖）。
package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteJSONRewriteLeavesNoDebrisAndKeepsFormat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "meta.json")

	if err := writeJSON(p, map[string]any{"version": "1.2.3", "sha256": strings.Repeat("a", 64)}); err != nil {
		t.Fatalf("首次落账: %v", err)
	}
	if err := writeJSON(p, map[string]any{"version": "1.2.4", "sha256": strings.Repeat("b", 64)}); err != nil {
		t.Fatalf("重写落账: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读目录: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "meta.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("落账后目录应只剩 meta.json，实得 %v（原子写的临时件必须被 rename 收走）", names)
	}

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	// 格式契约：两空格缩进的 JSON 对象（收口 jsonstore.Save 不得改变盘上形态）。
	if !strings.HasPrefix(string(raw), "{\n  \"") {
		t.Errorf("盘上格式非两空格缩进 JSON，前缀实得 %q", string(raw)[:min(12, len(raw))])
	}
	var mm map[string]any
	if err := json.Unmarshal(raw, &mm); err != nil {
		t.Fatalf("落盘内容不是合法 JSON: %v", err)
	}
	if mm["version"] != "1.2.4" || mm["sha256"] != strings.Repeat("b", 64) {
		t.Errorf("重读值与最后一次写入不符，实得 %v", mm)
	}
}

// TestWriteJSONFailureKeepsOldIntact 传入不可序列化的值时必须如实报错，
// 且不得把原文件写成空壳（账本被清空比写失败更糟）。
func TestWriteJSONFailureKeepsOldIntact(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "meta.json")
	if err := writeJSON(p, map[string]any{"version": "9.9.9"}); err != nil {
		t.Fatalf("预置账本: %v", err)
	}
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读预置: %v", err)
	}
	// chan 不可 JSON 序列化：写入应失败上抛
	if err := writeJSON(p, map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("不可序列化却返回 nil（错误被吞）")
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("失败后原账本竟不可读: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("写失败把原账本改动/清空了：原 %d 字节，现 %d 字节", len(before), len(after))
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("写失败却在目录留下 %d 个条目（残骸未清）", len(entries))
	}
}
