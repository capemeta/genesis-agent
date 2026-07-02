// Package http HTTP 传输层：基于标准 net/http 提供 RESTful API 和 SSE 流式接口
// 依赖 internal/app 应用服务层，与 CLI 传输层共享同一套业务逻辑
//
// 预留骨架（Phase 1B 实现），当前仅用于编译验证
package http

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"genesis-agent/internal/app"
	"genesis-agent/products/enterprise/internal/interfaces/http/handler"
)

// ServerConfig HTTP 服务器配置
type ServerConfig struct {
	Host         string        // 监听地址（默认 "0.0.0.0"）
	Port         int           // 监听端口（默认 8080）
	ReadTimeout  time.Duration // 请求读取超时
	WriteTimeout time.Duration // 响应写入超时
	IdleTimeout  time.Duration // 空闲连接超时
}

// DefaultServerConfig 默认服务器配置
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Host:         "0.0.0.0",
		Port:         8080,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second, // SSE 流式接口需要较长的写入时间
		IdleTimeout:  120 * time.Second,
	}
}

// Server HTTP API 服务器
type Server struct {
	cfg    ServerConfig
	svc    app.AgentService
	httpSv *http.Server
}

// NewServer 创建 HTTP API 服务器
func NewServer(svc app.AgentService, cfg ServerConfig) *Server {
	s := &Server{
		cfg: cfg,
		svc: svc,
	}

	mux := newRouter(svc)
	s.httpSv = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	return s
}

// Start 启动 HTTP 服务器（阻塞直到上下文取消或出错）
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		if err := s.httpSv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		// 优雅关闭：等待正在处理的请求完成（最多 30 秒）
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.httpSv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return fmt.Errorf("HTTP 服务器启动失败: %w", err)
	}
}

// Addr 返回服务器监听地址
func (s *Server) Addr() string {
	return fmt.Sprintf("http://%s:%d", s.cfg.Host, s.cfg.Port)
}

// 确保 handler 包被引用（避免 IDE 报 unused import）
var _ = handler.NewAgentHandler
