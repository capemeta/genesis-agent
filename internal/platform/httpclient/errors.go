package httpclient

import "fmt"

// ErrorKind 表示错误分类。
type ErrorKind string

const (
	ErrorKindTimeout         ErrorKind = "timeout"
	ErrorKindCanceled        ErrorKind = "canceled"
	ErrorKindNetwork         ErrorKind = "network"
	ErrorKindUnauthorized    ErrorKind = "unauthorized"
	ErrorKindForbidden       ErrorKind = "forbidden"
	ErrorKindNotFound        ErrorKind = "not_found"
	ErrorKindRateLimited     ErrorKind = "rate_limited"
	ErrorKindUpstream        ErrorKind = "upstream"
	ErrorKindInvalidResponse ErrorKind = "invalid_response"
	ErrorKindDecode          ErrorKind = "decode"
	ErrorKindSSE             ErrorKind = "sse"
	ErrorKindTooLarge        ErrorKind = "too_large"
)

// Error 是 HTTP Client 统一错误模型。
type Error struct {
	Kind         ErrorKind
	Message      string
	StatusCode   int
	Retryable    bool
	Operation    string
	URL          string
	RequestID    string
	UpstreamCode string
	RawBody      []byte
	Err          error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	base := e.Message
	if base == "" {
		base = string(e.Kind)
	}

	switch {
	case e.StatusCode > 0 && e.URL != "":
		return fmt.Sprintf("%s (status=%d url=%s)", base, e.StatusCode, e.URL)
	case e.StatusCode > 0:
		return fmt.Sprintf("%s (status=%d)", base, e.StatusCode)
	case e.URL != "":
		return fmt.Sprintf("%s (url=%s)", base, e.URL)
	default:
		return base
	}
}

// Unwrap 返回底层错误。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
