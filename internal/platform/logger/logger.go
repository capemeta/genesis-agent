// Package logger 提供基于标准库 log/slog 的结构化日志实现。
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Level 日志级别别名。
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// consoleHandler 自定义 slog Handler，输出更友好的控制台格式。
type consoleHandler struct {
	out   io.Writer
	level slog.Level
	attrs []slog.Attr
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	timeStr := r.Time.Format("15:04:05")
	levelStr := levelName(r.Level)
	line := fmt.Sprintf("[%s] [%s] %s", timeStr, levelStr, r.Message)

	for _, a := range h.attrs {
		line += fmt.Sprintf("  %s=%v", a.Key, a.Value)
	}
	r.Attrs(func(a slog.Attr) bool {
		line += fmt.Sprintf("  %s=%v", a.Key, a.Value)
		return true
	})

	fmt.Fprintln(h.out, line)
	return nil
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &consoleHandler{out: h.out, level: h.level, attrs: newAttrs}
}

func (h *consoleHandler) WithGroup(_ string) slog.Handler {
	return h
}

func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN "
	case l >= slog.LevelInfo:
		return "INFO "
	default:
		return "DEBUG"
	}
}

type slogLogger struct {
	inner *slog.Logger
}

// ParseLevel 将字符串解析为 Level，不认识的值默认返回 Info。
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// New 创建控制台 Logger，level 控制最低输出级别。
func New(level Level) Logger {
	handler := &consoleHandler{out: os.Stdout, level: level}
	return &slogLogger{inner: slog.New(handler)}
}

// NewNop 创建空 Logger，用于测试或静默模式。
func NewNop() Logger {
	return &slogLogger{inner: slog.New(nopHandler{})}
}

func (l *slogLogger) Info(msg string, args ...any)  { l.inner.Info(msg, args...) }
func (l *slogLogger) Warn(msg string, args ...any)  { l.inner.Warn(msg, args...) }
func (l *slogLogger) Error(msg string, args ...any) { l.inner.Error(msg, args...) }
func (l *slogLogger) Debug(msg string, args ...any) { l.inner.Debug(msg, args...) }

func (l *slogLogger) With(args ...any) Logger {
	return &slogLogger{inner: l.inner.With(args...)}
}

type nopHandler struct{}

func (nopHandler) Enabled(_ context.Context, _ slog.Level) bool  { return false }
func (nopHandler) Handle(_ context.Context, _ slog.Record) error { return nil }
func (n nopHandler) WithAttrs(_ []slog.Attr) slog.Handler        { return n }
func (n nopHandler) WithGroup(_ string) slog.Handler             { return n }
