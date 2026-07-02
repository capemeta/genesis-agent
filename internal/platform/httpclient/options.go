package httpclient

import (
	"math/rand"
	"net/http"
	"time"

	tracecontract "genesis-agent/internal/capabilities/trace/contract"
	"genesis-agent/internal/platform/idgen"
	loggercontract "genesis-agent/internal/platform/logger"
	sloglogger "genesis-agent/internal/platform/logger"
)

// Middleware 用于包装底层 RoundTripper。
type Middleware func(next http.RoundTripper) http.RoundTripper

// Config 描述 HTTP Client 默认配置。
type Config struct {
	DefaultTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	TLSHandshakeTimeout   time.Duration
	IdleConnTimeout       time.Duration
	SSEIdleTimeout        time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxResponseBodyBytes  int64
	MaxRequestBodyBytes   int64
	MaxErrorBodyBytes     int64
	UserAgent             string
	RequestIDHeader       string
	Retry                 RetryPolicy
}

// Option 描述 Client 构造选项。
type Option func(*client)

func defaultConfig() Config {
	return Config{
		DefaultTimeout:        30 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		SSEIdleTimeout:        0,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxResponseBodyBytes:  4 << 20,
		MaxRequestBodyBytes:   4 << 20,
		MaxErrorBodyBytes:     4 << 10,
		UserAgent:             "genesis-agent/httpclient",
		RequestIDHeader:       "X-Request-ID",
		Retry: RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: 200 * time.Millisecond,
			MaxBackoff:     2 * time.Second,
			Multiplier:     2,
			Jitter:         true,
		},
	}
}

// WithConfig 覆盖默认配置。
func WithConfig(cfg Config) Option {
	return func(c *client) {
		c.cfg = normalizeConfig(cfg)
	}
}

// WithLogger 设置日志实现。
func WithLogger(log loggercontract.Logger) Option {
	return func(c *client) {
		if log != nil {
			c.logger = log
		}
	}
}

// WithTracer 设置追踪实现。
func WithTracer(tracer tracecontract.Tracer) Option {
	return func(c *client) {
		if tracer != nil {
			c.tracer = tracer
		}
	}
}

// WithIDGenerator 设置请求 ID 生成器。
func WithIDGenerator(generator idgen.Generator) Option {
	return func(c *client) {
		if generator != nil {
			c.idGenerator = generator
		}
	}
}

// WithHTTPClient 设置普通请求使用的 http.Client。
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithSSEHTTPClient 设置 SSE 请求使用的 http.Client。
func WithSSEHTTPClient(httpClient *http.Client) Option {
	return func(c *client) {
		if httpClient != nil {
			c.sseHTTPClient = httpClient
		}
	}
}

// WithMiddleware 注册底层 RoundTripper 中间件。
func WithMiddleware(mw Middleware) Option {
	return func(c *client) {
		if mw != nil {
			c.middlewares = append(c.middlewares, mw)
		}
	}
}

func normalizeConfig(cfg Config) Config {
	defaults := defaultConfig()

	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = defaults.DefaultTimeout
	}
	if cfg.ResponseHeaderTimeout <= 0 {
		cfg.ResponseHeaderTimeout = defaults.ResponseHeaderTimeout
	}
	if cfg.TLSHandshakeTimeout <= 0 {
		cfg.TLSHandshakeTimeout = defaults.TLSHandshakeTimeout
	}
	if cfg.IdleConnTimeout <= 0 {
		cfg.IdleConnTimeout = defaults.IdleConnTimeout
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = defaults.MaxIdleConns
	}
	if cfg.MaxIdleConnsPerHost <= 0 {
		cfg.MaxIdleConnsPerHost = defaults.MaxIdleConnsPerHost
	}
	if cfg.MaxResponseBodyBytes <= 0 {
		cfg.MaxResponseBodyBytes = defaults.MaxResponseBodyBytes
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = defaults.MaxRequestBodyBytes
	}
	if cfg.MaxErrorBodyBytes <= 0 {
		cfg.MaxErrorBodyBytes = defaults.MaxErrorBodyBytes
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaults.UserAgent
	}
	if cfg.RequestIDHeader == "" {
		cfg.RequestIDHeader = defaults.RequestIDHeader
	}
	cfg.Retry = cfg.Retry.normalized()
	return cfg
}

func newDefaultClient() *client {
	cfg := normalizeConfig(defaultConfig())
	return &client{
		cfg:         cfg,
		logger:      sloglogger.NewNop(),
		tracer:      noopTracer{},
		idGenerator: idgen.NewUUIDGenerator(),
		rand:        rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // 仅用于退避抖动
	}
}
