package httpclient

import (
	"net/http"
	"net/url"
	"time"
)

// Request 描述一次 HTTP 请求。
type Request struct {
	Method  string
	BaseURL string
	Path    string
	Query   url.Values

	Headers http.Header

	Body        any
	ContentType string

	Auth *AuthConfig

	Timeout time.Duration
	Retry   *RetryPolicy

	ExpectedStatus []int

	IdempotencyKey string
	LastEventID    string

	Metadata map[string]string

	MaxRequestBodyBytes  int64
	MaxResponseBodyBytes int64
}

// Response 描述一次完整 HTTP 响应。
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// SSEEvent 表示一个 SSE 事件。
type SSEEvent struct {
	ID    string
	Event string
	Data  []byte
	Retry int
}

// EventStream 表示可持续接收的 SSE 事件流。
type EventStream interface {
	Recv() (*SSEEvent, error)
	Close() error
}
