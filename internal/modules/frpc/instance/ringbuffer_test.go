package instance

import "testing"

// RingBuffer 本体测试已随共享包提取迁至 internal/ringbuf；
// 此处仅保留 frpc 私有的日志脱敏逻辑测试。

func TestRedactText(t *testing.T) {
	got := redactText("auth token=supersecret123 connecting", []string{"supersecret123"})
	if got != "auth token=*** connecting" {
		t.Fatalf("redactText = %q", got)
	}
	// 空串安全
	if redactText("abc", []string{""}) != "abc" {
		t.Fatal("empty secret should be no-op")
	}
	// 无命中保持原样
	if redactText("plain line", []string{"nope"}) != "plain line" {
		t.Fatal("non-matching secret should not alter line")
	}
}
