package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWhitelisted(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		want bool
	}{
		{"根配置", "config.json", true},
		{"state 模块 JSON", "state/memo.json", true},
		{"state 深层", "state/sub/deep.json", true},
		{"memo 文件库", "memo/memo_1.md", true},
		{"原子写残骸", "state/memo.json.tmp.4242", false},
		{"根原子写残骸", "config.json.tmp.9", false},
		{"取证副本", "config.json.corrupt-20260917-143000", false},
		{"迁移留底", "state/memo.json.migrated", false},
		{"明文雷区 runtime", "runtime/frpc/frpc-1.toml", false},
		{"日志", "logs/app-20260917.log", false},
		{"版本仓", "versions/frpc/0.60.0/frpc.exe", false},
		{"安装包", "installers/a.zip", false},
		{"快照自嵌套", ".snapshots/repo.git/HEAD", false},
		{"穿越", "../etc/passwd", false},
		{"中间穿越", "state/../../evil.json", false},
		{"绝对路径", `C:\Windows\x.json`, false},
		{"目录本身", "state/", false},
		{"空串", "", false},
		{"./ 前缀容忍", "./config.json", true},
		{"反斜杠容忍", `state\memo.json`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Whitelisted(c.rel); got != c.want {
				t.Errorf("Whitelisted(%q) = %v, want %v", c.rel, got, c.want)
			}
		})
	}
}

func TestWhitelistRoots(t *testing.T) {
	dir := t.TempDir()
	// 空态：一个都没有
	if got := WhitelistRoots(dir); len(got) != 0 {
		t.Fatalf("empty dir should yield no roots, got %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "state"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "runtime"), 0755); err != nil {
		t.Fatal(err) // 非白名单目录不得出现
	}
	got := WhitelistRoots(dir)
	if len(got) != 2 || got[0] != "config.json" || got[1] != "state" {
		t.Fatalf("WhitelistRoots = %v, want [config.json state]", got)
	}
}

func TestScanMtime(t *testing.T) {
	dir := t.TempDir()
	if _, found := ScanMtime(dir); found {
		t.Fatal("空目录不应发现文件")
	}
	past := time.Now().Add(-time.Hour)
	write := func(rel string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("config.json")
	write("state/ccswitch.json")
	write("runtime/frpc/x.toml") // 白名单外：不得影响水位
	// 手工压低一个"旧"文件的 mtime，验证水位取最大值且只看白名单
	if err := os.Chtimes(filepath.Join(dir, "runtime", "frpc", "x.toml"), past, past); err != nil {
		t.Fatal(err)
	}
	maxMT, found := ScanMtime(dir)
	if !found {
		t.Fatal("应发现白名单文件")
	}
	if maxMT.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("水位应取白名单最新 mtime，got %v", maxMT)
	}
	// 白名单文件全部改到比 runtime 残骸更早：水位若被白名单外的 runtime 文件
	// （-1h）抬起来，说明扫描越界。
	older := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "config.json"), older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "state", "ccswitch.json"), older, older); err != nil {
		t.Fatal(err)
	}
	maxMT2, _ := ScanMtime(dir)
	if maxMT2.After(older.Add(time.Second)) {
		t.Fatalf("白名单外 runtime 文件不应抬升水位: max=%v, older=%v", maxMT2, older)
	}
}
