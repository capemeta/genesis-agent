// Package logger 定义结构化日志接口，并提供基于 slog 的默认实现。
package logger

// Logger 日志接口，支持结构化键值对字段。
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Debug(msg string, args ...any)
	With(args ...any) Logger
}
