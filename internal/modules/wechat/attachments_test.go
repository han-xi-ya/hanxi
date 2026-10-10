package wechat

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestSanitizeInboundFileName(t *testing.T) {
	tests := map[string]string{
		`../../evil.exe`:     "evil.exe",
		`..\\..\\report.pdf`: "report.pdf",
		`a<b>c.txt`:          "a_b_c.txt",
		`...`:                "微信文件",
		``:                   "微信文件",
	}
	for input, want := range tests {
		if got := sanitizeInboundFileName(input); got != want {
			t.Errorf("sanitizeInboundFileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAttachmentStoreRegister(t *testing.T) {
	store := newAttachmentStore()
	defer store.clear()
	id, err := store.register("account-1", InboundFilePayload{
		FileName: "test.txt",
		Len:      16,
		Media: InboundMedia{
			EncryptQueryParam: "query",
			AESKey:            "key",
			EncryptType:       1,
		},
	})
	if err != nil {
		t.Fatalf("register() error = %v", err)
	}
	if id == "" {
		t.Fatal("register() returned empty id")
	}
	item, ok := store.get(id)
	if !ok || item.FileName != "test.txt" || item.AccountID != "account-1" || item.Kind != kindFile {
		t.Fatalf("get() = %+v, %v", item, ok)
	}

	store.deleteAccount("account-1")
	if _, ok := store.get(id); ok {
		t.Fatal("attachment still exists after deleteAccount")
	}
}

func TestAttachmentStoreRegisterImage(t *testing.T) {
	store := newAttachmentStore()
	defer store.clear()
	id, err := store.registerImage("account-1", "微信图片_20260906_120304.jpg", InboundMedia{
		EncryptQueryParam: "query",
		AESKey:            "key",
		EncryptType:       1,
	})
	if err != nil {
		t.Fatalf("registerImage() error = %v", err)
	}
	item, ok := store.get(id)
	// 图片消息不携带长度：FileSize 恒 0；Kind 决定预览通道准入。
	if !ok || item.Kind != kindImage || item.FileName != "微信图片_20260906_120304.jpg" || item.FileSize != 0 {
		t.Fatalf("get() = %+v, %v", item, ok)
	}
}

func TestAttachmentStoreRejectsIncompleteMedia(t *testing.T) {
	store := newAttachmentStore()
	defer store.clear()
	for _, payload := range []InboundFilePayload{
		{FileName: "a.txt", Media: InboundMedia{AESKey: "key"}},
		{FileName: "a.txt", Media: InboundMedia{EncryptQueryParam: "query"}},
	} {
		if _, err := store.register("account-1", payload); err == nil {
			t.Fatalf("register(%+v) unexpectedly succeeded", payload)
		}
	}
}

// attachmentTTLTestClock 假时钟：由测试显式推进，TTL 边界推演不靠 sleep。
func attachmentTTLTestClock(base time.Time) (*attachmentStore, func(time.Duration)) {
	store := newAttachmentStore()
	now := base
	store.now = func() time.Time { return now }
	return store, func(d time.Duration) { now = now.Add(d) }
}

func TestAttachmentStoreTTLExpiresEntries(t *testing.T) {
	base := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	store, advance := attachmentTTLTestClock(base)
	defer store.clear()

	id, err := store.register("acc", InboundFilePayload{
		FileName: "chat.txt",
		Media:    InboundMedia{EncryptQueryParam: "query", AESKey: "key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := store.stageOpenedFile(id, "chat.txt", []byte("secret plaintext"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("staged copy missing: %v", err)
	}

	// 未过 TTL：注册项与明文副本都在
	if _, ok := store.get(id); !ok {
		t.Fatal("entry expired before TTL elapsed")
	}

	// 越过 TTL："附件不存在或已过期"话术自此有真实机制支撑——
	// 查询按不存在口径返回，且注册项与名下受管明文副本一并即刻回收。
	advance(attachmentTTL + time.Second)
	if _, ok := store.get(id); ok {
		t.Fatal("entry still queryable after TTL")
	}
	if _, err := os.Stat(copied); !os.IsNotExist(err) {
		t.Fatalf("TTL 过期后受管明文副本仍残留: %v", err)
	}
	store.mu.RLock()
	items, opened := len(store.items), len(store.opened)
	store.mu.RUnlock()
	if items != 0 || opened != 0 {
		t.Fatalf("items=%d opened=%d, want 0/0", items, opened)
	}
}

func TestAttachmentStoreMaxItemsEvictsOldest(t *testing.T) {
	store := newAttachmentStore()
	defer store.clear()
	// 每取一次时钟走 1 秒：注册时序严格全序，"按最旧淘汰"可判定；
	// 总推进量远小于 TTL，本用例只压条数上限这一条线。
	base := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	tick := 0
	store.now = func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Second)
	}

	const total = attachmentMaxItems + 10
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		id, err := store.register("acc", InboundFilePayload{
			FileName: fmt.Sprintf("f%04d.bin", i),
			Media:    InboundMedia{EncryptQueryParam: "query", AESKey: "key"},
		})
		if err != nil {
			t.Fatalf("register #%d: %v", i, err)
		}
		ids = append(ids, id)
	}

	store.mu.RLock()
	n := len(store.items)
	store.mu.RUnlock()
	if n != attachmentMaxItems {
		t.Fatalf("items = %d, want %d", n, attachmentMaxItems)
	}
	if _, ok := store.get(ids[0]); ok {
		t.Fatal("最旧注册项未被上限淘汰")
	}
	if _, ok := store.get(ids[total-1]); !ok {
		t.Fatal("最新注册项被误淘汰")
	}
}

func TestAttachmentStoreDeleteAccountReclaimsOpenedCopy(t *testing.T) {
	store := newAttachmentStore()
	defer store.clear()

	id, err := store.register("acc", InboundFilePayload{
		FileName: "note.txt",
		Media:    InboundMedia{EncryptQueryParam: "query", AESKey: "key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := store.stageOpenedFile(id, "note.txt", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}

	store.deleteAccount("acc")
	if _, ok := store.openedPath(id); ok {
		t.Fatal("deleteAccount 后明文副本登记仍在")
	}
	if _, err := os.Stat(copied); !os.IsNotExist(err) {
		t.Fatalf("deleteAccount 未回收受管明文副本: %v", err)
	}
}

func TestAttachmentStoreClearRemovesOpenDir(t *testing.T) {
	store := newAttachmentStore()
	id, err := store.register("acc", InboundFilePayload{
		FileName: "a.txt",
		Media:    InboundMedia{EncryptQueryParam: "query", AESKey: "key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.stageOpenedFile(id, "a.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	dir := store.openDir
	store.mu.RUnlock()
	if dir == "" {
		t.Fatal("受管目录未惰性创建")
	}

	store.clear()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("clear 后受管目录未整目录回收: %v", err)
	}
}
