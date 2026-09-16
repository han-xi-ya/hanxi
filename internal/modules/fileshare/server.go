package fileshare

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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

	"hanxi/internal/modules/fileshare/web"
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

	// 口令会话 Cookie：HttpOnly + SameSite=Lax，登录一次有效期内免再输；
	// 跨站 POST 不带 Cookie，天然免疫 CSRF 借权。
	sessionCookieName   = "hanxi_share_session"
	sessionCookieMaxAge = 30 * 24 * time.Hour
)

// sessionCookieValue 由访问口令 HMAC 派生会话 Cookie 值：口令不落 Cookie，
// 换口令即令全部旧 Cookie 失效（无需服务端会话表，重启后旧登录态仍有效）。
func sessionCookieValue(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("hanxi-fileshare/session-v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

type uploadParams struct {
	dir  string
	name string
	size int64
}

// progressTimeoutReader 上传流包装器：每次 Read 前用 ResponseController 刷新读截止时间，
// 实现"停滞超时"而非总时长超时——慢但持续的传输不被掐断，卡死的连接按时断开。
type progressTimeoutReader struct {
	reader     io.Reader
	controller *http.ResponseController
	timeout    time.Duration
}

// Read 设置本轮读超时后透传底层读取；ErrNotSupported（如非 HTTP 流）容忍降级。
func (r *progressTimeoutReader) Read(p []byte) (int, error) {
	if err := r.controller.SetReadDeadline(time.Now().Add(r.timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, fmt.Errorf("设置上传停滞超时失败: %w", err)
	}
	return r.reader.Read(p)
}

// byteCountingReader 按块回调累计已读字节数，驱动 /api/stats 实时速率采样。
type byteCountingReader struct {
	reader io.Reader
	onRead func(int64)
}

// Read 透传底层读取；onRead 在 n>0 时以本次字节数回调（回调需自行保证并发安全）。
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
	statsDone chan struct{} // 等待速率采样协程退出

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

	handler, err := s.handler()
	if err != nil {
		listener.Close()
		s.listener = nil
		return 0, err
	}

	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0, // 大文件上传无限制
		WriteTimeout:      0, // 大文件下载无限制
	}
	s.server = httpServer

	go func(server *http.Server, listener net.Listener) {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("[fileshare] server serve error: %v\n", err)
		}
	}(httpServer, listener)

	// 启动速率采样并清理旧进程遗留的上传临时文件
	statsQuit := make(chan struct{})
	statsDone := make(chan struct{})
	s.statsQuit = statsQuit
	s.statsDone = statsDone
	go s.cleanupExpiredUploadTemps(time.Now())
	go s.samplingLoop(statsQuit, statsDone)

	return actualPort, nil
}

// handler 组装完整 HTTP 处理链（connTracker → authGate → mux），
// Start 与集成测试共用，避免测试平行维护一份路由表。
func (s *Server) handler() (http.Handler, error) {
	mux := http.NewServeMux()
	assetFS, err := fs.Sub(web.DistFS, "assets")
	if err != nil {
		return nil, fmt.Errorf("加载快传静态资源失败: %w", err)
	}
	mux.Handle("/assets/", s.handleAssets(http.StripPrefix("/assets/", http.FileServer(http.FS(assetFS)))))
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/list", s.handleList)
	mux.HandleFunc("/api/download", s.handleDownload)
	mux.HandleFunc("/api/open", s.handleOpen)
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/api/drop", s.handleDrop)
	mux.HandleFunc("/api/stats", s.handleStats)
	return s.connTracker(s.authGate(mux)), nil
}

