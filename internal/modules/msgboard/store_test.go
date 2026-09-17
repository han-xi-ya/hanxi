package msgboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreDefaultsWhenMissing(t *testing.T) {
	s := newMsgBoardStore(t.TempDir())
	cfg := s.Get()
	if cfg.Text != "" || cfg.Screen != "" {
		t.Fatalf("默认正文/屏幕应为空，实为 %+v", cfg)
	}
	if cfg.FontSize != 64 {
		t.Fatalf("默认字号 64，实为 %d", cfg.FontSize)
	}
	if cfg.Hotkey != defaultHotkey {
		t.Fatalf("默认热键 %q，实为 %q", defaultHotkey, cfg.Hotkey)
	}
	if _, err := os.Stat(s.filePath); !os.IsNotExist(err) {
		t.Fatalf("构造只读不写，不应产生文件：%v", err)
	}
}

// 落盘→重载往返：字段无损；空热键（显式停用）必须与"未设置"区分保留。
func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newMsgBoardStore(dir)
	in := Config{Text: "去开会了，11 点后回来", FontSize: 96, Screen: `\\.\DISPLAY2`, Hotkey: ""}
	out, err := s.Set(in)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if out != in {
		t.Fatalf("Set 应原样返回规范值，得到 %+v", out)
	}

	re := newMsgBoardStore(dir)
	got := re.Get()
	if got != in {
		t.Fatalf("重载后 %+v，期望 %+v", got, in)
	}
}

// 损坏容忍：非法 JSON 视同无内容，内存态保持默认、构造不报错。
func TestStoreCorruptTolerance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "msgboard.json")
	if err := os.WriteFile(path, []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newMsgBoardStore(dir)
	if got := s.Get(); got.FontSize != 64 || got.Hotkey != defaultHotkey {
		t.Fatalf("损坏文件应按默认值继续，实为 %+v", got)
	}
	// 下一次 Set 原子覆盖坏文件（tmp+rename 范式，不留 .tmp 残骸）。
	if _, err := s.Set(Config{FontSize: 40}); err != nil {
		t.Fatalf("Set over corrupt: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Fatalf("原子写失败路径不应残留临时文件：%v", entries)
		}
	}
}

func TestStoreValidation(t *testing.T) {
	s := newMsgBoardStore(t.TempDir())

	if _, err := s.Set(Config{Text: strings.Repeat("长", maxTextRunes+1)}); err == nil {
		t.Fatal("超长正文应报错")
	}
	if _, err := s.Set(Config{Hotkey: "B"}); err == nil {
		t.Fatal("无修饰键热键应报错")
	}

	// 字号钳位：越界不报错、收敛到边界（数值控件防呆兜底）。
	got, err := s.Set(Config{FontSize: 8})
	if err != nil || got.FontSize != minFontSize {
		t.Fatalf("过小字号应钳位到 %d，得 %+v err=%v", minFontSize, got, err)
	}
	got, err = s.Set(Config{FontSize: 9999})
	if err != nil || got.FontSize != maxFontSize {
		t.Fatalf("过大字号应钳位到 %d，得 %+v err=%v", maxFontSize, got, err)
	}

	// 校验失败的 Set 不得污染内存态。
	if _, err := s.Set(Config{Text: strings.Repeat("长", maxTextRunes+1)}); err == nil {
		t.Fatal("超长正文应报错")
	}
	if cur := s.Get(); cur.Text != "" {
		t.Fatalf("失败的 Set 不应写入内存态：%+v", cur)
	}
}

func TestEffectiveText(t *testing.T) {
	if got := effectiveText(Config{}); got != Presets[0] {
		t.Fatalf("空正文应回落首条预设，实为 %q", got)
	}
	if got := effectiveText(Config{Text: "自定义"}); got != "自定义" {
		t.Fatalf("有正文应原样展示，实为 %q", got)
	}
}
