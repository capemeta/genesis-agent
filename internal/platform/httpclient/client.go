package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	tracecontract "genesis-agent/internal/capabilities/trace/contract"
	"genesis-agent/internal/platform/idgen"
	loggercontract "genesis-agent/internal/platform/logger"
)

// Client 定义统一 HTTP Client 接口。
type Client interface {
	Do(ctx context.Context, req *Request) (*Response, error)
	StreamSSE(ctx context.Context, req *Request) (EventStream, error)
}

type client struct {
	cfg           Config
	httpClient    *http.Client
	sseHTTPClient *http.Client
	logger        loggercontract.Logger
	tracer        tracecontract.Tracer
	idGenerator   idgen.Generator
	middlewares   []Middleware
	rand          *rand.Rand
}

// New 创建一个带默认治理能力的 HTTP Client。
func New(opts ...Option) Client {
	c := newDefaultClient()
	for _, opt := range opts {
		opt(c)
	}

	if c.httpClient == nil {
		c.httpClient = &http.Client{
			Transport: c.buildTransport(c.cfg.ResponseHeaderTimeout),
		}
	}
	if c.sseHTTPClient == nil {
		c.sseHTTPClient = &http.Client{
			Transport: c.buildTransport(c.cfg.SSEIdleTimeout),
		}
	}
	return c
}

func (c *client) Do(ctx context.Context, req *Request) (*Response, error) {
	return c.do(ctx, req, false)
}

func (c *client) StreamSSE(ctx context.Context, req *Request) (EventStream, error) {
	if req == nil {
		return nil, &Error{Kind: ErrorKindInvalidResponse, Message: "request 不能为空"}
	}

	requestID := c.requestID(req)
	ctx, cancel := c.requestContext(ctx, req.Timeout)

	span := c.startTrace(ctx, req, requestID, "http.sse")

	urlStr, baseReq, replayable, err := c.buildHTTPRequest(ctx, req, requestID)
	if err != nil {
		cancel()
		c.finishTraceWithError(ctx, span, err)
		return nil, err
	}

	if !replayable && req.Retry != nil && req.Retry.MaxAttempts > 1 {
		c.logger.Debug("http sse 请求体不可重放，忽略重试策略", "url", urlStr, "request_id", requestID)
	}

	baseReq.Header.Set("Accept", "text/event-stream")
	resp, err := c.sseHTTPClient.Do(baseReq)
	if err != nil {
		cancel()
		httpErr := c.mapTransportError(err, "stream_sse", urlStr, requestID)
		c.finishTraceWithError(ctx, span, httpErr)
		c.logResult(req, urlStr, requestID, 0, 0, httpErr, 0)
		return nil, httpErr
	}

	if !c.isExpectedStatus(req.ExpectedStatus, resp.StatusCode) {
		defer resp.Body.Close()
		body, readErr := c.readLimited(resp.Body, c.maxErrorBodyBytes())
		if readErr != nil {
			cancel()
			httpErr := c.mapBodyReadError(readErr, "stream_sse", urlStr, requestID)
			c.finishTraceWithError(ctx, span, httpErr)
			return nil, httpErr
		}

		httpErr := c.mapStatusError(resp.StatusCode, "stream_sse", urlStr, requestID, body)
		cancel()
		c.finishTraceWithError(ctx, span, httpErr)
		c.logResult(req, urlStr, requestID, resp.StatusCode, len(body), httpErr, 0)
		return nil, httpErr
	}

	c.logResult(req, urlStr, requestID, resp.StatusCode, 0, nil, 0)
	c.finishTraceWithError(ctx, span, nil)
	return newSSEStream(resp.Body, cancel), nil
}