// Stop 优雅关闭 HTTP 服务；超时后强制关闭，且不持状态锁等待 handler。
func (s *Server) Stop() error {
	s.mu.Lock()
	server := s.server
	statsQuit := s.statsQuit
	statsDone := s.statsDone
	if server == nil {
		s.mu.Unlock()
		return nil
	}
	s.server = nil
	s.listener = nil
	s.statsQuit = nil
	s.statsDone = nil
	s.mu.Unlock()

	if statsQuit != nil {
		close(statsQuit)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := server.Shutdown(ctx)
	cancel()
	if err != nil {
		closeErr := server.Close()
		if closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			err = errors.Join(err, closeErr)
		}
	}
	if statsDone != nil {
		<-statsDone
	}
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
	// 口令热更新：会话 Cookie 由口令 HMAC 派生，换口令即令旧登录态全部失效。
	s.config.AuthToken = cfg.AuthToken
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

// connTracker 连接中间件。
// 曾对全响应下发 Access-Control-Allow-Origin: *：访客页与本服务同源，
// 本不需要 CORS；通配反而允许任意网页借用户浏览器跨源打局域网端点，
// 已按 BUG-001 审查结论移除（BUG 编号见 docs/BUG_AUDIT.md）。
func (s *Server) connTracker(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 上传请求统一使用独立连接，避免 Safari/WKWebView 长连接状态异常。
		// 单次流只有一个长请求，不会产生反复建连开销。
		if r.URL.Path == "/api/upload" {
			r.Close = true
			w.Header().Set("Connection", "close")
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

// configSnapshot 返回当前配置副本，避免运行时热更新与 handler 无锁读发生竞争。
func (s *Server) configSnapshot() ShareConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// handleConfig 返回公共配置（authRequired 供访客页决定是否弹口令门禁；
// 只暴露三个开关与上限，不含口令与路径，未登录可读）。
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.configSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"allowUpload":     cfg.AllowUpload,
		"allowTextDrop":   cfg.AllowTextDrop,
		"maxUploadSizeMB": cfg.MaxUploadSizeMB,
		"authRequired":    cfg.AuthToken != "",
	})
}

// handleLogin 校验访问口令并签发会话 Cookie。
// 免密模式下明确拒绝（400），避免调用方误以为存在可绕过的登录态。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.configSnapshot()
	if cfg.AuthToken == "" {
		http.Error(w, "本共享未设置访问口令", http.StatusBadRequest)
		return
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload); err != nil {
		http.Error(w, "请求体不合法", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(payload.Token), []byte(cfg.AuthToken)) != 1 {
		// 失败路径统一延时，压缩局域网内口令爆破的尝试频率
		time.Sleep(400 * time.Millisecond)
		http.Error(w, "访问口令不正确", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionCookieValue(cfg.AuthToken),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionCookieMaxAge / time.Second),
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// authGate 口令门禁：AuthToken 为空保持免密语义（产品定位「免密局域网共享」）；
// 非空时页面与静态资源放行（登录界面自身需要加载），/api/login 与 /api/config
// 白名单放行，其余 /api/*（列表/下载/预览/上传/投递/统计）必须持有有效会话。
func (s *Server) authGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.configSnapshot().AuthToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		switch {
		case !strings.HasPrefix(r.URL.Path, "/api/"),
			r.URL.Path == "/api/login",
			r.URL.Path == "/api/config":
			next.ServeHTTP(w, r)
		case s.authenticated(r):
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "需要访问口令", http.StatusUnauthorized)
		}
	})
}

// authenticated 双通道校验：浏览器走会话 Cookie（HMAC 派生值，恒时比较），
// 非浏览器集成（脚本/快捷指令等）支持 Authorization: Bearer <口令>。
func (s *Server) authenticated(r *http.Request) bool {
	cfg := s.configSnapshot()
	want := sessionCookieValue(cfg.AuthToken)
	if cookie, err := r.Cookie(sessionCookieName); err == nil &&
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(want)) == 1 {
		return true
	}
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok &&
		subtle.ConstantTimeCompare([]byte(token), []byte(cfg.AuthToken)) == 1 {
		return true
	}
	return false
}

// resolveSafePath 严格解析为共享根内的相对路径；真实访问必须继续经 os.Root。
func (s *Server) resolveSafePath(subPath string) (string, error) {
	if filepath.IsAbs(subPath) || strings.HasPrefix(subPath, "/") || strings.HasPrefix(subPath, "\\") || filepath.VolumeName(subPath) != "" {
		return "", errors.New("禁止访问非法越界路径 (Absolute Path Forbidden)")
	}
	cleanRel := filepath.Clean(filepath.FromSlash(subPath))
	if cleanRel == "." || cleanRel == "" {
		return ".", nil
	}
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", errors.New("禁止访问非法越界路径 (Path Traversal Forbidden)")
	}
	return cleanRel, nil
}

func (s *Server) openRoot() (*os.Root, error) {
	s.mu.RLock()
	sharePath := s.config.SharePath
	s.mu.RUnlock()
	if sharePath == "" {
		return nil, errors.New("共享路径不能为空")
	}
	return os.OpenRoot(sharePath)
}

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

