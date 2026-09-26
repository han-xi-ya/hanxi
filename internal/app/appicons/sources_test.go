package appicons

import (
	"bytes"
	"image/png"
	"io/fs"
	"strings"
	"testing"
)

// 升切台账执法条：sourcePx 表与内嵌件**双向锁死**——
//  1. 每个内嵌 PNG 必须有登记，且登记边长 == PNG 实际解码边长（正方形）；
//  2. 表里不允许出现未内嵌的幽灵键；
//  3. SourcePx 未登记模块恒 0（消费面据此走回落轨）。
//
// 任何人换图、加图漏登记、或把小图“放大凑 64”都会在这里变红。
func TestSourcePxMatchesEmbedded(t *testing.T) {
	files, err := fs.Glob(FS(), "*.png")
	if err != nil {
		t.Fatalf("内嵌清单异常: %v", err)
	}
	for _, name := range files {
		id := strings.TrimSuffix(name, ".png")
		want, ok := sourcePx[id]
		if !ok {
			t.Errorf("%s 已内嵌但未登记 sourcePx（新增图标须同批登台账）", name)
			continue
		}
		data, err := FS().(fs.ReadFileFS).ReadFile(name)
		if err != nil {
			t.Errorf("读取 %s 失败: %v", name, err)
			continue
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Errorf("%s 非可解码 PNG: %v", name, err)
			continue
		}
		if cfg.Width != cfg.Height {
			t.Errorf("%s 非正方形: %dx%d", name, cfg.Width, cfg.Height)
		}
		if cfg.Width != want {
			t.Errorf("%s 边长漂移: 内嵌 %dpx ≠ 台账 %dpx", name, cfg.Width, want)
		}
	}
	for id := range sourcePx {
		if _, err := fs.Stat(FS(), id+".png"); err != nil {
			t.Errorf("台账键 %s 无对应内嵌件（幽灵登记）", id)
		}
	}
}

func TestSourcePxUnknown(t *testing.T) {
	if SourcePx("no-such-module") != 0 {
		t.Error("未登记模块必须返回 0")
	}
	if SourcePx("") != 0 {
		t.Error("空模块 ID 必须返回 0")
	}
}
