package fileshare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/modules/fileshare/web"
)

func TestFileshareWebAssets(t *testing.T) {
	server := NewServer(ShareConfig{}, nil, nil)
	assetsFS, err := fs.Sub(web.DistFS, "assets")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/assets/", server.handleAssets(http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS)))))
	mux.HandleFunc("/", server.handleIndex)

	tests := []struct {
		name        string
		method      string
		path        string
		status      int
		mediaType   string
		bodyContain string
	}{
		{name: "index", method: http.MethodGet, path: "/", status: http.StatusOK, mediaType: "text/html", bodyContain: "/assets/js/app.js"},
		{name: "stylesheet", method: http.MethodGet, path: "/assets/app.css", status: http.StatusOK, mediaType: "text/css", bodyContain: ".stats-grid"},
		{name: "app module", method: http.MethodGet, path: "/assets/js/app.js", status: http.StatusOK, mediaType: "text/javascript", bodyContain: "createFileBrowser"},
		{name: "upload module", method: http.MethodGet, path: "/assets/js/upload.js", status: http.StatusOK, mediaType: "text/javascript", bodyContain: "XMLHttpRequest"},
		{name: "head", method: http.MethodHead, path: "/assets/app.css", status: http.StatusOK, mediaType: "text/css"},
		{name: "method rejected", method: http.MethodPost, path: "/assets/app.css", status: http.StatusMethodNotAllowed},
		{name: "directory rejected", method: http.MethodGet, path: "/assets/", status: http.StatusNotFound},
		{name: "missing asset", method: http.MethodGet, path: "/assets/missing.css", status: http.StatusNotFound},
		{name: "unknown page", method: http.MethodGet, path: "/missing", status: http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.mediaType != "" {
				got, _, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
				if err != nil || got != tc.mediaType {
					t.Fatalf("Content-Type=%q want=%q err=%v", rec.Header().Get("Content-Type"), tc.mediaType, err)
				}
			}
			if tc.bodyContain != "" && !strings.Contains(rec.Body.String(), tc.bodyContain) {
				t.Fatalf("body missing %q", tc.bodyContain)
			}
		})
	}

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRec := httptest.NewRecorder()
	mux.ServeHTTP(indexRec, indexReq)
	for _, inline := range []string{"onclick=", "onchange=", "onkeydown="} {
		if strings.Contains(indexRec.Body.String(), inline) {
			t.Fatalf("index still contains inline handler %q", inline)
		}
	}
}