// handleUpload 以单个二进制请求流式接收文件。
// 请求体不会整体进入内存；先写入隐藏临时文件，完整校验后再原子发布。
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.configSnapshot()
	if !cfg.AllowUpload {
		http.Error(w, "服务器未开启文件上传权限", http.StatusForbidden)
		return
	}

	p, err := s.parseUploadParams(r, cfg.MaxUploadSizeMB)
	if err != nil {
		uploadParamError(w, err)
		return
	}
	if r.ContentLength >= 0 && r.ContentLength != p.size {
		http.Error(w, "请求体大小与文件声明不一致", http.StatusBadRequest)
		return
	}

	dirRel, err := s.resolveSafePath(p.dir)
	if err != nil {
		uploadParamError(w, err)
		return
	}
	root, err := s.openRoot()
	if err != nil {
		http.Error(w, "无法打开共享目录: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer root.Close()
	targetRoot, err := root.OpenRoot(dirRel)
	if err != nil {
		http.Error(w, "上传目录不存在或不可读", http.StatusBadRequest)
		return
	}
	defer targetRoot.Close()

	filename := filepath.Base(p.name)
	tempName, temp, err := createUploadTemp(targetRoot)
	if err != nil {
		http.Error(w, "无法创建上传临时文件: "+err.Error(), http.StatusInternalServerError)
		return
	}
	published := false
	defer func() {
		_ = temp.Close()
		if !published {
			_ = targetRoot.Remove(tempName)
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
	finalName, pathErr := getNonConflictingName(targetRoot, filename)
	var renameErr error
	if pathErr == nil {
		renameErr = targetRoot.Rename(tempName, finalName)
	}
	s.publishMu.Unlock()
	if pathErr != nil || renameErr != nil {
		if pathErr != nil {
			renameErr = pathErr
		}
		http.Error(w, "发布上传文件失败: "+renameErr.Error(), http.StatusInternalServerError)
		return
	}
	published = true

	atomic.AddInt64(&s.uploadCount, 1)
	s.logEvent(TransferEvent{
		Type: "upload", Filename: finalName, Size: written, ClientIP: getClientIP(r), Timestamp: time.Now(), Success: true,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"name":    finalName,
		"size":    written,
	})
}

func isUploadTempName(name string) bool {
	return strings.HasPrefix(name, ".hanxi-upload-") && strings.HasSuffix(name, ".tmp")
}

func parsePositiveInt64(value, field string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s 参数不合法", field)
	}
	return n, nil
}

func (s *Server) parseUploadParams(r *http.Request, maxUploadSizeMB int64) (uploadParams, error) {
	q := r.URL.Query()
	p := uploadParams{dir: q.Get("dir"), name: q.Get("name")}
	if p.name == "" || p.name == "." || p.name == ".." {
		return p, errors.New("name 参数不合法")
	}
	var err error
	if p.size, err = parsePositiveInt64(q.Get("size"), "size"); err != nil {
		return p, err
	}
	if maxMB := maxUploadSizeMB; maxMB > 0 && (maxMB > (1<<63-1)/(1024*1024) || p.size > maxMB*1024*1024) {
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
func (s *Server) samplingLoop(quit <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.sampleRatePoint()
		case <-quit:
			return
		}
	}
}

func (s *Server) cleanupExpiredUploadTemps(now time.Time) {
	root, err := s.openRoot()
	if err != nil {
		return
	}
	defer root.Close()
	cutoff := now.Add(-uploadTempTTL)
	_ = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !isUploadTempName(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := root.Remove(filepath.FromSlash(path)); err != nil && !os.IsNotExist(err) {
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

// Write 透传响应写入并累计字节数；无 Flush/Hijack 等可选接口需求，故不转发。
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

func createUploadTemp(root *os.Root) (string, *os.File, error) {
	for range 100 {
		var token [8]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", nil, err
		}
		name := ".hanxi-upload-" + hex.EncodeToString(token[:]) + ".tmp"
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			return name, file, nil
		}
		if !os.IsExist(err) {
			return "", nil, err
		}
	}
	return "", nil, errors.New("无法分配上传临时文件名")
}

func getNonConflictingName(root *os.Root, target string) (string, error) {
	if _, err := root.Lstat(target); os.IsNotExist(err) {
		return target, nil
	} else if err != nil {
		return "", err
	}
	ext := filepath.Ext(target)
	base := strings.TrimSuffix(target, ext)
	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := root.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("同名文件过多，无法分配目标名称")
}
