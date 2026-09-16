package fileshare

// 上传通道：单个二进制请求流式落盘（请求体不整体进内存），先写隐藏临时文件，
// 完整校验后再原子发布；同名冲突由 getNonConflictingName 让路，绝不覆盖既有文件。

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	uploadTempTTL           = 24 * time.Hour
	streamUploadIdleTimeout = 2 * time.Minute
	streamUploadBufferSize  = 1024 * 1024

	// uploadTempNameAttempts 上传临时文件名的随机重试次数上限（O_EXCL 撞名极少，留足余量）。
	uploadTempNameAttempts = 100
)

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

func createUploadTemp(root *os.Root) (string, *os.File, error) {
	for range uploadTempNameAttempts {
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
