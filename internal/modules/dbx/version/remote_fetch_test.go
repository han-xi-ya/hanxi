package version

// fetchJSON / readCapped 的行为锁。
//
// 起因是真实故障：`releases?per_page=100` 响应 13.3 MB，而读取上限 8 MiB 把响应
// 静默截断，解析层于是报 "unexpected end of JSON input"——"响应太大"这件确定事实
// 被伪装成"上游格式变了"。这里的用例专门盯两件事：截断必须被认出来，以及
// 只有"过大"才值得降页重试。

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
		_, _ = io.WriteString(w, `[{"tag_name":"1.2.3"}]`)
	}))
	defer srv.Close()

	body, err := fetchJSON([]string{srv.URL})
	if err != nil {
		t.Fatalf("降页后应成功: %v", err)
	}
	if !bytes.Contains(body, []byte("1.2.3")) {
		t.Fatalf("返回的不是小页内容: %s", body)
	}
	if len(seen) != 2 || seen[0] != fmt.Sprint(perPageFirst) || seen[1] != fmt.Sprint(perPageSmall) {
		t.Errorf("应按 30 → 10 两级降级请求，实得 %v", seen)
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
