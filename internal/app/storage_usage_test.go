package app

import (
	"reflect"
	"testing"

	"hanxi/packages/go/dirstats"
)

// TestGroupBySoftware versions 下钻聚合语义钉死（W3-b）：`<模块>_<版本>` 按首个
// 下划线前缀聚合成每软件一行；非目录、无下划线、下划线开头等均自成一组如实呈现；
// Partial 取或、计数求和、按 Bytes 降序。
func TestGroupBySoftware(t *testing.T) {
	children := []dirstats.Child{
		{Name: "paseo_1.0.0", IsDir: true, Stats: dirstats.Stats{Bytes: 500, Files: 5, Partial: true, ErrorCount: 1}},
		{Name: "paseo_0.9.0-beta.2", IsDir: true, Stats: dirstats.Stats{Bytes: 300, Files: 3}},
		{Name: "markeron_0.5.0", IsDir: true, Stats: dirstats.Stats{Bytes: 120, Files: 2}},
		{Name: "stray.txt", Stats: dirstats.Stats{Bytes: 10, Files: 1}},
		{Name: "_leading", IsDir: true, Stats: dirstats.Stats{Bytes: 7}},
	}
	got := groupBySoftware(children)

	if len(got) != 4 {
		t.Fatalf("分组数 = %d, want 4: %+v", len(got), got)
	}
	wantOrder := []string{"paseo", "markeron", "stray.txt", "_leading"}
	for i, want := range wantOrder {
		if got[i].Name != want {
			t.Errorf("第 %d 组 = %q, want %q", i, got[i].Name, want)
		}
	}
	p := got[0]
	if p.Bytes != 800 || p.Files != 8 || p.ErrorCount != 1 || !p.Partial {
		t.Errorf("paseo 聚合异常: %+v", p)
	}
	if !reflect.DeepEqual(p.Entries, []string{"paseo_1.0.0", "paseo_0.9.0-beta.2"}) {
		t.Errorf("paseo Entries = %v", p.Entries)
	}
	// 文件（非目录）不做前缀切分；下划线开头的名字不切（i>0 才切），均如实自成组。
	if m := got[1]; m.Bytes != 120 || !reflect.DeepEqual(m.Entries, []string{"markeron_0.5.0"}) {
		t.Errorf("markeron 聚合异常: %+v", m)
	}
}

// TestGroupBySoftwareEmpty 空输入不炸。
func TestGroupBySoftwareEmpty(t *testing.T) {
	if got := groupBySoftware(nil); len(got) != 0 {
		t.Fatalf("空输入应回空组: %+v", got)
	}
}