func (c *client) do(ctx context.Context, req *Request, isSSE bool) (*Response, error) {
	if req == nil {
		return nil, &Error{Kind: ErrorKindInvalidResponse, Message: "request 不能为空"}
	}

	requestID := c.requestID(req)
	operation := "http.do"
	if isSSE {
		operation = "http.sse"
	}
	span := c.startTrace(ctx, req, requestID, operation)
	var finalErr error
	defer func() {
		c.finishTraceWithError(ctx, span, finalErr)
	}()

	policy := c.resolveRetryPolicy(req)
	attempt := 0
	for {
		attempt++

		attemptCtx, cancel := c.requestContext(ctx, req.Timeout)
		urlStr, httpReq, replayable, err := c.buildHTTPRequest(attemptCtx, req, requestID)
		if err != nil {
			cancel()
			finalErr = err
			return nil, err
		}

		httpResp, err := c.httpClient.Do(httpReq)
		if err != nil {
			cancel()
			httpErr := c.mapTransportError(err, "do", urlStr, requestID)
			if c.shouldRetry(policy, req, replayable, attempt, 0, httpErr) {
				c.logRetry(req, urlStr, requestID, attempt, httpErr)
				if waitErr := c.waitBackoff(ctx, policy, attempt, nil); waitErr != nil {
					finalErr = httpErr
					return nil, httpErr
				}
				continue
			}
			c.logResult(req, urlStr, requestID, 0, 0, httpErr, attempt)
			finalErr = httpErr
			return nil, httpErr
		}

		resp, httpErr := c.handleHTTPResponse(req, urlStr, requestID, httpResp)
		cancel()
		if httpErr != nil && c.shouldRetry(policy, req, replayable, attempt, httpResp.StatusCode, httpErr) {
			c.logRetry(req, urlStr, requestID, attempt, httpErr)
			if waitErr := c.waitBackoff(ctx, policy, attempt, httpResp); waitErr != nil {
				finalErr = httpErr
				return nil, httpErr
			}
			continue
		}

		if httpErr != nil {
			c.logResult(req, urlStr, requestID, httpResp.StatusCode, len(respBody(resp)), httpErr, attempt)
			finalErr = httpErr
			return nil, httpErr
		}

		c.logResult(req, urlStr, requestID, httpResp.StatusCode, len(resp.Body), nil, attempt)
		return resp, nil
	}
}

func (c *client) handleHTTPResponse(req *Request, urlStr string, requestID string, httpResp *http.Response) (*Response, error) {
	defer httpResp.Body.Close()

	bodyLimit := c.cfg.MaxResponseBodyBytes
	if req.MaxResponseBodyBytes > 0 {
		bodyLimit = req.MaxResponseBodyBytes
	}

	body, err := c.readLimited(httpResp.Body, bodyLimit)
	if err != nil {
		return nil, c.mapBodyReadError(err, "read_body", urlStr, requestID)
	}

	resp := &Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header.Clone(),
		Body:       body,
	}

	if c.isExpectedStatus(req.ExpectedStatus, httpResp.StatusCode) {
		return resp, nil
	}

	return resp, c.mapStatusError(httpResp.StatusCode, "do", urlStr, requestID, body)
}

