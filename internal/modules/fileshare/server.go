package fileshare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hubkit/internal/modules/fileshare/web"
)

// ratePoint 速率采样点 (保存某个时刻的累计传输字节数)
type ratePoint struct {
	at   time.Time
	up   int64 // 该时刻累计上传字节
	down int64 // 该时刻累计下载字节
}

// Server 局域网 HTTP 文件与文本传输引擎
type Server struct {
	config    ShareConfig
	listener  net.Listener
	server    *http.Server
	startedAt time.Time
	statsQuit chan struct{} // 关闭速率采样协程

	mu                sync.RWMutex
	activeConnections int64
	uploadCount       int64
	downloadCount     int64
	upBytes           int64 // 累计上传字节
	downBytes         int64 // 累计下载字节
	ratePoints        []ratePoint
	dropInbox         []DropItem
	events            []TransferEvent

	onDropHook     func(item DropItem)
	onTransferHook func(event TransferEvent)
}

// NewServer 创建文件共享服务器实例
func NewServer(cfg ShareConfig, onDrop func(DropItem), onTransfer func(TransferEvent)) *Server {
	return &Server{
		config:         cfg,
		dropInbox:      make([]DropItem, 0),
		events:         make([]TransferEvent, 0),
		onDropHook:     onDrop,
		onTransferHook: onTransfer,
	}
}

// Start 启动 HTTP 服务
func (s *Server) Start() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return 0, errors.New("服务已在运行中")
	}

	// 校验共享目录物理存在性
	if s.config.SharePath == "" {
		return 0, errors.New("共享路径不能为空")
	}
	info, err := os.Stat(s.config.SharePath)
	if err != nil || !info.IsDir() {
		return 0, fmt.Errorf("共享路径无效或不是目录: %s", s.config.SharePath)
	}

	addr := fmt.Sprintf("0.0.0.0:%d", s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("监听端口失败: %w", err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	s.config.Port = actualPort
	s.listener = listener
	s.startedAt = time.Now()

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/list", s.handleList)
	mux.HandleFunc("/api/download", s.handleDownload)
	mux.HandleFunc("/api/open", s.handleOpen)
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/api/upload/status", s.handleUploadStatus)
	mux.HandleFunc("/api/upload/append", s.handleUploadAppend)
	mux.HandleFunc("/api/upload/complete", s.handleUploadComplete)
	mux.HandleFunc("/api/upload/abort", s.handleUploadAbort)
	mux.HandleFunc("/api/drop", s.handleDrop)
	mux.HandleFunc("/api/stats", s.handleStats)

	s.server = &http.Server{
		Handler:      s.connTracker(mux),
		ReadTimeout:  0, // 大文件上传无限制
		WriteTimeout: 0, // 大文件下载无限制
	}

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("[fileshare] server serve error: %v\n", err)
		}
	}()

	// 启动每秒速率采样协程
	s.statsQuit = make(chan struct{})
	go s.samplingLoop()

	return actualPort, nil
}

// Stop 优雅关闭 HTTP 服务
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.server.Shutdown(ctx)

	// 停止速率采样协程 (close 后协程在下一次 select 立即退出)
	if s.statsQuit != nil {
		close(s.statsQuit)
		s.statsQuit = nil
	}

	s.server = nil
	s.listener = nil
	return err
}

// UpdateConfig 运行时动态更新配置
func (s *Server) UpdateConfig(cfg ShareConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.AllowUpload = cfg.AllowUpload
	s.config.AllowTextDrop = cfg.AllowTextDrop
	s.config.MaxUploadSizeMB = cfg.MaxUploadSizeMB
	s.config.AutoSaveToMemo = cfg.AutoSaveToMemo
	if cfg.SharePath != "" {
		s.config.SharePath = cfg.SharePath
	}
}

// IsRunning 判断是否正在运行
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.server != nil
}

// connTracker 连接中间件
func (s *Server) connTracker(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 轮询性请求不计入活跃连接数，避免统计面板自身虚高
		if r.URL.Path == "/api/stats" {
			next.ServeHTTP(w, r)
			return
		}
		atomic.AddInt64(&s.activeConnections, 1)
		defer atomic.AddInt64(&s.activeConnections, -1)

		// 跨域支持 (用于局域网不同端访问)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleIndex 提供嵌入式 Web 前端
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	content, err := web.DistFS.ReadFile("index.html")
	if err != nil {
		http.Error(w, "Web assets not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// handleConfig 返回公共配置
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"allowUpload":     s.config.AllowUpload,
		"allowTextDrop":   s.config.AllowTextDrop,
		"maxUploadSizeMB": s.config.MaxUploadSizeMB,
	})
}

