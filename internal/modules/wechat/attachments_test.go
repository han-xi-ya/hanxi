package wechat

import "testing"

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
	if !ok || item.FileName != "test.txt" || item.AccountID != "account-1" {
		t.Fatalf("get() = %+v, %v", item, ok)
	}

	store.deleteAccount("account-1")
	if _, ok := store.get(id); ok {
		t.Fatal("attachment still exists after deleteAccount")
	}
}

func TestAttachmentStoreRejectsIncompleteMedia(t *testing.T) {
	store := newAttachmentStore()
	for _, payload := range []InboundFilePayload{
		{FileName: "a.txt", Media: InboundMedia{AESKey: "key"}},
		{FileName: "a.txt", Media: InboundMedia{EncryptQueryParam: "query"}},
	} {
		if _, err := store.register("account-1", payload); err == nil {
			t.Fatalf("register(%+v) unexpectedly succeeded", payload)
		}
	}
}