func (c *client) buildHTTPRequest(ctx context.Context, req *Request, requestID string) (string, *http.Request, bool, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	urlStr, err := buildURL(req.BaseURL, req.Path, req.Query)
	if err != nil {
		return "", nil, false, &Error{
			Kind:      ErrorKindInvalidResponse,
			Message:   "构造请求 URL 失败",
			Operation: "build_request",
			Err:       err,
		}
	}

	bodyFactory, replayable, contentType, err := c.bodyFactory(req)
	if err != nil {
		return "", nil, false, err
	}

	body, err := bodyFactory()
	if err != nil {
		return "", nil, replayable, &Error{
			Kind:      ErrorKindInvalidResponse,
			Message:   "构造请求体失败",
			Operation: "build_request",
			URL:       urlStr,
			RequestID: requestID,
			Err:       err,
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return "", nil, replayable, &Error{
			Kind:      ErrorKindInvalidResponse,
			Message:   "创建 HTTP 请求失败",
			Operation: "build_request",
			URL:       urlStr,
			RequestID: requestID,
			Err:       err,
		}
	}

	httpReq.Header = cloneHeader(req.Headers)
	if contentType != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if c.cfg.UserAgent != "" && httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	if c.cfg.RequestIDHeader != "" && httpReq.Header.Get(c.cfg.RequestIDHeader) == "" {
		httpReq.Header.Set(c.cfg.RequestIDHeader, requestID)
	}
	if req.IdempotencyKey != "" && httpReq.Header.Get("Idempotency-Key") == "" {
		httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}
	if req.LastEventID != "" && httpReq.Header.Get("Last-Event-ID") == "" {
		httpReq.Header.Set("Last-Event-ID", req.LastEventID)
	}

	if err := applyAuth(httpReq, req.Auth); err != nil {
		return "", nil, replayable, &Error{
			Kind:      ErrorKindInvalidResponse,
			Message:   "应用认证配置失败",
			Operation: "build_request",
			URL:       urlStr,
			RequestID: requestID,
			Err:       err,
		}
	}

	return urlStr, httpReq, replayable, nil
}

func (c *client) bodyFactory(req *Request) (func() (io.ReadCloser, error), bool, string, error) {
	maxBytes := c.cfg.MaxRequestBodyBytes
	if req.MaxRequestBodyBytes > 0 {
		maxBytes = req.MaxRequestBodyBytes
	}

	switch body := req.Body.(type) {
	case nil:
		return func() (io.ReadCloser, error) { return http.NoBody, nil }, true, req.ContentType, nil
	case []byte:
		if maxBytes > 0 && int64(len(body)) > maxBytes {
			return nil, true, "", &Error{Kind: ErrorKindTooLarge, Message: "请求体超过限制"}
		}
		buf := append([]byte(nil), body...)
		return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(buf)), nil }, true, req.ContentType, nil
	case string:
		if maxBytes > 0 && int64(len(body)) > maxBytes {
			return nil, true, "", &Error{Kind: ErrorKindTooLarge, Message: "请求体超过限制"}
		}
		buf := []byte(body)
		return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(buf)), nil }, true, req.ContentType, nil
	case io.ReadCloser:
		return func() (io.ReadCloser, error) { return body, nil }, false, req.ContentType, nil
	case io.Reader:
		return func() (io.ReadCloser, error) { return io.NopCloser(body), nil }, false, req.ContentType, nil
	default:
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, true, "", &Error{
				Kind:      ErrorKindDecode,
				Message:   "编码 JSON 请求体失败",
				Operation: "encode_body",
				Err:       err,
			}
		}
		if maxBytes > 0 && int64(len(payload)) > maxBytes {
			return nil, true, "", &Error{Kind: ErrorKindTooLarge, Message: "请求体超过限制"}
		}
		contentType := req.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }, true, contentType, nil
	}
}

func buildURL(baseURL string, rawPath string, query url.Values) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("base_url 不能为空")
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	if rawPath != "" {
		if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") {
			base, err = url.Parse(rawPath)
			if err != nil {
				return "", err
			}
		} else {
			ref, err := url.Parse(rawPath)
			if err != nil {
				return "", err
			}
			base = base.ResolveReference(ref)
		}
	}

	if len(query) > 0 {
		q := base.Query()
		for key, values := range query {
			q.Del(key)
			for _, value := range values {
				q.Add(key, value)
			}
		}
		base.RawQuery = q.Encode()
	}

	return base.String(), nil
}

func applyAuth(req *http.Request, auth *AuthConfig) error {
	if auth == nil || auth.Type == "" || auth.Type == AuthTypeNone {
		return nil
	}

	switch auth.Type {
	case AuthTypeAPIKeyHeader:
		header := auth.HeaderName
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, auth.Value)
	case AuthTypeAPIKeyQuery:
		name := auth.QueryName
		if name == "" {
			name = "api_key"
		}
		q := req.URL.Query()
		q.Set(name, auth.Value)
		req.URL.RawQuery = q.Encode()
	case AuthTypeBearerToken:
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case AuthTypeBasicAuth:
		req.SetBasicAuth(auth.Username, auth.Password)
	case AuthTypeCustomHeader:
		if auth.HeaderName == "" {
			return fmt.Errorf("custom_header 模式必须提供 HeaderName")
		}
		req.Header.Set(auth.HeaderName, auth.Value)
	default:
		return fmt.Errorf("不支持的认证类型: %s", auth.Type)
	}
	return nil
}

func (c *client) requestContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = c.cfg.DefaultTimeout
	}
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *client) resolveRetryPolicy(req *Request) RetryPolicy {
	if req != nil && req.Retry != nil {
		return req.Retry.normalized()
	}
	return c.cfg.Retry.normalized()
}

