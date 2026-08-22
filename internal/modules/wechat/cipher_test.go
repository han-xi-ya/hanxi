package wechat

import (
	"bytes"
	"testing"
)

func TestAESECBEncryptDecrypt(t *testing.T) {
	key := []byte("1234567890123456") // 16 bytes
	testCases := [][]byte{
		[]byte("hello world"),
		[]byte("exact 16 bytes!!"),
		[]byte("a slightly longer string that spans across multiple aes 16-byte blocks with padding"),
		[]byte(""),
	}

	for _, tc := range testCases {
		enc, err := aesEcbEncrypt(tc, key)
		if err != nil {
			t.Fatalf("encrypt failed for %q: %v", string(tc), err)
		}

		if len(enc)%16 != 0 {
			t.Fatalf("cipher length %d is not multiple of 16", len(enc))
		}

		dec, err := aesEcbDecrypt(enc, key)
		if err != nil {
			t.Fatalf("decrypt failed for %q: %v", string(tc), err)
		}

		if !bytes.Equal(dec, tc) {
			t.Fatalf("mismatch: expected %q, got %q", string(tc), string(dec))
		}
	}
}

func TestAESECBPaddedSize(t *testing.T) {
	tests := []struct {
		raw      int
		expected int
	}{
		{0, 16},
		{1, 16},
		{15, 16},
		{16, 32},
		{17, 32},
		{31, 32},
		{32, 48},
	}

	for _, tc := range tests {
		got := aesEcbPaddedSize(tc.raw)
		if got != tc.expected {
			t.Errorf("raw %d: expected padded size %d, got %d", tc.raw, tc.expected, got)
		}
	}
}
