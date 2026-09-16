package fileshare

// 传输引擎生命周期与骨架：Server 结构、启停、路由装配、连接中间件、静态资源与配置接口、
// 共享根路径收口（resolveSafePath/openRoot）、速率采样。
// 处理面按职责拆分到同包：auth.go（口令门禁）、upload.go（上传流）、browse.go（浏览/投递/统计）、
// util.go（响应与格式化工具）。

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hanxi/internal/modules/fileshare/web"
)

// 服务面固定时间参数（收口自裸值，改值须同步核对前端轮询与移动端超时表现）。
const (
	// httpReadHeaderTimeout 只约束"请求头到达"的速度，不影响慢速大文件正文传输。
	httpReadHeaderTimeout = 10 * time.Second
	// gracefulStopTimeout 优雅停机上限：超时后强制关闭，避免卡在半途的上传拖住模块卸载。
	gracefulStopTimeout = 3 * time.Second
	// rateSampleWindow 速率采样点保留窗口：仅窗口内的点参与差分计算。
	rateSampleWindow = 10 * time.Second
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
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       0, // 大文件上传无限制
		WriteTimeout:      0, // 大文件下载无限制
	}
	s.server = httpServer

	go func(server *http.Server, listener net.Listener) {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("局域网快传服务异常退出", "err", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), gracefulStopTimeout)
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
			slog.Warn("清理过期上传临时文件失败", "path", path, "err", err)
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
	// 仅保留最近 rateSampleWindow 内的采样点 (至少保留最后 2 个用于差分计算)
	cutoff := time.Now().Add(-rateSampleWindow)
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