func (c *client) shouldRetry(policy RetryPolicy, req *Request, replayable bool, attempt int, statusCode int, err error) bool {
	if attempt >= policy.MaxAttempts {
		return false
	}
	if !replayable {
		return false
	}

	method := http.MethodGet
	if req != nil && req.Method != "" {
		method = strings.ToUpper(req.Method)
	}
	if !containsFold(policy.RetryMethods, method) && !(method == http.MethodPost && req != nil && req.IdempotencyKey != "") {
		return false
	}

	var httpErr *Error
	if errors.As(err, &httpErr) {
		if httpErr.Kind == ErrorKindTimeout || httpErr.Kind == ErrorKindNetwork || httpErr.Kind == ErrorKindRateLimited {
			return true
		}
		if containsInt(policy.RetryStatusCodes, statusCode) {
			return true
		}
		return httpErr.Retryable
	}

	return containsInt(policy.RetryStatusCodes, statusCode)
}

func (c *client) waitBackoff(ctx context.Context, policy RetryPolicy, attempt int, resp *http.Response) error {
	delay := retryAfterDelay(resp)
	if delay <= 0 {
		delay = computeBackoff(policy, attempt, c.rand)
	}
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func computeBackoff(policy RetryPolicy, attempt int, rnd *rand.Rand) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	multiplier := math.Pow(policy.Multiplier, float64(attempt-1))
	delay := time.Duration(float64(policy.InitialBackoff) * multiplier)
	if delay > policy.MaxBackoff {
		delay = policy.MaxBackoff
	}
	if !policy.Jitter || delay <= 0 || rnd == nil {
		return delay
	}
	return time.Duration(rnd.Int63n(int64(delay) + 1))
}

func retryAfterDelay(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryTime, err := http.ParseTime(value); err == nil {
		delay := time.Until(retryTime)
		if delay > 0 {
			return delay
		}
	}
	return 0
}

func (c *client) readLimited(body io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(body)
	}

	reader := io.LimitReader(body, maxBytes+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, &Error{Kind: ErrorKindTooLarge, Message: "响应体超过限制", RawBody: truncateBytes(data, c.maxErrorBodyBytes())}
	}
	return data, nil
}

func (c *client) isExpectedStatus(expected []int, statusCode int) bool {
	if len(expected) == 0 {
		return statusCode >= 200 && statusCode < 300
	}
	for _, code := range expected {
		if code == statusCode {
			return true
		}
	}
	return false
}

func (c *client) mapTransportError(err error, operation string, urlStr string, requestID string) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Kind: ErrorKindTimeout, Message: "HTTP 请求超时", Retryable: true, Operation: operation, URL: urlStr, RequestID: requestID, Err: err}
	case errors.Is(err, context.Canceled):
		return &Error{Kind: ErrorKindCanceled, Message: "HTTP 请求已取消", Operation: operation, URL: urlStr, RequestID: requestID, Err: err}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		kind := ErrorKindNetwork
		message := "HTTP 网络错误"
		if netErr.Timeout() {
			kind = ErrorKindTimeout
			message = "HTTP 请求超时"
		}
		return &Error{Kind: kind, Message: message, Retryable: true, Operation: operation, URL: urlStr, RequestID: requestID, Err: err}
	}

	return &Error{Kind: ErrorKindNetwork, Message: "HTTP 网络错误", Retryable: true, Operation: operation, URL: urlStr, RequestID: requestID, Err: err}
}

func (c *client) mapBodyReadError(err error, operation string, urlStr string, requestID string) error {
	var httpErr *Error
	if errors.As(err, &httpErr) {
		httpErr.Operation = operation
		httpErr.URL = urlStr
		httpErr.RequestID = requestID
		return httpErr
	}
	return &Error{Kind: ErrorKindInvalidResponse, Message: "读取响应体失败", Operation: operation, URL: urlStr, RequestID: requestID, Err: err}
}

