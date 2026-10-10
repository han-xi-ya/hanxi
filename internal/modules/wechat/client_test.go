package wechat

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 网关体量防线：baseURL 是用户自定义网关（本模块按信任处理该地址），
// 但其响应体量不受信——超限必须拒绝而非无界吸收，且错误文本不得携带大段响应原文。

func TestPostRejectsOversizedResponse(t *testing.T) {
	big := bytes.Repeat([]byte("A"), maxAPIResponseBytes+64<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(big)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	var out map[string]any
	err := client.post(context.Background(), "/ilink/bot/sendmessage", "token", map[string]any{}, &out)
	if err == nil {
		t.Fatal("超限响应被照单全收")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want 超限错误", err)
	}
	// 超限响应体绝不能整份留在错误文本里（error 会一路带进 toast）。
	if len(err.Error()) > 2<<10 {
		t.Fatalf("error text leaks oversized body: len=%d", len(err.Error()))
	}
}

func TestGetTruncatesErrorResponseBody(t *testing.T) {
	body := bytes.Repeat([]byte("B"), 64<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(body)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.get(context.Background(), "/ilink/bot/anything", "", nil)
	if err == nil {
		t.Fatal("500 响应未报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "status 500") || !strings.Contains(msg, "truncated") || !strings.Contains(msg, "65536 bytes total") {
		t.Fatalf("err = %q, want 截断标记 + 状态码", msg)
	}
	if len(msg) > 2<<10 {
		t.Fatalf("错误文本必须保持有界, len=%d", len(msg))
	}
}

func TestUnmarshalErrorTruncatesRawBody(t *testing.T) {
	// 1MB 非法 JSON：在读取上限内，但进 "raw:" 诊断片段的部分必须被截断。
	body := bytes.Repeat([]byte("{"), 1<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	var out map[string]any
	err := client.get(context.Background(), "/ilink/bot/anything", "", &out)
	if err == nil {
		t.Fatal("非法 JSON 未报错")
	}
	if !strings.Contains(err.Error(), "unmarshal") || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("err = %q", err)
	}
	if len(err.Error()) > 2<<10 {
		t.Fatalf("反序列化错误文本必须保持有界, len=%d", len(err.Error()))
	}
}

func TestGetDecodesNormalResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ret":0,"get_updates_buf":"cursor-1","msgs":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	var resp updatesResp
	if err := client.get(context.Background(), "/ilink/bot/getupdates", "token", &resp); err != nil {
		t.Fatalf("正常响应报错: %v", err)
	}
	if resp.Ret != 0 || resp.GetUpdatesBuf != "cursor-1" {
		t.Fatalf("resp = %+v", resp)
	}
}
