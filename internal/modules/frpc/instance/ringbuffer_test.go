package instance

import (
	"reflect"
	"testing"
)

func TestRingBufferBasic(t *testing.T) {
	rb := newRingBuffer(3)
	rb.Write("a")
	rb.Write("b")
	rb.Write("c")

	got := rb.Last(0)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("Last(0) = %v, want [a b c]", got)
	}
	if rb.Len() != 3 {
		t.Fatalf("Len = %d, want 3", rb.Len())
	}

	// 写满后覆盖最旧
	rb.Write("d")
	got = rb.Last(0)
	if !reflect.DeepEqual(got, []string{"b", "c", "d"}) {
		t.Fatalf("after overflow Last(0) = %v, want [b c d]", got)
	}
	if rb.Len() != 3 {
		t.Fatalf("Len = %d, want 3 (capacity bound)", rb.Len())
	}
}

func TestRingBufferLastN(t *testing.T) {
	rb := newRingBuffer(5)
	for _, s := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		rb.Write(s)
	}
	// 当前有效 [3 4 5 6 7]
	got := rb.Last(3)
	if !reflect.DeepEqual(got, []string{"5", "6", "7"}) {
		t.Fatalf("Last(3) = %v, want [5 6 7]", got)
	}
	got = rb.Last(5)
	if !reflect.DeepEqual(got, []string{"3", "4", "5", "6", "7"}) {
		t.Fatalf("Last(5) = %v, want [3 4 5 6 7]", got)
	}
	// 负数 = 全部
	got = rb.Last(-1)
	if len(got) != 5 {
		t.Fatalf("Last(-1) len = %d, want 5", len(got))
	}
}

func TestRingBufferTrimsNewline(t *testing.T) {
	rb := newRingBuffer(2)
	rb.Write("line1\r\n")
	rb.Write("line2")
	got := rb.Last(0)
	if len(got) != 2 || got[0] != "line1" {
		t.Fatalf("got %v, want [line1 line2]", got)
	}
}

func TestRingBufferIgnoresEmpty(t *testing.T) {
	rb := newRingBuffer(2)
	rb.Write("")
	rb.Write("\r\n")
	if rb.Len() != 0 {
		t.Fatalf("empty lines should be ignored, Len = %d", rb.Len())
	}
}

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