func (c *client) mapStatusError(statusCode int, operation string, urlStr string, requestID string, body []byte) error {
	kind := ErrorKindUpstream
	message := "上游返回错误响应"
	retryable := statusCode >= 500

	switch statusCode {
	case http.StatusUnauthorized:
		kind = ErrorKindUnauthorized
		message = "上游鉴权失败"
	case http.StatusForbidden:
		kind = ErrorKindForbidden
		message = "上游拒绝访问"
	case http.StatusNotFound:
		kind = ErrorKindNotFound
		message = "上游资源不存在"
	case http.StatusTooManyRequests:
		kind = ErrorKindRateLimited
		message = "上游触发限流"
		retryable = true
	}

	return &Error{
		Kind:       kind,
		Message:    message,
		StatusCode: statusCode,
		Retryable:  retryable,
		Operation:  operation,
		URL:        urlStr,
		RequestID:  requestID,
		RawBody:    truncateBytes(body, c.maxErrorBodyBytes()),
	}
}

func (c *client) requestID(req *Request) string {
	if req != nil && req.Headers != nil && c.cfg.RequestIDHeader != "" {
		if value := req.Headers.Get(c.cfg.RequestIDHeader); value != "" {
			return value
		}
	}
	return c.idGenerator.Generate()
}

func (c *client) buildTransport(responseHeaderTimeout time.Duration) http.RoundTripper {
	base := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          c.cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   c.cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       c.cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   c.cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ForceAttemptHTTP2:     true,
	}

	var rt http.RoundTripper = base
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		rt = c.middlewares[i](rt)
	}
	return rt
}

func (c *client) startTrace(ctx context.Context, req *Request, requestID string, operation string) *tracecontract.Span {
	if c.tracer == nil {
		return nil
	}
	span := c.tracer.StartSpan(ctx, operation, requestID)
	if span == nil {
		return nil
	}
	if span.Tags == nil {
		span.Tags = make(map[string]string)
	}
	if req != nil {
		span.Tags["method"] = strings.ToUpper(defaultMethod(req.Method))
		span.Tags["path"] = req.Path
	}
	span.Tags["request_id"] = requestID
	return span
}

func (c *client) finishTraceWithError(ctx context.Context, span *tracecontract.Span, err error) {
	if span == nil || c.tracer == nil {
		return
	}
	if httpErr, ok := err.(*Error); ok {
		if httpErr.Kind != "" {
			span.Tags["error_kind"] = string(httpErr.Kind)
		}
		if httpErr.StatusCode > 0 {
			span.Tags["status_code"] = strconv.Itoa(httpErr.StatusCode)
		}
	}
	c.tracer.EndSpan(ctx, span, err)
}

func (c *client) logRetry(req *Request, urlStr string, requestID string, attempt int, err error) {
	c.logger.Warn("http 请求触发重试",
		"method", defaultMethod(req.Method),
		"url", urlStr,
		"attempt", attempt,
		"request_id", requestID,
		"error", err,
	)
}

func (c *client) logResult(req *Request, urlStr string, requestID string, statusCode int, bodyBytes int, err error, attempts int) {
	args := []any{
		"method", defaultMethod(req.Method),
		"url", urlStr,
		"status_code", statusCode,
		"body_bytes", bodyBytes,
		"attempts", attempts,
		"request_id", requestID,
	}

	if upstream := req.Metadata["upstream_service"]; upstream != "" {
		args = append(args, "upstream_service", upstream)
	}

	if err != nil {
		args = append(args, "error", err)
		c.logger.Error("http 请求失败", args...)
		return
	}
	c.logger.Info("http 请求完成", args...)
}

func (c *client) maxErrorBodyBytes() int64 {
	return c.cfg.MaxErrorBodyBytes
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return make(http.Header)
	}
	return header.Clone()
}

func truncateBytes(data []byte, maxBytes int64) []byte {
	if maxBytes <= 0 || int64(len(data)) <= maxBytes {
		return append([]byte(nil), data...)
	}
	return append([]byte(nil), data[:maxBytes]...)
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func defaultMethod(method string) string {
	if strings.TrimSpace(method) == "" {
		return http.MethodGet
	}
	return strings.ToUpper(method)
}

type noopTracer struct{}

func (noopTracer) StartSpan(_ context.Context, operation, id string) *tracecontract.Span {
	return &tracecontract.Span{
		SpanID:    id,
		Operation: operation,
		StartedAt: time.Now(),
		Tags:      make(map[string]string),
	}
}

func (noopTracer) EndSpan(_ context.Context, _ *tracecontract.Span, _ error) {}

func respBody(resp *Response) []byte {
	if resp == nil {
		return nil
	}
	return resp.Body
}