// resolveSafePath 严格防目录穿越沙箱解析
func (s *Server) resolveSafePath(subPath string) (string, error) {
	// 如果传入绝对路径，或者包含驱动器卷标/根路径前缀，直接拒绝
	if filepath.IsAbs(subPath) || strings.HasPrefix(subPath, "/") || strings.HasPrefix(subPath, "\\") || filepath.VolumeName(subPath) != "" {
		return "", errors.New("禁止访问非法越界路径 (Absolute Path Forbidden)")
	}

	cleanRel := filepath.Clean(filepath.FromSlash(subPath))
	if cleanRel == "." || cleanRel == "" {
		return s.config.SharePath, nil
	}

	// 拼接绝对路径
	target := filepath.Join(s.config.SharePath, cleanRel)
	// 验证最终目标路径是否包含在 SharePath 之内
	rel, err := filepath.Rel(s.config.SharePath, target)
	if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, "\\") || strings.HasPrefix(rel, "/") {
		return "", errors.New("禁止访问非法越界路径 (Path Traversal Forbidden)")
	}

	return target, nil
}

// handleList 列出指定目录下的文件与子目录
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	fullPath, err := s.resolveSafePath(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, "无法读取目录: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		// 隐藏断点续传的临时分片文件 (.part.<size>.<mod>)
		if strings.Contains(e.Name(), ".part.") {
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
	fullPath, err := s.resolveSafePath(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	s.serveFile(w, r, fullPath, true)
}

// handleOpen 内联打开文件 (不设置 attachment 头，浏览器直接预览图片/视频/PDF 等)
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	fullPath, err := s.resolveSafePath(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	s.serveFile(w, r, fullPath, false)
}

// serveFile 统一的文件下发逻辑 (attach=true 强制下载，false 浏览器内联预览)
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, fullPath string, attach bool) {
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.Error(w, "文件不存在或为目录", http.StatusNotFound)
		return
	}

	atomic.AddInt64(&s.downloadCount, 1)

	s.logEvent(TransferEvent{
		Type:      "download",
		Filename:  info.Name(),
		Size:      info.Size(),
		ClientIP:  getClientIP(r),
		Timestamp: time.Now(),
		Success:   true,
	})

	if attach {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(fullPath)))
	}

	// 包装 ResponseWriter 统计实际下发的字节数 (兼容 Range 断点续传)
	cw := &countingResponseWriter{ResponseWriter: w}
	http.ServeFile(cw, r, fullPath)
	s.recordBytes("down", atomic.LoadInt64(&cw.n))
}

// handleUpload 流式接收多文件上传
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !s.config.AllowUpload {
		http.Error(w, "服务器未开启文件上传权限", http.StatusForbidden)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "无效的表单流: "+err.Error(), http.StatusBadRequest)
		return
	}

	targetDir := s.config.SharePath

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "读取上传流异常: "+err.Error(), http.StatusBadRequest)
			return
		}

		if part.FormName() == "path" {
			subPathBytes, _ := io.ReadAll(part)
			resolved, err := s.resolveSafePath(string(subPathBytes))
			if err == nil {
				targetDir = resolved
			}
			part.Close()
			continue
		}

		if part.FormName() == "file" && part.FileName() != "" {
			filename := filepath.Base(part.FileName())
			savePath := filepath.Join(targetDir, filename)

			// 防重名覆盖策略: 若存在同名文件，添加 (1), (2) 后缀
			savePath = getNonConflictingPath(savePath)

			dst, err := os.OpenFile(savePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				part.Close()
				http.Error(w, "无法写入磁盘: "+err.Error(), http.StatusInternalServerError)
				return
			}

			// 流式零拷贝写入，内存恒定
			written, copyErr := io.Copy(dst, part)
			dst.Close()
			part.Close()

			if copyErr != nil {
				s.logEvent(TransferEvent{
					Type:      "upload",
					Filename:  filename,
					Size:      written,
					ClientIP:  getClientIP(r),
					Timestamp: time.Now(),
					Success:   false,
					ErrorMsg:  copyErr.Error(),
				})
				http.Error(w, "上传写入中断: "+copyErr.Error(), http.StatusInternalServerError)
				return
			}

			atomic.AddInt64(&s.uploadCount, 1)
			s.recordBytes("up", written)
			s.logEvent(TransferEvent{
				Type:      "upload",
				Filename:  filename,
				Size:      written,
				ClientIP:  getClientIP(r),
				Timestamp: time.Now(),
				Success:   true,
			})
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Upload success"))
}

