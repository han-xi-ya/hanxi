package history

import (
	"testing"
)

// newTestService 起一个真 Store（t.TempDir 落盘）的绑定壳。
func newTestService(t *testing.T) (*HistoryService, *Store) {
	t.Helper()
	store := NewStore(t.TempDir())
	return NewHistoryService(store), store
}

func TestServiceForwardListDeleteClear(t *testing.T) {
	svc, store := newTestService(t)
	if err := store.Save(Record{FuncType: "portkill", Input: "8080"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Record{FuncType: "memo", Input: "片段"}); err != nil {
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
	if err := svc.Clear("memo"); err != nil {
		t.Fatalf("Clear 转发失败: %v", err)
	}
	if got, _ := svc.List("memo", ""); len(got) != 0 {
		t.Fatal("Clear 未透传到桶")
	}
}
