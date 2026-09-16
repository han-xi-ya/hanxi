package fileshare

// 浏览与取回通道：目录列表、强制下载/内联预览（共用 serveFile，原生支持 Range 续传）、
// 移动端文本/链接投递与实时统计接口。全部路径经 resolveSafePath + os.Root 双重收口。

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// dropInboxLimit 投递箱最多保留的最新条数（超出丢最旧，防无界增长）。
const dropInboxLimit = 100

// handleList 列出指定目录下的文件与子目录
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	relPath, err := s.resolveSafePath(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	root, err := s.openRoot()
	if err != nil {
		http.Error(w, "无法打开共享目录: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), filepath.ToSlash(relPath))
	if err != nil {
		http.Error(w, "无法读取目录: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		// 隐藏正在接收的单次流上传临时文件
		if isUploadTempName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}

		relPath := filepath.ToSlash(filepath.Join(reqPath, e.Name()))
		size := info.Size()
		if e.IsDir() {
			size = 0
		}

		result = append(result, FileEntry{
			Name:      e.Name(),
			Path:      relPath,
			Size:      size,
			SizeHuman: formatBytes(size),
			IsDir:     e.IsDir(),
			ModTime:   info.ModTime(),
			Ext:       strings.ToLower(filepath.Ext(e.Name())),
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// handleDownload 处理文件强制下载 (原生支持 HTTP Range 断点续传)
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	relPath, err := s.resolveSafePath(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	s.serveFile(w, r, relPath, true)
}

// handleOpen 内联打开文件 (不设置 attachment 头，浏览器直接预览图片/视频/PDF 等)
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	relPath, err := s.resolveSafePath(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	s.serveFile(w, r, relPath, false)
}

// serveFile 统一的文件下发逻辑 (attach=true 强制下载，false 浏览器内联预览)
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, relPath string, attach bool) {
	root, err := s.openRoot()
	if err != nil {
		http.Error(w, "无法打开共享目录", http.StatusInternalServerError)
		return
	}
	defer root.Close()
	file, err := root.Open(relPath)
	if err != nil {
		http.Error(w, "文件不存在或不可访问", http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "文件不存在或为目录", http.StatusNotFound)
		return
	}

	atomic.AddInt64(&s.downloadCount, 1)
	s.logEvent(TransferEvent{
		Type: "download", Filename: info.Name(), Size: info.Size(), ClientIP: getClientIP(r), Timestamp: time.Now(), Success: true,
	})
	if attach {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, info.Name()))
	}
	cw := &countingResponseWriter{ResponseWriter: w}
	http.ServeContent(cw, r, info.Name(), info.ModTime(), file)
	s.recordBytes("down", atomic.LoadInt64(&cw.n))
}

// handleDrop 处理移动端投递文本/URL
func (s *Server) handleDrop(w http.ResponseWriter, r *http.Request) {
	cfg := s.configSnapshot()
	if !cfg.AllowTextDrop {
		http.Error(w, "服务器未开启文本投递功能", http.StatusForbidden)
		return
	}

	var payload struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || strings.TrimSpace(payload.Content) == "" {
		http.Error(w, "投递内容不能为空", http.StatusBadRequest)
		return
	}

	content := strings.TrimSpace(payload.Content)
	isURL := strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://")

	item := DropItem{
		ID:        fmt.Sprintf("drop_%d", time.Now().UnixNano()),
		Content:   content,
		SenderIP:  getClientIP(r),
		UserAgent: r.UserAgent(),
		CreatedAt: time.Now(),
		IsURL:     isURL,
	}

	s.mu.Lock()
	// 最多保留最新 dropInboxLimit 条收件箱
	s.dropInbox = append([]DropItem{item}, s.dropInbox...)
	if len(s.dropInbox) > dropInboxLimit {
		s.dropInbox = s.dropInbox[:dropInboxLimit]
	}
	s.mu.Unlock()

	s.logEvent(TransferEvent{
		Type:      "drop",
		Filename:  content,
		Size:      int64(len(content)),
		ClientIP:  item.SenderIP,
		Timestamp: item.CreatedAt,
		Success:   true,
	})

	// 先完整写出成功响应 (显式 Content-Length，避免移动端浏览器对 chunked
	// 响应断开过早而误报网络异常)，再异步触发联动回调，防止回调阻塞或
	// 异常导致响应无法送达
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": item.ID})

	if s.onDropHook != nil {
		itemCopy := item
		go func() {
			defer func() { _ = recover() }()
			s.onDropHook(itemCopy)
		}()
	}
}

// logEvent 转发传输审计事件
func (s *Server) logEvent(event TransferEvent) {
	if s.onTransferHook != nil {
		s.onTransferHook(event)
	}
}

// handleStats 返回实时传输统计 (供 Web 端轮询展示)
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	upRate, downRate := s.currentRates()
	writeJSON(w, http.StatusOK, map[string]any{
		"activeConnections": atomic.LoadInt64(&s.activeConnections),
		"uploadCount":       atomic.LoadInt64(&s.uploadCount),
		"downloadCount":     atomic.LoadInt64(&s.downloadCount),
		"uploadBytes":       atomic.LoadInt64(&s.upBytes),
		"downloadBytes":     atomic.LoadInt64(&s.downBytes),
		"uploadRate":        upRate,
		"downloadRate":      downRate,
	})
}
