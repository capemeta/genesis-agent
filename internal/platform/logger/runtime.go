package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"genesis-agent/internal/platform/config"
)

// RuntimeLogging 聚合三类运行日志通道；进程内应只创建一次并注入。
type RuntimeLogging struct {
	AgentLogger Logger
	AuditWriter io.WriteCloser
	UsageWriter io.WriteCloser
	closers     []io.Closer
}

// Close 关闭所有文件 Writer。
func (r *RuntimeLogging) Close() error {
	if r == nil {
		return nil
	}
	var first error
	for _, c := range r.closers {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// RuntimeLoggingOptions 控制 RuntimeLogging 创建行为。
type RuntimeLoggingOptions struct {
	// ConfigDir 用于解析相对 log.dir（相对配置目录的父目录，与旧 path 行为一致）。
	ConfigDir string
	// Quiet 为 true 时 agent 写文件；false 时 agent 写 stdout（仍可启用 audit/usage 文件）。
	Quiet bool
	// ConsoleAlso 在 Quiet 文件模式下额外 tee 到 stdout（默认 false）。
	ConsoleAlso bool
}

// NewRuntimeLogging 按配置创建三类通道；调用方负责 Close。
func NewRuntimeLogging(cfg config.LogConfig, opts RuntimeLoggingOptions) (*RuntimeLogging, error) {
	dir, err := resolveLogDir(opts.ConfigDir, cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	out := &RuntimeLogging{}
	rotateBase := RotateOptions{
		Daily:      cfg.Rotate.DailyEnabled(),
		MaxSizeMB:  cfg.Rotate.MaxSizeMB,
		RetainDays: cfg.Rotate.RetainDays,
		Compress:   cfg.Rotate.Compress,
	}

	agentCh := cfg.Channels["agent"]
	if agentCh.ChannelEnabled() {
		level := ParseLevel(firstNonEmpty(agentCh.Level, cfg.Level))
		format := strings.ToLower(strings.TrimSpace(agentCh.Format))
		if opts.Quiet {
			w, err := newChannelWriter(dir, channelStem(agentCh.File, "agent"), rotateBase, agentCh.RetainDays)
			if err != nil {
				_ = out.Close()
				return nil, err
			}
			out.closers = append(out.closers, w)
			var agentOut io.Writer = w
			if opts.ConsoleAlso {
				agentOut = io.MultiWriter(w, os.Stdout)
			}
			out.AgentLogger = newWriterLogger(level, agentOut, format)
		} else if format == "json" {
			out.AgentLogger = newWriterLogger(level, os.Stdout, "json")
		} else {
			out.AgentLogger = New(level)
		}
	} else {
		out.AgentLogger = NewNop()
	}

	if ch := cfg.Channels["audit"]; ch.ChannelEnabled() {
		w, err := newChannelWriter(dir, channelStem(ch.File, "audit"), rotateBase, ch.RetainDays)
		if err != nil {
			_ = out.Close()
			return nil, err
		}
		out.AuditWriter = w
		out.closers = append(out.closers, w)
	}

	if ch := cfg.Channels["usage"]; ch.ChannelEnabled() {
		w, err := newChannelWriter(dir, channelStem(ch.File, "usage"), rotateBase, ch.RetainDays)
		if err != nil {
			_ = out.Close()
			return nil, err
		}
		out.UsageWriter = w
		out.closers = append(out.closers, w)
	}

	if out.AgentLogger == nil {
		out.AgentLogger = NewNop()
	}
	return out, nil
}

func newChannelWriter(dir, name string, base RotateOptions, retainDays int) (*RotatingWriter, error) {
	opts := base
	if retainDays > 0 {
		opts.RetainDays = retainDays
	}
	return NewRotatingWriter(dir, name, opts)
}

func channelStem(file, fallback string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return fallback
	}
	base := filepath.Base(file)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func resolveLogDir(configDir string, cfg config.LogConfig) (string, error) {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		dir = ".genesis/logs"
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir), nil
	}
	base := filepath.Dir(strings.TrimSpace(configDir))
	if strings.TrimSpace(configDir) == "" {
		base = "."
	}
	return filepath.Clean(filepath.Join(base, dir)), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func newWriterLogger(level Level, out io.Writer, format string) Logger {
	if strings.EqualFold(format, "json") {
		handler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})
		return &slogLogger{inner: slog.New(handler)}
	}
	handler := &consoleHandler{out: out, level: level}
	return &slogLogger{inner: slog.New(handler)}
}

// NewFileLogger 创建基于滚动 Writer 的文件 Logger（单通道 agent 便捷方法）。
// 返回的 closer 必须在不再使用时调用；产品装配应优先使用 NewRuntimeLogging。
func NewFileLogger(level Level, filePath string) (Logger, io.Closer, error) {
	dir := filepath.Dir(filePath)
	name := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	if name == "" {
		name = "agent"
	}
	w, err := NewRotatingWriter(dir, name, RotateOptions{Daily: true, MaxSizeMB: 100, RetainDays: 14})
	if err != nil {
		return nil, nil, err
	}
	return newWriterLogger(level, w, "text"), w, nil
}
