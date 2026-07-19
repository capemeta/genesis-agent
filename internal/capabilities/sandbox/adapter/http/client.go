// Package http 提供 genesis-sandbox HTTP API 的产品无关客户端适配。
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
)

const (
	defaultQueueWaitSeconds = 10
	// 与 genesis-sandbox/sdks/go/sandbox 默认心跳参数对齐。
	defaultRenewInterval = 30 * time.Second
	defaultRenewExtend   = 90 * time.Second
	defaultPollStart     = 500 * time.Millisecond
	defaultPollMax       = 3 * time.Second
	defaultCloseTimeout  = 30 * time.Second
	defaultRenewTimeout  = 15 * time.Second
	defaultSessionTTL    = 300
)

// Config 描述 genesis-sandbox HTTP client 配置。
// 单次请求超时一律由 context / RunOptions.Timeout 控制，禁止依赖 http.Client.Timeout。
type Config struct {
	BaseURL       string
	APIKey        string
	Client        *http.Client
	RenewInterval time.Duration
	RenewExtend   time.Duration
	PollStart     time.Duration
	PollMax       time.Duration
	CloseTimeout  time.Duration
}

// Client 实现 sandbox CommandClient。
type Client struct {
	baseURL       *url.URL
	apiKey        string
	httpClient    *http.Client
	renewInterval time.Duration
	renewExtend   time.Duration
	pollStart     time.Duration
	pollMax       time.Duration
	closeTimeout  time.Duration
}

// New 创建 HTTP sandbox client。
func New(cfg Config) (*Client, error) {
	rawBaseURL := strings.TrimSpace(cfg.BaseURL)
	if rawBaseURL == "" {
		return nil, fmt.Errorf("sandbox http base_url不能为空")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("解析sandbox http base_url失败: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("sandbox http base_url必须包含scheme和host")
	}
	httpClient, err := unboundHTTPClient(cfg.Client)
	if err != nil {
		return nil, err
	}
	renewInterval := cfg.RenewInterval
	if renewInterval <= 0 {
		renewInterval = defaultRenewInterval
	}
	renewExtend := cfg.RenewExtend
	if renewExtend <= 0 {
		renewExtend = defaultRenewExtend
	}
	pollStart := cfg.PollStart
	if pollStart <= 0 {
		pollStart = defaultPollStart
	}
	pollMax := cfg.PollMax
	if pollMax <= 0 {
		pollMax = defaultPollMax
	}
	closeTimeout := cfg.CloseTimeout
	if closeTimeout <= 0 {
		closeTimeout = defaultCloseTimeout
	}
	return &Client{
		baseURL:       parsed,
		apiKey:        strings.TrimSpace(cfg.APIKey),
		httpClient:    httpClient,
		renewInterval: renewInterval,
		renewExtend:   renewExtend,
		pollStart:     pollStart,
		pollMax:       pollMax,
		closeTimeout:  closeTimeout,
	}, nil
}

// unboundHTTPClient 返回 Timeout==0 的 http.Client。
// 始终 clone，避免与调用方共享同一 Client 后被改回有限 Timeout。
func unboundHTTPClient(src *http.Client) (*http.Client, error) {
	if src == nil {
		return &http.Client{}, nil
	}
	clone := *src
	clone.Timeout = 0
	return &clone, nil
}

// deleteSessionBestEffort 在 OpenSession 校验失败时清理半成品 Session，避免无超时挂起。
func (c *Client) deleteSessionBestEffort(sessionID string) {
	if c == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.closeTimeout)
	defer cancel()
	_ = c.doJSON(ctx, http.MethodDelete, "/v1/sessions/"+url.PathEscape(sessionID), nil, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body io.Reader, out any) error {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(c.baseURL.Path, "/") + path})
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("创建sandbox http请求失败: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("调用sandbox http失败: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapHTTPError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("解析sandbox http响应失败: %w", err))
	}
	return nil
}

func mapHTTPError(resp *http.Response) error {
	var apiErr errorResponse
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if len(data) > 0 {
		_ = json.Unmarshal(data, &apiErr)
	}
	message := strings.TrimSpace(apiErr.Message)
	if message == "" {
		message = strings.TrimSpace(string(data))
	}
	if message == "" {
		message = resp.Status
	}
	code := strings.TrimSpace(apiErr.Code)
	upperCode := strings.ToUpper(code)
	upperMessage := strings.ToUpper(message)
	switch {
	case resp.StatusCode == http.StatusBadRequest || code == "INVALID_ARGUMENT":
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("%s", message))
	case resp.StatusCode == http.StatusNotFound || strings.Contains(upperCode, "NOT_FOUND") ||
		strings.HasPrefix(upperMessage, "NOT_FOUND"):
		// 保留 code，供 fs 层识别 NOT_FOUND / SANDBOX_NOT_FOUND（含下划线形态）。
		label := firstNonEmpty(code, "NOT_FOUND")
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("%s: %s", label, message))
	case resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusPreconditionFailed ||
		code == "ALREADY_EXISTS" || code == "PRECONDITION_FAILED" || code == "CONFLICT":
		// 保留原始 code/message，供 fsErrorFromExec 区分 AlreadyExists / ModifiedExternally。
		if code != "" {
			return execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("%s: %s", code, message))
		}
		return execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("%s", message))
	case resp.StatusCode == http.StatusForbidden || code == "POLICY_DENIED" || code == "NETWORK_DENIED":
		return execcontract.NewError(execcontract.ErrCodePermissionDenied, fmt.Errorf("%s", message))
	case resp.StatusCode == http.StatusTooManyRequests || code == "QUEUE_FULL" || code == "QUOTA_EXCEEDED":
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("%s", message))
	case resp.StatusCode == http.StatusServiceUnavailable || code == "RUNTIME_UNAVAILABLE":
		return execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("%s", message))
	case code == "EXEC_TIMEOUT":
		return execcontract.NewError(execcontract.ErrCodeTimeout, fmt.Errorf("%s", message))
	default:
		return execcontract.NewError(execcontract.ErrCodeRunnerFailed, fmt.Errorf("%s", message))
	}
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *Client) doRaw(ctx context.Context, method, apiPath string, query url.Values, contentType string, body io.Reader, headers http.Header) (*http.Response, error) {
	rawQuery := ""
	if query != nil {
		rawQuery = query.Encode()
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(c.baseURL.Path, "/") + apiPath, RawQuery: rawQuery})
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeInvalidInput, fmt.Errorf("创建sandbox http请求失败: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, execcontract.NewError(execcontract.ErrCodeSandboxUnavailable, fmt.Errorf("调用sandbox http失败: %w", err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, mapHTTPError(resp)
	}
	return resp, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
