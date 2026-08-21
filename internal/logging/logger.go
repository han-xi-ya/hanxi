package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var (
	// 脱敏正则：匹配 token、secret、password、authorization 等
	reAssignment = regexp.MustCompile(`(?i)(token|secret|password|passwd|sk|auth|authorization)\s*[:=]\s*["']?([^"'\s,]+)["']?`)
	reBearer     = regexp.MustCompile(`(?i)(bearer\s+)([a-zA-Z0-9_\-\.]{10,})`)
)

// Redact 对文本进行敏感信息打码
func Redact(text string) string {
	res := reAssignment.ReplaceAllString(text, `$1="******"`)
	res = reBearer.ReplaceAllString(res, `$1******`)
	return res
}

// RedactHandler slog 的自定义 Handler，实现日志属性及消息的自动脱敏
type RedactHandler struct {
	inner slog.Handler
}

func NewRedactHandler(inner slog.Handler) *RedactHandler {
	return &RedactHandler{inner: inner}
}

func (h *RedactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error {
	// 消息脱敏
	r.Message = Redact(r.Message)

	// 属性脱敏
	var newAttrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		if a.Value.Kind() == slog.KindString {
			newAttrs = append(newAttrs, slog.String(a.Key, Redact(a.Value.String())))
		} else {
			newAttrs = append(newAttrs, a)
		}
		return true
	})

	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	newRecord.AddAttrs(newAttrs...)
	return h.inner.Handle(ctx, newRecord)
}

func (h *RedactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redactedAttrs := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		if a.Value.Kind() == slog.KindString {
			redactedAttrs[i] = slog.String(a.Key, Redact(a.Value.String()))
		} else {
			redactedAttrs[i] = a
		}
	}
	return &RedactHandler{inner: h.inner.WithAttrs(redactedAttrs)}
}

func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{inner: h.inner.WithGroup(name)}
}

var (
	globalLogger *slog.Logger
	logMu        sync.Mutex
)

// InitLogger 初始化日志器（控制台 + 文件，带自动脱敏）
func InitLogger(logDir string, retainDays int) (*slog.Logger, func(), error) {
	logMu.Lock()
	defer logMu.Unlock()

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, nil, err
	}

	today := time.Now().Format("2006-01-02")
	logFile := filepath.Join(logDir, "app-"+today+".log")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, err
	}

	// 多路输出：标准错误 + 磁盘日志文件
	mw := io.MultiWriter(os.Stderr, f)

	jsonHandler := slog.NewJSONHandler(mw, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	handler := NewRedactHandler(jsonHandler)
	logger := slog.New(handler)
	globalLogger = logger
	slog.SetDefault(logger)

	cleanup := func() {
		_ = f.Sync()
		_ = f.Close()
	}

	return logger, cleanup, nil
}

func L() *slog.Logger {
	if globalLogger == nil {
		return slog.Default()
	}
	return globalLogger
}
