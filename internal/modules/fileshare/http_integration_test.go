package fileshare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 真实 TCP 集成测试：完整模拟前端分片续传流程
func TestFileshareChunkedRealHTTP(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fileshare_http_test_*")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := ShareConfig{Port: 0, SharePath: tempDir, AllowUpload: true, AllowTextDrop: true}
	server := NewServer(cfg, nil, nil)
	port, err := server.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer server.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 30 * time.Second}

	// 构造 6MB 测试数据
	content := make([]byte, 6*1024*1024)
	for i := range content {
		content[i] = byte(i % 251)
	}
	name := "big.bin"
	mod := int64(1700000000000)
	q := fmt.Sprintf("dir=&name=%s&size=%d&mod=%d", name, len(content), mod)

	// 1. 查询进度
	resp, err := client.Get(base + "/api/upload/status?" + q)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var st map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if st["uploaded"] != float64(0) {
		t.Fatalf("unexpected initial uploaded: %v", st)
	}

	// 2. 分两段追加 (4MB + 2MB，与前端 CHUNK 相同语义)
	chunk := 4 * 1024 * 1024
	uploaded := 0
	for uploaded < len(content) {
		end := uploaded + chunk
		if end > len(content) {
			end = len(content)
		}
		req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/upload/append?%s&offset=%d", base, q, uploaded), bytes.NewReader(content[uploaded:end]))
		req.Header.Set("Content-Type", "application/octet-stream")
		aresp, err := client.Do(req)
		if err != nil {
			t.Fatalf("append %d: %v", uploaded, err)
		}
		var aj map[string]any
		_ = json.NewDecoder(aresp.Body).Decode(&aj)
		aresp.Body.Close()
		if aresp.StatusCode != http.StatusOK {
			t.Fatalf("append %d failed: %d %v", uploaded, aresp.StatusCode, aj)
		}
		got, _ := aj["uploaded"].(float64)
		if int64(got) != int64(end) {
			t.Fatalf("append %d: server uploaded=%v want=%d", uploaded, aj["uploaded"], end)
		}
		uploaded = end
	}

	// 3. complete
	req, _ := http.NewRequest(http.MethodPost, base+"/api/upload/complete?"+q, nil)
	cresp, err := client.Do(req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusOK {
		t.Fatalf("complete failed: %d", cresp.StatusCode)
	}

	// 4. 校验内容完整
	final, err := os.ReadFile(filepath.Join(tempDir, name))
	if err != nil {
		t.Fatalf("final read: %v", err)
	}
	if !bytes.Equal(final, content) {
		t.Fatalf("final content mismatch: len=%d want=%d", len(final), len(content))
	}
	t.Logf("集成上传 OK: %d bytes, part 分片 %d 段", len(final), 2)
}
