package version

// fetchJSON / readCapped 的行为锁（照 dbx 同名单元测的形状适配本模块）。
//
// 起因是 dbx 的真实故障：per_page 页响应越限，而 io.LimitReader 截断不报错，
// 半截 JSON 被解析层报成 "unexpected end of JSON input"——"响应太大"这件确定
// 事实被伪装成"上游格式变了"。本模块 per_page=60 实测约 3.8 MB，当下不炸，
// 但一次上游 changelog 变长就会走到同一条路。这里的用例专门盯两件事：截断
// 必须被认出来（且绝不交给解析层），以及只有"过大"才值得降页重试。

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadCapped(t *testing.T) {
	cases := []struct {
		name     string
		data     string
		limit    int
		wantLen  int
		wantOver bool
	}{
		{"小于上限", "hello", 100, 5, false},
		{"正好等于上限", "abcd", 4, 4, false}, // 恰好读完不能被误报成超限
		{"超过上限", "abcdef", 4, 4, true},
		{"零长度内容", "", 4, 0, false},
	}
	for _, c := range cases {
		got, over, err := readCapped(strings.NewReader(c.data), c.limit)
		if err != nil {
			t.Errorf("%s: 不该有读错误: %v", c.name, err)
			continue
		}
		if len(got) != c.wantLen || over != c.wantOver {
			t.Errorf("%s: 期望 len=%d over=%v，实得 len=%d over=%v", c.name, c.wantLen, c.wantOver, len(got), over)
		}
	}

	wantErr := errors.New("boom")
	if _, _, err := readCapped(errReader{wantErr}, 10); !errors.Is(err, wantErr) {
		t.Errorf("读错误必须原样上报，实得 %v", err)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestFetchJSONDowngradesOnlyOnOversize(t *testing.T) {
	prev := maxFeedBytes
	maxFeedBytes = 64 // 压低上限，让"超限"在单测里可达（生产不改它）
	t.Cleanup(func() { maxFeedBytes = prev })

	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("per_page"))
		if r.URL.Query().Get("per_page") == fmt.Sprint(perPageFirst) {
			_, _ = io.WriteString(w, strings.Repeat("x", maxFeedBytes+10)) // 超限
			return
		}
		_, _ = io.WriteString(w, `[{"tag_name":"v6.17.6"}]`)
	}))
	defer srv.Close()

	body, err := fetchJSON([]string{srv.URL})
	if err != nil {
		t.Fatalf("降页后应成功: %v", err)
	}
	if !bytes.Contains(body, []byte("v6.17.6")) {
		t.Fatalf("返回的不是小页内容: %s", body)
	}
	if len(seen) != 2 || seen[0] != fmt.Sprint(perPageFirst) || seen[1] != fmt.Sprint(perPageSmall) {
		t.Errorf("应按 %d → %d 两级降级请求，实得 %v", perPageFirst, perPageSmall, seen)
	}
}

// TestFetchJSONNoDowngradeOnOtherFailures 锁另一半：网络失败/非 200 **不许**用降页
// 掩盖，否则真实原因（限流、断网、UA 被封）会被伪装成"试了两轮都不行"。
func TestFetchJSONNoDowngradeOnOtherFailures(t *testing.T) {
	prev := maxFeedBytes
	maxFeedBytes = 64
	t.Cleanup(func() { maxFeedBytes = prev })

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := fetchJSON([]string{srv.URL})
	if err == nil {
		t.Fatal("全源 403 必须报错")
	}
	if errors.Is(err, errResponseTooLarge) {
		t.Errorf("非体积故障不该被归成超限: %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("错误里要留着真实状态码便于排查，实得 %v", err)
	}
	if hits != 1 {
		t.Errorf("403 时不该降页重试（每源各试一次即可），实得请求数 %d", hits)
	}
}

// TestFetchJSONMirrorsTriedOnceOn403 多镜像下的 403 形态：每个源都只许按首档
// per_page 试一次——既不能漏源，更不能把限流滚成"两轮全源轰炸"。
func TestFetchJSONMirrorsTriedOnceOn403(t *testing.T) {
	prev := maxFeedBytes
	maxFeedBytes = 64
	t.Cleanup(func() { maxFeedBytes = prev })

	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("per_page"))
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := fetchJSON([]string{srv.URL, srv.URL})
	if err == nil {
		t.Fatal("全源 403 必须报错")
	}
	if errors.Is(err, errResponseTooLarge) {
		t.Errorf("非体积故障不该被归成超限: %v", err)
	}
	if len(seen) != 2 {
		t.Errorf("两个源应各试一次，实得请求序列 %v", seen)
	}
	for _, p := range seen {
		if p != fmt.Sprint(perPageFirst) {
			t.Errorf("403 不应触发降页，实得 per_page=%v", seen)
			break
		}
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("错误里要留着真实状态码便于排查，实得 %v", err)
	}
}

// TestFetchJSONNeverReturnsTruncatedBody 锁"超限的响应绝不交给解析层"：
// 所有页都超限时必须报错（错误链保留超限哨兵与两轮 per_page 轨迹），
// 决不许把半截 JSON 当成正常结果返回。
func TestFetchJSONNeverReturnsTruncatedBody(t *testing.T) {
	prev := maxFeedBytes
	maxFeedBytes = 64
	t.Cleanup(func() { maxFeedBytes = prev })

	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("per_page"))
		_, _ = io.WriteString(w, strings.Repeat("x", maxFeedBytes+10)) // 无论哪档都超限
	}))
	defer srv.Close()

	body, err := fetchJSON([]string{srv.URL})
	if err == nil {
		t.Fatalf("两轮都超限必须报错，实得 body len=%d", len(body))
	}
	if body != nil {
		t.Errorf("超限响应绝不许返回给解析层，实得 len=%d", len(body))
	}
	if !errors.Is(err, errResponseTooLarge) {
		t.Errorf("错误应保留超限哨兵便于分诊，实得 %v", err)
	}
	if len(seen) != 2 || seen[0] != fmt.Sprint(perPageFirst) || seen[1] != fmt.Sprint(perPageSmall) {
		t.Errorf("应按 %d → %d 两级降级后仍失败才放弃，实得 %v", perPageFirst, perPageSmall, seen)
	}
}