func TestFileshareIndexDisablesBrowserCache(t *testing.T) {
	server := NewServer(ShareConfig{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	server.handleIndex(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control=%q, want no-store", got)
	}
}

func TestByteCountingReaderReportsStreamingProgress(t *testing.T) {
	var counted int64
	reader := &byteCountingReader{
		reader: strings.NewReader("streaming"),
		onRead: func(n int64) {
			counted += n
		},
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "streaming" || counted != int64(len(content)) {
		t.Fatalf("content=%q counted=%d", content, counted)
	}
}

func TestFileshareSingleStreamUploadValidationAndCleanup(t *testing.T) {
	tempDir := t.TempDir()
	server := NewServer(ShareConfig{
		SharePath:       tempDir,
		AllowUpload:     true,
		MaxUploadSizeMB: 1,
	}, nil, nil)

	tests := []struct {
		name string
		url  string
		body string
		code int
	}{
		{
			name: "short body",
			url:  "/api/upload?dir=&name=short.bin&size=10",
			body: "short",
			code: http.StatusBadRequest,
		},
		{
			name: "declared over limit",
			url:  "/api/upload?dir=&name=large.bin&size=1048577",
			body: "x",
			code: http.StatusRequestEntityTooLarge,
		},
		{
			name: "path traversal",
			url:  "/api/upload?dir=..%2Foutside&name=escape.bin&size=1",
			body: "x",
			code: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.url, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			server.handleUpload(rec, req)
			if rec.Code != tc.code {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.code, rec.Body.String())
			}
		})
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed uploads left files behind: %v", entries)
	}
}

func TestFileshareSingleStreamUploadAvoidsOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	server := NewServer(ShareConfig{SharePath: tempDir, AllowUpload: true}, nil, nil)
	if err := os.WriteFile(filepath.Join(tempDir, "same.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	body := "new"
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/upload?dir=&name=same.txt&size=3",
		strings.NewReader(body),
	)
	rec := httptest.NewRecorder()
	server.handleUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(tempDir, "same.txt")); string(got) != "old" {
		t.Fatalf("existing file overwritten: %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(tempDir, "same (*).txt"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("renamed upload not found: %v err=%v", matches, err)
	}
	if got, _ := os.ReadFile(matches[0]); string(got) != body {
		t.Fatalf("renamed upload content=%q", got)
	}
}

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
	dropReqBody, _ := json.Marshal(map[string]string{"content": "https://hanxi.dev/doc"})
	req := httptest.NewRequest(http.MethodPost, "/api/drop", bytes.NewReader(dropReqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleDrop(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for drop, got %d", w.Code)
	}

	select {
	case droppedItem := <-droppedCh:
		if droppedItem.Content != "https://hanxi.dev/doc" || !droppedItem.IsURL {
			t.Errorf("unexpected dropped item: %+v", droppedItem)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("drop hook was not called within timeout")
	}

	// 测试 2: 单次二进制流式上传 API (/api/upload)
	content := []byte("Hello Hanxi Streaming File Upload")
	query := fmt.Sprintf("dir=&name=test_stream.txt&size=%d", len(content))
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/upload?"+query, bytes.NewReader(content))
	uploadReq.Header.Set("Content-Type", "application/octet-stream")
	uploadRec := httptest.NewRecorder()
	server.handleUpload(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for upload, got %d (%s)", uploadRec.Code, uploadRec.Body.String())
	}

	uploadedFile := filepath.Join(tempDir, "test_stream.txt")
	uploadedContent, err := os.ReadFile(uploadedFile)
	if err != nil {
		t.Fatalf("uploaded file not found on disk: %v", err)
	}
	if string(uploadedContent) != "Hello Hanxi Streaming File Upload" {
		t.Errorf("uploaded content mismatch: %s", string(uploadedContent))
	}
}

func TestProgressTimeoutReaderRefreshesReadDeadline(t *testing.T) {
	deadlineSet := false
	writer := &deadlineResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		setReadDeadline: func(deadline time.Time) error {
			deadlineSet = deadline.After(time.Now())
			return nil
		},
	}
	reader := &progressTimeoutReader{
		reader:     strings.NewReader("chunk"),
		controller: http.NewResponseController(writer),
		timeout:    time.Second,
	}
	buf := make([]byte, 5)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !deadlineSet {
		t.Fatal("read deadline was not refreshed")
	}
	if n != 5 || string(buf) != "chunk" {
		t.Fatalf("unexpected read: n=%d data=%q", n, string(buf))
	}
}

type deadlineResponseWriter struct {
	*httptest.ResponseRecorder
	setReadDeadline func(time.Time) error
}

func (w *deadlineResponseWriter) SetReadDeadline(deadline time.Time) error {
	return w.setReadDeadline(deadline)
}

func TestFileshareCleanupExpiredUploadTemps(t *testing.T) {
	tempDir := t.TempDir()
	server := NewServer(ShareConfig{SharePath: tempDir}, nil, nil)
	oldTemp := filepath.Join(tempDir, ".hanxi-upload-old.tmp")
	newTemp := filepath.Join(tempDir, ".hanxi-upload-new.tmp")
	normal := filepath.Join(tempDir, "notes.tmp")
	for _, path := range []string{oldTemp, newTemp, normal} {
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	_ = os.Chtimes(oldTemp, now.Add(-25*time.Hour), now.Add(-25*time.Hour))
	server.cleanupExpiredUploadTemps(now)
	if _, err := os.Stat(oldTemp); !os.IsNotExist(err) {
		t.Fatalf("expired upload temp not removed: %v", err)
	}
	for _, path := range []string{newTemp, normal} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("valid file removed: %s: %v", path, err)
		}
	}
}
