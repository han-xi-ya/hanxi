package fileshare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

const (
	uploadTempTTL           = 24 * time.Hour
	streamUploadIdleTimeout = 2 * time.Minute
	streamUploadBufferSize  = 1024 * 1024
)

type uploadParams struct {
	dir  string
	name string
	size int64
}

type progressTimeoutReader struct {
	reader     io.Reader
	controller *http.ResponseController
	timeout    time.Duration
}

func (r *progressTimeoutReader) Read(p []byte) (int, error) {
	if err := r.controller.SetReadDeadline(time.Now().Add(r.timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, fmt.Errorf("设置上传停滞超时失败: %w", err)
	}
	return r.reader.Read(p)
}

type byteCountingReader struct {
	reader io.Reader
	onRead func(int64)
}

func (r *byteCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && r.onRead != nil {
		r.onRead(int64(n))
	}
	return n, err
}

// Server 局域网 HTTP 文件与文本传输引擎
type Server struct {
	config    ShareConfig
	listener  net.Listener
	server    *http.Server
	startedAt time.Time
	statsQuit chan struct{} // 关闭速率采样协程

	mu        sync.RWMutex
	publishMu sync.Mutex

	activeConnections int64
	uploadCount       int64
	downloadCount     int64
	upBytes           int64 // 累计上传字节
	downBytes         int64 // 累计下载字节
	ratePoints        []ratePoint
	dropInbox         []DropItem

	onDropHook     func(item DropItem)
	onTransferHook func(event TransferEvent)
}

// NewServer 创建文件共享服务器实例
func NewServer(cfg ShareConfig, onDrop func(DropItem), onTransfer func(TransferEvent)) *Server {
	return &Server{
		config:         cfg,
		dropInbox:      make([]DropItem, 0),
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
	assetFS, err := fs.Sub(web.DistFS, "assets")
	if err != nil {
		listener.Close()
		s.listener = nil
		return 0, fmt.Errorf("加载快传静态资源失败: %w", err)
	}
	mux.Handle("/assets/", s.handleAssets(http.StripPrefix("/assets/", http.FileServer(http.FS(assetFS)))))
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/list", s.handleList)
	mux.HandleFunc("/api/download", s.handleDownload)
	mux.HandleFunc("/api/open", s.handleOpen)
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/api/drop", s.handleDrop)
	mux.HandleFunc("/api/stats", s.handleStats)

	s.server = &http.Server{
		Handler:           s.connTracker(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0, // 大文件上传无限制
		WriteTimeout:      0, // 大文件下载无限制
	}

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("[fileshare] server serve error: %v\n", err)
		}
	}()

	// 启动速率采样并清理旧进程遗留的上传临时文件
	s.statsQuit = make(chan struct{})
	go s.cleanupExpiredUploadTemps(time.Now())
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
		// 跨域支持 (用于局域网不同端访问)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// 上传请求统一使用独立连接，避免 Safari/WKWebView 长连接状态异常。
		// 单次流只有一个长请求，不会产生反复建连开销。
		if r.URL.Path == "/api/upload" {
			r.Close = true
			w.Header().Set("Connection", "close")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// 轮询性请求不计入活跃连接数，避免统计面板自身虚高
		if r.URL.Path == "/api/stats" {
			next.ServeHTTP(w, r)
			return
		}
		atomic.AddInt64(&s.activeConnections, 1)
		defer atomic.AddInt64(&s.activeConnections, -1)

		next.ServeHTTP(w, r)
	})
}

// handleAssets 提供嵌入式 CSS 与 JavaScript 静态资源
func (s *Server) handleAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "" || r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
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
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
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

