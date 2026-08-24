package fileshare

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesharePathSecurity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fileshare_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建测试目录结构
	subDir := filepath.Join(tempDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	testFile := filepath.Join(subDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("world"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg := ShareConfig{
		Port:          0,
		SharePath:     tempDir,
		AllowUpload:   true,
		AllowTextDrop: true,
	}

	server := NewServer(cfg, nil, nil)

	// 1. 测试合法相对路径解析
	safe, err := server.resolveSafePath("sub/hello.txt")
	if err != nil {
		t.Fatalf("expected legal path to pass, got err: %v", err)
	}
	if safe != testFile {
		t.Errorf("expected %s, got %s", testFile, safe)
	}

	// 2. 测试越界目录穿越攻击 (Path Traversal Attacks)
	dangerousPaths := []string{
		"../secret.txt",
		"..\\secret.txt",
		"sub/../../secret.txt",
		"/etc/passwd",
		"C:\\Windows\\System32\\cmd.exe",
		"../../../../",
	}

	for _, dp := range dangerousPaths {
		_, err := server.resolveSafePath(dp)
		if err == nil {
			t.Errorf("expected path traversal attack to be blocked for: %s", dp)
		}
	}
}

func TestFileshareUploadAndDrop(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fileshare_upload_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := ShareConfig{
		Port:          0,
		SharePath:     tempDir,
		AllowUpload:   true,
		AllowTextDrop: true,
	}

	// 回调为异步触发，通过 channel 接收投递结果
	droppedCh := make(chan DropItem, 1)
	server := NewServer(cfg, func(item DropItem) {
		droppedCh <- item
	}, nil)

	// 测试 1: 文本投递 API (/api/drop)
	dropReqBody, _ := json.Marshal(map[string]string{"content": "https://hubkit.dev/doc"})
	req := httptest.NewRequest(http.MethodPost, "/api/drop", bytes.NewReader(dropReqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleDrop(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for drop, got %d", w.Code)
	}

	select {
	case droppedItem := <-droppedCh:
		if droppedItem.Content != "https://hubkit.dev/doc" || !droppedItem.IsURL {
			t.Errorf("unexpected dropped item: %+v", droppedItem)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("drop hook was not called within timeout")
	}

	// 测试 2: 文件流式上传 API (/api/upload)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test_stream.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = io.WriteString(part, "Hello HubKit Streaming File Upload")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	server.handleUpload(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for upload, got %d (%s)", uploadRec.Code, uploadRec.Body.String())
	}

	uploadedFile := filepath.Join(tempDir, "test_stream.txt")
	content, err := os.ReadFile(uploadedFile)
	if err != nil {
		t.Fatalf("uploaded file not found on disk: %v", err)
	}
	if string(content) != "Hello HubKit Streaming File Upload" {
		t.Errorf("uploaded content mismatch: %s", string(content))
	}
}

func TestFileshareChunkedResumableUpload(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fileshare_resume_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := ShareConfig{
		Port:          0,
		SharePath:     tempDir,
		AllowUpload:   true,
		AllowTextDrop: true,
	}
	server := NewServer(cfg, nil, nil)

	name := "resume.bin"
	content := "0123456789abcdef" // 16 字节

	q := func() string {
		return "dir=&name=" + name + "&size=16&mod=1700000000000"
	}

	// 1. 初始状态: 无已传进度
	req := httptest.NewRequest(http.MethodGet, "/api/upload/status?"+q(), nil)
	wr := httptest.NewRecorder()
	server.handleUploadStatus(wr, req)
	var st map[string]any
	if err := json.Unmarshal(wr.Body.Bytes(), &st); err != nil {
		t.Fatalf("bad status response: %v", err)
	}
	if st["exists"] != false {
		t.Errorf("expected exists=false initially, got %v", st["exists"])
	}

	// 2. 追加前半部分 (10 字节), 模拟已传进度
	req = httptest.NewRequest(http.MethodPost, "/api/upload/append?"+q(), bytes.NewReader([]byte(content[:10])))
	wr = httptest.NewRecorder()
	server.handleUploadAppend(wr, req)
	if wr.Code != http.StatusOK {
		t.Fatalf("append failed: %d %s", wr.Code, wr.Body.String())
	}

	// 3. 再次查询: 应返回已传 10 字节
	req = httptest.NewRequest(http.MethodGet, "/api/upload/status?"+q(), nil)
	wr = httptest.NewRecorder()
	server.handleUploadStatus(wr, req)
	if err := json.Unmarshal(wr.Body.Bytes(), &st); err != nil {
		t.Fatalf("bad status response: %v", err)
	}
	if st["exists"] != true || st["uploaded"] != float64(10) {
		t.Errorf("expected uploaded=10, got %v", st)
	}

	// 4. 追加剩余部分 (6 字节), 模拟续传后的第二段
	req = httptest.NewRequest(http.MethodPost, "/api/upload/append?"+q(), bytes.NewReader([]byte(content[10:])))
	wr = httptest.NewRecorder()
	server.handleUploadAppend(wr, req)
	if wr.Code != http.StatusOK {
		t.Fatalf("append 2 failed: %d %s", wr.Code, wr.Body.String())
	}

	// 5. 临时片文件不应出现在目录列表
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != name+".part.16.1700000000000" {
		t.Errorf("expected part file only, got: %v", entries)
	}

	// 6. 完成合并
	req = httptest.NewRequest(http.MethodPost, "/api/upload/complete?"+q(), nil)
	wr = httptest.NewRecorder()
	server.handleUploadComplete(wr, req)
	if wr.Code != http.StatusOK {
		t.Fatalf("complete failed: %d %s", wr.Code, wr.Body.String())
	}

	// 7. 最终文件内容完整且无残留片文件
	finalContent, err := os.ReadFile(filepath.Join(tempDir, name))
	if err != nil {
		t.Fatalf("final file not found: %v", err)
	}
	if string(finalContent) != content {
		t.Errorf("final content mismatch: %q", string(finalContent))
	}
	entries, _ = os.ReadDir(tempDir)
	if len(entries) != 1 || entries[0].Name() != name {
		t.Errorf("expected only final file, got: %v", entries)
	}
}
