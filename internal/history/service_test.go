package history

import (
	"path/filepath"
	"testing"

	"hanxi/internal/settings"
)

// newTestService 起一个真 Store（t.TempDir 落盘）+ 真 settings.Store 的绑定壳。
func newTestService(t *testing.T) (*HistoryService, *Store) {
	t.Helper()
	store := NewStore(t.TempDir())
	cfg, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return NewHistoryService(store, cfg), store
}

func TestServiceForwardListDeleteClear(t *testing.T) {
	svc, store := newTestService(t)
	if err := store.Save(Record{FuncType: "portkill", Input: "8080"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Record{FuncType: "envcheck", Input: "claude"}); err != nil {
		t.Fatal(err)
	}

	list, err := svc.List("portkill", "")
	if err != nil || len(list) != 1 || list[0].Input != "8080" {
		t.Fatalf("List 转发失真: %+v %v", list, err)
	}
	if err := svc.Delete(list[0].ID); err != nil {
		t.Fatalf("Delete 转发失败: %v", err)
	}
	if got, _ := svc.List("portkill", ""); len(got) != 0 {
		t.Fatal("Delete 未透传到桶")
	}
	if err := svc.Clear("envcheck"); err != nil {
		t.Fatalf("Clear 转发失败: %v", err)
	}
	if got, _ := svc.List("envcheck", ""); len(got) != 0 {
		t.Fatal("Clear 未透传到桶")
	}
}

func TestServiceOcrFullTextTogglePersisted(t *testing.T) {
	svc, _ := newTestService(t)
	// Q1 裁定：默认开=全文入库
	if v, err := svc.GetOcrFullText(); err != nil || !v {
		t.Fatalf("默认档位应为全文入库: %v %v", v, err)
	}
	if err := svc.SetOcrFullText(false); err != nil {
		t.Fatal(err)
	}
	if v, _ := svc.GetOcrFullText(); v {
		t.Fatal("关闭后应读到 false")
	}
	// settings.Store 落盘真实生效（config.json 往返）
	if err := svc.SetOcrFullText(true); err != nil {
		t.Fatal(err)
	}
	if v, _ := svc.GetOcrFullText(); !v {
		t.Fatal("重开后应读到 true")
	}
}

func TestServiceNilSettingsDefaultsFull(t *testing.T) {
	svc := NewHistoryService(NewStore(t.TempDir()), nil)
	if v, err := svc.GetOcrFullText(); err != nil || !v {
		t.Fatalf("无配置存储时默认全文开: %v %v", v, err)
	}
	if err := svc.SetOcrFullText(false); err != nil {
		t.Fatal("无配置存储时设定应无害")
	}
}