// uploadPartName 依据文件指纹构造断点续传临时片文件名
func uploadPartName(name string, size, mod int64) string {
	return fmt.Sprintf("%s.part.%d.%d", name, size, mod)
}

// uploadPartPath 解析分片相对路径并做防穿越校验
func (s *Server) uploadPartPath(dir, name string, size, mod int64) (string, error) {
	if name == "" || name == "." || name == ".." || size <= 0 || mod <= 0 {
		return "", errors.New("上传参数不合法")
	}
	partName := uploadPartName(filepath.Base(name), size, mod)
	return s.resolveSafePath(filepath.ToSlash(filepath.Join(dir, partName)))
}

// handleUploadStatus 查询目标文件的断点续传进度
func (s *Server) handleUploadStatus(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	name := r.URL.Query().Get("name")
	size, _ := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	mod, _ := strconv.ParseInt(r.URL.Query().Get("mod"), 10, 64)

	full, err := s.uploadPartPath(dir, name, size, mod)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result := map[string]any{"exists": false, "uploaded": 0}
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		result = map[string]any{"exists": true, "uploaded": info.Size()}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleUploadAppend 追加一个二进制分片到临时片文件 (支持断点续传)
func (s *Server) handleUploadAppend(w http.ResponseWriter, r *http.Request) {
	if !s.config.AllowUpload {
		http.Error(w, "服务器未开启文件上传权限", http.StatusForbidden)
		return
	}

	dir := r.URL.Query().Get("dir")
	name := r.URL.Query().Get("name")
	size, _ := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	mod, _ := strconv.ParseInt(r.URL.Query().Get("mod"), 10, 64)
	// 客户端当前续传起点: 必须与片文件实际长度严格对齐, 防止超时重试导致重复写入损坏文件
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)

	full, err := s.uploadPartPath(dir, name, size, mod)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 偏移量校验: 不匹配说明有残留分片滞留 (中断/超时写了一半), 返回实际长度让前端清片重传
	if info, statErr := os.Stat(full); statErr == nil && !info.IsDir() {
		if info.Size() != offset {
			writeJSON(w, http.StatusConflict, map[string]any{"uploaded": info.Size(), "reset": true})
			return
		}
	} else if offset > 0 {
		// 片文件已不存在 (如被 abort), 从零开始
		writeJSON(w, http.StatusConflict, map[string]any{"uploaded": 0, "reset": true})
		return
	}

	f, err := os.OpenFile(full, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		http.Error(w, "无法写入磁盘: "+err.Error(), http.StatusInternalServerError)
		return
	}
	n, copyErr := io.Copy(f, r.Body)
	f.Close()
	if copyErr != nil {
		http.Error(w, "写入中断: "+copyErr.Error(), http.StatusInternalServerError)
		return
	}
	s.recordBytes("up", n)

	// 返回片文件当前累计大小，前端据此跳过分片
	total := n
	if info, err := os.Stat(full); err == nil {
		total = info.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": total})
}

// handleUploadAbort 取消上传: 删除已追加的临时片文件 (幂等，无片文件也返回成功)
func (s *Server) handleUploadAbort(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	name := r.URL.Query().Get("name")
	size, _ := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	mod, _ := strconv.ParseInt(r.URL.Query().Get("mod"), 10, 64)

	full, err := s.uploadPartPath(dir, name, size, mod)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		http.Error(w, "清理片文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// handleUploadComplete 将临时片文件合并为正式文件并计入统计
func (s *Server) handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	if !s.config.AllowUpload {
		http.Error(w, "服务器未开启文件上传权限", http.StatusForbidden)
		return
	}

	dir := r.URL.Query().Get("dir")
	name := r.URL.Query().Get("name")
	size, _ := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	mod, _ := strconv.ParseInt(r.URL.Query().Get("mod"), 10, 64)

	full, err := s.uploadPartPath(dir, name, size, mod)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	partInfo, err := os.Stat(full)
	if err != nil {
		http.Error(w, "片文件不存在，无法完成: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 合并目标：与片文件同目录，正式文件名 (存在同名则自动加后缀)
	finalPath := getNonConflictingPath(filepath.Join(filepath.Dir(full), filepath.Base(name)))
	if err := os.Rename(full, finalPath); err != nil {
		http.Error(w, "合并文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	atomic.AddInt64(&s.uploadCount, 1)
	s.logEvent(TransferEvent{
		Type:      "upload",
		Filename:  filepath.Base(finalPath),
		Size:      partInfo.Size(),
		ClientIP:  getClientIP(r),
		Timestamp: time.Now(),
		Success:   true,
	})

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "name": filepath.Base(finalPath)})
}

// writeJSON 显式 Content-Length 写出 JSON 响应
// (避免隐式 chunked 流式响应在部分 WebView/移动浏览器环境挂起)
func writeJSON(w http.ResponseWriter, status int, data any) {
	body, _ := json.Marshal(data)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// handleDrop 处理移动端投递文本/URL
func (s *Server) handleDrop(w http.ResponseWriter, r *http.Request) {
	if !s.config.AllowTextDrop {
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
	// 最多保留最新 100 条收件箱
	s.dropInbox = append([]DropItem{item}, s.dropInbox...)
	if len(s.dropInbox) > 100 {
		s.dropInbox = s.dropInbox[:100]
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

// logEvent 记录审计日志
func (s *Server) logEvent(event TransferEvent) {
	s.mu.Lock()
	s.events = append([]TransferEvent{event}, s.events...)
	if len(s.events) > 50 {
		s.events = s.events[:50]
	}
	s.mu.Unlock()

	if s.onTransferHook != nil {
		s.onTransferHook(event)
	}
}

// recordBytes 累计传输字节数 (速率由每秒采样任务依据累计值差分得出)
func (s *Server) recordBytes(dir string, n int64) {
	if n <= 0 {
		return
	}
	if dir == "up" {
		atomic.AddInt64(&s.upBytes, n)
	} else {
		atomic.AddInt64(&s.downBytes, n)
	}
}

// samplingLoop 每秒记录一次累计字节采样点，供实时速率差分计算
func (s *Server) samplingLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.sampleRatePoint()
		case <-s.statsQuit:
			return
		}
	}
}

func (s *Server) sampleRatePoint() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ratePoints = append(s.ratePoints, ratePoint{
		at:   time.Now(),
		up:   atomic.LoadInt64(&s.upBytes),
		down: atomic.LoadInt64(&s.downBytes),
	})
	// 仅保留最近 10 秒内的采样点 (至少保留最后 2 个用于差分计算)
	cutoff := time.Now().Add(-10 * time.Second)
	trim := 0
	for trim < len(s.ratePoints)-2 && s.ratePoints[trim].at.Before(cutoff) {
		trim++
	}
	if trim > 0 {
		s.ratePoints = s.ratePoints[trim:]
	}
}

// currentRates 基于采样点差分计算当前上传/下载速率 (B/s)
func (s *Server) currentRates() (upRate, downRate float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := len(s.ratePoints)
	if n < 2 {
		return 0, 0
	}
	first := s.ratePoints[0]
	last := s.ratePoints[n-1]
	dt := last.at.Sub(first.at).Seconds()
	if dt <= 0 {
		return 0, 0
	}
	upRate = float64(last.up-first.up) / dt
	downRate = float64(last.down-first.down) / dt
	if upRate < 0 {
		upRate = 0
	}
	if downRate < 0 {
		downRate = 0
	}
	return upRate, downRate
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

// countingResponseWriter 包装 ResponseWriter 以统计实际写入客户端的字节数
type countingResponseWriter struct {
	http.ResponseWriter
	n int64
}

func (w *countingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func getNonConflictingPath(target string) string {
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return target
	}
	dir := filepath.Dir(target)
	ext := filepath.Ext(target)
	base := strings.TrimSuffix(filepath.Base(target), ext)

	for i := 1; i < 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return target
}
