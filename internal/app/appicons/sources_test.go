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

// menuPx 是菜单变体档的出货边长（SM_CXSMICON 常规档）。托盘消费位
// （tray.go→ForMenu）依赖该恒定性，改档必须连托盘观感一起重验。
const menuPx = 16

// 菜单变体双向锁：每个登记真图标必有 menus/<id>.png 对偶且 IHDR 恒 16²；
// menus/ 下不允许出现未登记的幽灵变体。升切新增件时漏出变体，这里先红。
func TestMenuVariantsPaired(t *testing.T) {
	variants, err := fs.Glob(MenuFS(), "menus/*.png")
	if err != nil {
		t.Fatalf("变体清单异常: %v", err)
	}
	for _, name := range variants {
		id := strings.TrimSuffix(strings.TrimPrefix(name, "menus/"), ".png")
		if _, ok := sourcePx[id]; !ok {
			t.Errorf("%s 系幽灵变体（sourcePx 无登记）", name)
		}
	}
	for id := range sourcePx {
		data, err := fs.ReadFile(MenuFS(), "menus/"+id+".png")
		if err != nil {
			t.Errorf("登记件 %s 缺 16px 菜单变体（新增图标须同批出变体）", id)
			continue
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Errorf("menus/%s.png 非可解码 PNG: %v", id, err)
			continue
		}
		if cfg.Width != menuPx || cfg.Height != menuPx {
			t.Errorf("menus/%s.png 边长 %dx%d ≠ 变体档 %d²", id, cfg.Width, cfg.Height, menuPx)
		}
	}
}

// ForMenu 语义：登记件出 16px PNG 字节流；未登记/空恒 nil（绝不回退展示档）。
func TestForMenuSemantics(t *testing.T) {
	pngBytes := ForMenu("ccswitch")
	if len(pngBytes) < 8 || string(pngBytes[1:4]) != "PNG" {
		t.Fatalf("ccswitch 变体缺失或非 PNG 流: len=%d", len(pngBytes))
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(pngBytes)); err != nil || cfg.Width != menuPx {
		t.Errorf("ccswitch 变体边长异常: %v %+v", err, cfg)
	}
	if ForMenu("no-such-module") != nil {
		t.Error("无变体模块必须恒 nil（回落文字菜单，不给图）")
	}
	if ForMenu("") != nil {
		t.Error("空模块 ID 必须恒 nil")
	}
}
