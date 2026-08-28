package wechat

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadInboundFile(t *testing.T) {
	plaintext := []byte("hubkit wechat attachment")
	key := []byte("0123456789abcdef")
	ciphertext, err := aesEcbEncrypt(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("encrypted_query_param"); got != "a+b/c=&%" {
			t.Fatalf("query = %q", got)
		}
		w.Write(ciphertext)
	}))
	defer server.Close()

	client := NewClient("")
	client.cdnBase = server.URL
	client.httpClient = server.Client()
	keyHex := hex.EncodeToString(key)
	data, err := client.DownloadInboundFile(context.Background(), InboundMedia{
		EncryptQueryParam: "a+b/c=&%",
		AESKey:            base64.StdEncoding.EncodeToString([]byte(keyHex)),
		EncryptType:       1,
	}, int64(len(ciphertext)))
	if err != nil {
		t.Fatalf("DownloadInboundFile() error = %v", err)
	}
	if string(data) != string(plaintext) {
		t.Fatalf("data = %q", data)
	}
}

func TestDownloadInboundFileIgnoresReportedLength(t *testing.T) {
	plaintext := []byte("wechat reports a different length")
	key := []byte("0123456789abcdef")
	ciphertext, err := aesEcbEncrypt(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(ciphertext)
	}))
	defer server.Close()

	client := NewClient("")
	client.cdnBase = server.URL
	client.httpClient = server.Client()
	data, err := client.DownloadInboundFile(context.Background(), InboundMedia{
		EncryptQueryParam: "query",
		AESKey:            base64.StdEncoding.EncodeToString(key),
	}, 1)
	if err != nil {
		t.Fatalf("DownloadInboundFile() error = %v", err)
	}
	if string(data) != string(plaintext) {
		t.Fatalf("data = %q", data)
	}
}

func TestDownloadInboundFileAcceptsMissingEncryptType(t *testing.T) {
	plaintext := []byte("excel attachment")
	key := []byte("0123456789abcdef")
	ciphertext, err := aesEcbEncrypt(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(ciphertext)
	}))
	defer server.Close()

	client := NewClient("")
	client.cdnBase = server.URL
	client.httpClient = server.Client()
	data, err := client.DownloadInboundFile(context.Background(), InboundMedia{
		EncryptQueryParam: "query",
		AESKey:            base64.StdEncoding.EncodeToString(key),
	}, int64(len(ciphertext)))
	if err != nil {
		t.Fatalf("DownloadInboundFile() error = %v", err)
	}
	if string(data) != string(plaintext) {
		t.Fatalf("data = %q", data)
	}
}

func TestParseInboundAESKey(t *testing.T) {
	key := []byte("0123456789abcdef")
	for _, encoded := range []string{
		base64.StdEncoding.EncodeToString(key),
		base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key))),
	} {
		got, err := parseInboundAESKey(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(key) {
			t.Fatalf("key = %q", got)
		}
	}

	if _, err := parseInboundAESKey(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 15)))); err == nil {
		t.Fatal("invalid key unexpectedly succeeded")
	}
}
