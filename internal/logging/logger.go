// Package logging 提供应用统一日志设施：JSON 日志 + 按天落盘 + 敏感信息自动脱敏。
// 脱敏发生在 slog.Handler 层（RedactHandler），因此所有经 slog 输出的记录
// 无论来源模块都无法绕过；各模块不应绕过 L()/slog.Default 直接打印含凭据的文本。
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// NewRedactHandler 包装内层 Handler，对经由它输出的所有日志做脱敏。
func NewRedactHandler(inner slog.Handler) *RedactHandler {
	return &RedactHandler{inner: inner}
}

// Enabled 直接委托内层 Handler 判断级别是否输出。
func (h *RedactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle 实现 slog.Handler：复制记录并对 Message 与字符串类型属性逐条 Redact 后下传。
// 只脱敏 KindString 属性，非字符串值（数字/布尔等）无法承载凭据文本，原样透传。
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

// WithAttrs 返回预绑定属性的新 Handler；绑定前先脱敏，防止凭据随 logger 派生扩散。
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

// WithGroup 返回带属性组前缀的新 Handler，脱敏行为不变。
func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{inner: h.inner.WithGroup(name)}
}

var (
	globalLogger *slog.Logger
	logMu        sync.Mutex
)

// pruneOldLogs 按天清理过期日志：删除 logDir 下 mtime 早于 retainDays 天前的
// app-*.log（InitLogger 在打开当天文件前调用；当天文件 mtime 必然最新，天然豁免）。
// retainDays<=0 视为不清理。单文件删除失败仅告警不阻断初始化——日志清理是尽力
// 而为的辅助能力，句柄占用/权限异常不应影响应用启动。抽成独立函数便于单测。
func pruneOldLogs(logDir string, retainDays int) {
	if retainDays <= 0 {
		return
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		slog.Warn("logging: prune old logs failed to read dir", "dir", logDir, "err", err)
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retainDays)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "app-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(logDir, name)); err != nil {
			slog.Warn("logging: remove expired log file failed", "file", name, "err", err)
		}
	}
}

// InitLogger 初始化日志器（控制台 + 文件，带自动脱敏；初始化时顺带按保留天数清理过期日志）
func InitLogger(logDir string, retainDays int) (*slog.Logger, func(), error) {
	logMu.Lock()
	defer logMu.Unlock()

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, nil, err
	}

	// 先清理再落盘：让 retainDays 配置真正生效（此前参数一直被忽略）
	pruneOldLogs(logDir, retainDays)

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

// L 返回全局 logger；InitLogger 未调用时回退到 slog.Default()（仅 stderr、无脱敏），保证任何时机调用都不 panic。
func L() *slog.Logger {
	if globalLogger == nil {
		return slog.Default()
	}
	return globalLogger
}