// handleUpload 以单个二进制请求流式接收文件。
// 请求体不会整体进入内存；先写入隐藏临时文件，完整校验后再原子发布。
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
		return
	}
	if !s.config.AllowUpload {
		http.Error(w, "服务器未开启文件上传权限", http.StatusForbidden)
		return
	}

	p, err := s.parseUploadParams(r)
	if err != nil {
		uploadParamError(w, err)
		return
	}
	if r.ContentLength >= 0 && r.ContentLength != p.size {
		http.Error(w, "请求体大小与文件声明不一致", http.StatusBadRequest)
		return
	}

	targetDir, err := s.resolveSafePath(p.dir)
	if err != nil {
		uploadParamError(w, err)
		return
	}
	info, err := os.Stat(targetDir)
	if err != nil || !info.IsDir() {
		http.Error(w, "上传目录不存在或不可读", http.StatusBadRequest)
		return
	}

	filename := filepath.Base(p.name)

	temp, err := os.CreateTemp(targetDir, ".hubkit-upload-*.tmp")
	if err != nil {
		http.Error(w, "无法创建上传临时文件: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tempPath := temp.Name()
	published := false
	defer func() {
		_ = temp.Close()
		if !published {
			_ = os.Remove(tempPath)
		}
	}()

	controller := http.NewResponseController(w)
	limited := http.MaxBytesReader(w, r.Body, p.size)
	idleReader := &progressTimeoutReader{
		reader:     limited,
		controller: controller,
		timeout:    streamUploadIdleTimeout,
	}
	reader := &byteCountingReader{
		reader: idleReader,
		onRead: func(n int64) {
			s.recordBytes("up", n)
		},
	}
	buf := make([]byte, streamUploadBufferSize)
	written, copyErr := io.CopyBuffer(temp, reader, buf)
	if err := controller.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		copyErr = errors.Join(copyErr, fmt.Errorf("清除上传停滞超时失败: %w", err))
	}
	closeErr := temp.Close()

	if copyErr != nil || closeErr != nil || written != p.size {
		if copyErr == nil {
			copyErr = closeErr
		}
		if copyErr == nil {
			copyErr = fmt.Errorf("实际接收 %d 字节，声明 %d 字节", written, p.size)
		}
		s.logEvent(TransferEvent{
			Type:      "upload",
			Filename:  filename,
			Size:      written,
			ClientIP:  getClientIP(r),
			Timestamp: time.Now(),
			Success:   false,
			ErrorMsg:  copyErr.Error(),
		})
		http.Error(w, "上传写入中断: "+copyErr.Error(), http.StatusBadRequest)
		return
	}

	s.publishMu.Lock()
	finalPath := getNonConflictingPath(filepath.Join(targetDir, filename))
	renameErr := os.Rename(tempPath, finalPath)
	s.publishMu.Unlock()
	if renameErr != nil {
		http.Error(w, "发布上传文件失败: "+renameErr.Error(), http.StatusInternalServerError)
		return
	}
	published = true

	atomic.AddInt64(&s.uploadCount, 1)
	s.logEvent(TransferEvent{
		Type:      "upload",
		Filename:  filepath.Base(finalPath),
		Size:      written,
		ClientIP:  getClientIP(r),
		Timestamp: time.Now(),
		Success:   true,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"name":    filepath.Base(finalPath),
		"size":    written,
	})
}

func isUploadTempName(name string) bool {
	return strings.HasPrefix(name, ".hubkit-upload-") && strings.HasSuffix(name, ".tmp")
}

func parsePositiveInt64(value, field string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s 参数不合法", field)
	}
	return n, nil
}

func (s *Server) parseUploadParams(r *http.Request) (uploadParams, error) {
	q := r.URL.Query()
	p := uploadParams{dir: q.Get("dir"), name: q.Get("name")}
	if p.name == "" || p.name == "." || p.name == ".." {
		return p, errors.New("name 参数不合法")
	}
	var err error
	if p.size, err = parsePositiveInt64(q.Get("size"), "size"); err != nil {
		return p, err
	}
	if maxMB := s.config.MaxUploadSizeMB; maxMB > 0 && (maxMB > (1<<63-1)/(1024*1024) || p.size > maxMB*1024*1024) {
		return p, fmt.Errorf("文件超过 %d MB 上传限制", maxMB)
	}
	return p, nil
}

func uploadParamError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if strings.Contains(err.Error(), "上传限制") {
		status = http.StatusRequestEntityTooLarge
	}
	http.Error(w, err.Error(), status)
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

// logEvent 转发传输审计事件
func (s *Server) logEvent(event TransferEvent) {
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

func (s *Server) cleanupExpiredUploadTemps(now time.Time) {
	s.mu.RLock()
	root := s.config.SharePath
	s.mu.RUnlock()
	if root == "" {
		return
	}
	cutoff := now.Add(-uploadTempTTL)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !isUploadTempName(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Printf("[fileshare] cleanup upload temp %s failed: %v\n", path, err)
		}
		return nil
	})
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
