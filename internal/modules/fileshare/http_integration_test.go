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

// 真实 TCP 集成测试：完整模拟 Web 页面的单次二进制流上传流程。
func TestFileshareSingleStreamRealHTTP(t *testing.T) {
	tempDir := t.TempDir()
	cfg := ShareConfig{Port: 0, SharePath: tempDir, AllowUpload: true, AllowTextDrop: true}
	server := NewServer(cfg, nil, nil)
	port, err := server.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer server.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 30 * time.Second}
	content := make([]byte, 6*1024*1024)
	for i := range content {
		content[i] = byte(i % 251)
	}
	name := "big.bin"
	query := fmt.Sprintf("dir=&name=%s&size=%d", name, len(content))

	req, err := http.NewRequest(http.MethodPost, base+"/api/upload?"+query, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		resp.Body.Close()
		t.Fatalf("decode response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload failed: %d %v", resp.StatusCode, result)
	}
	if !resp.Close {
		t.Fatal("upload response did not close the TCP connection")
	}
	if result["size"] != float64(len(content)) {
		t.Fatalf("server size=%v want=%d", result["size"], len(content))
	}

	final, err := os.ReadFile(filepath.Join(tempDir, name))
	if err != nil {
		t.Fatalf("final read: %v", err)
	}
	if !bytes.Equal(final, content) {
		t.Fatalf("final content mismatch: len=%d want=%d", len(final), len(content))
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		t.Fatalf("temporary upload file remained: %v", entries)
	}
}
