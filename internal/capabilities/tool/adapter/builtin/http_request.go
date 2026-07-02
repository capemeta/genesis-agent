// Package builtin 提供内置工具实现。
package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	connection "genesis-agent/internal/capabilities/connection/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

const (
	httpToolWorkspaceEnv             = "GENESIS_HTTP_TOOL_WORKSPACE"
	defaultHTTPToolMaxMultipartBytes = 4 << 20
)

// HTTPRequestTool 是通用 HTTP 请求工具。
// 设计目标是提供“底层万能请求能力”，而访问控制与准入限制交给上层治理。
type HTTPRequestTool struct {
	client   platformhttp.Client
	resolver connection.HTTPResolver
}

type httpRequestInput struct {
	TenantID             string                `json:"tenant_id"`
	ConnectionRef        string                `json:"connection_ref"`
	Method               string                `json:"method"`
	URL                  string                `json:"url"`
	BaseURL              string                `json:"base_url"`
	Path                 string                `json:"path"`
	Query                map[string]any        `json:"query"`
	Headers              map[string]any        `json:"headers"`
	Body                 any                   `json:"body"`
	ContentType          string                `json:"content_type"`
	TimeoutMS            int                   `json:"timeout_ms"`
	ExpectedStatus       []int                 `json:"expected_status"`
	IdempotencyKey       string                `json:"idempotency_key"`
	MaxResponseBodyBytes int64                 `json:"max_response_body_bytes"`
	MaxRequestBodyBytes  int64                 `json:"max_request_body_bytes"`
	Auth                 *httpRequestAuthInput `json:"auth"`
	Multipart            *multipartInput       `json:"multipart"`
	Download             *downloadInput        `json:"download"`
}

type httpRequestAuthInput struct {
	Type       string `json:"type"`
	HeaderName string `json:"header_name"`
	QueryName  string `json:"query_name"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	Token      string `json:"token"`
	Value      string `json:"value"`
}

type multipartInput struct {
	Fields map[string]any       `json:"fields"`
	Files  []multipartFileInput `json:"files"`
}

type multipartFileInput struct {
	Field       string `json:"field"`
	Path        string `json:"path"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type downloadInput struct {
	SaveAs   string `json:"save_as"`
	MaxBytes int64  `json:"max_bytes"`
}

type httpRequestResult struct {
	StatusCode  int                 `json:"status_code"`
	Headers     map[string][]string `json:"headers"`
	ContentType string              `json:"content_type,omitempty"`
	RequestID   string              `json:"request_id,omitempty"`
	BodyText    string              `json:"body_text,omitempty"`
	BodyJSON    any                 `json:"body_json,omitempty"`
	Download    *downloadResult     `json:"download,omitempty"`
}

type downloadResult struct {
	SavedAs string `json:"saved_as"`
	Bytes   int    `json:"bytes"`
}

// NewHTTPRequestTool 创建通用 HTTP 请求工具。
func NewHTTPRequestTool(client platformhttp.Client, resolvers ...connection.HTTPResolver) tool.Tool {
	var resolver connection.HTTPResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	return &HTTPRequestTool{client: client, resolver: resolver}
}

func (t *HTTPRequestTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name: "http_request",
		Description: "发起通用HTTP请求，适用于调用外部API、Webhook或业务接口。" +
			"支持 connection_ref、method、url/base_url+path、query、headers、JSON/body、multipart 文件上传、受控下载、超时、期望状态码和常见认证方式。" +
			"推荐业务接口通过 connection_ref 调用，由连接配置集中管理 base_url、认证和默认策略。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"tenant_id":               {Type: "string", Description: "租户 ID。省略时使用 dev。使用 connection_ref 时用于隔离连接与密钥。"},
				"connection_ref":          {Type: "string", Description: "预配置 HTTP 连接 ID。推荐用于业务接口调用，可自动注入 base_url、默认 header、认证、超时和重试策略。"},
				"method":                  {Type: "string", Description: "HTTP 方法，默认 GET。支持 GET、POST、PUT、PATCH、DELETE、HEAD、OPTIONS。", Enum: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}},
				"url":                     {Type: "string", Description: "完整 URL。若提供，则优先于 base_url + path。使用 connection_ref 时不允许传入。"},
				"base_url":                {Type: "string", Description: "基础地址，如 https://api.example.com。使用 connection_ref 时不允许传入。"},
				"path":                    {Type: "string", Description: "请求路径，如 /v1/orders。"},
				"query":                   {Type: "object", Description: "查询参数对象。值可为 string、number、bool 或数组。"},
				"headers":                 {Type: "object", Description: "请求头对象。值可为 string 或字符串数组。"},
				"body":                    {Type: "object", Description: "请求体。可传 JSON 对象、数组、字符串等。不能与 multipart 同时使用。"},
				"content_type":            {Type: "string", Description: "请求体 Content-Type。未提供且 body 为对象时默认 application/json。"},
				"timeout_ms":              {Type: "integer", Description: "请求超时毫秒数。省略则使用连接配置或平台默认值。"},
				"expected_status":         {Type: "array", Description: "期望成功状态码列表，例如 [200, 201]。", Items: &tool.ParameterSchema{Type: "integer"}},
				"idempotency_key":         {Type: "string", Description: "幂等键，适合可重试的创建类请求。"},
				"max_response_body_bytes": {Type: "integer", Description: "单次请求允许读取的最大响应体字节数。省略则使用平台默认值。"},
				"max_request_body_bytes":  {Type: "integer", Description: "单次请求允许发送的最大请求体字节数。multipart 省略时默认限制 4MiB。"},
				"multipart":               {Type: "object", Description: "multipart/form-data 上传配置。fields 为普通字段，files 为受控工作区内文件列表。"},
				"download":                {Type: "object", Description: "受控下载配置。save_as 为工作区内保存路径，max_bytes 为本次下载大小上限。"},
				"auth":                    {Type: "object", Description: "直接请求认证配置，支持 none、api_key_header、api_key_query、bearer_token、basic_auth、custom_header。使用 connection_ref 时通常不需要传入。"},
			},
		},
	}
}

func (t *HTTPRequestTool) Execute(ctx context.Context, params string) (string, error) {
	if t.client == nil {
		return "", fmt.Errorf("HTTP 请求工具未初始化 client")
	}

	var input httpRequestInput
	if err := json.Unmarshal([]byte(params), &input); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}

	req, err := t.buildHTTPRequestInput(ctx, &input)
	if err != nil {
		return "", err
	}

	resp, err := t.client.Do(ctx, req)
	if err != nil {
		return "", err
	}

	result := httpRequestResult{
		StatusCode:  resp.StatusCode,
		Headers:     map[string][]string(resp.Headers),
		ContentType: resp.Headers.Get("Content-Type"),
		RequestID:   firstHeader(resp.Headers, "X-Request-ID", "x-request-id"),
	}

	if input.Download != nil {
		downloaded, err := saveDownloadedBody(input.Download, resp.Body)
		if err != nil {
			return "", err
		}
		result.Download = downloaded
	} else {
		bodyText := string(resp.Body)
		if bodyText != "" {
			result.BodyText = bodyText
		}
		if len(resp.Body) > 0 && looksLikeJSON(resp.Headers.Get("Content-Type"), resp.Body) {
			var payload any
			if err := json.Unmarshal(resp.Body, &payload); err == nil {
				result.BodyJSON = payload
			}
		}
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("结果序列化失败: %w", err)
	}
	return string(output), nil
}

func (t *HTTPRequestTool) buildHTTPRequestInput(ctx context.Context, input *httpRequestInput) (*platformhttp.Request, error) {
	if input == nil {
		return nil, fmt.Errorf("请求参数不能为空")
	}
	if input.Multipart != nil && input.Body != nil {
		return nil, fmt.Errorf("body 与 multipart 不能同时使用")
	}

	resolved, err := t.resolveConnection(ctx, input)
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimSpace(input.BaseURL)
	path := strings.TrimSpace(input.Path)
	if rawURL := strings.TrimSpace(input.URL); rawURL != "" {
		baseURL = rawURL
		path = ""
	}
	if baseURL == "" && resolved == nil {
		return nil, fmt.Errorf("url、base_url 或 connection_ref 不能为空")
	}

	query, err := buildQueryValues(input.Query)
	if err != nil {
		return nil, fmt.Errorf("query 参数无效: %w", err)
	}
	headers, err := buildHeaders(input.Headers)
	if err != nil {
		return nil, fmt.Errorf("headers 参数无效: %w", err)
	}
	auth, err := buildAuthConfig(input.Auth)
	if err != nil {
		return nil, fmt.Errorf("auth 参数无效: %w", err)
	}

	body := input.Body
	contentType := strings.TrimSpace(input.ContentType)
	maxRequestBodyBytes := input.MaxRequestBodyBytes
	if input.Multipart != nil {
		if maxRequestBodyBytes <= 0 {
			maxRequestBodyBytes = defaultHTTPToolMaxMultipartBytes
		}
		multipartBody, multipartContentType, err := buildMultipartBody(input.Multipart, maxRequestBodyBytes)
		if err != nil {
			return nil, err
		}
		body = multipartBody
		contentType = multipartContentType
	}

	timeout := time.Duration(0)
	if input.TimeoutMS > 0 {
		timeout = time.Duration(input.TimeoutMS) * time.Millisecond
	}
	maxResponseBodyBytes := input.MaxResponseBodyBytes
	if input.Download != nil && input.Download.MaxBytes > 0 {
		maxResponseBodyBytes = input.Download.MaxBytes
	}

	req := &platformhttp.Request{
		Method:               strings.ToUpper(strings.TrimSpace(defaultHTTPMethod(input.Method))),
		BaseURL:              baseURL,
		Path:                 path,
		Query:                query,
		Headers:              headers,
		Body:                 body,
		ContentType:          contentType,
		Auth:                 auth,
		Timeout:              timeout,
		ExpectedStatus:       input.ExpectedStatus,
		IdempotencyKey:       strings.TrimSpace(input.IdempotencyKey),
		MaxRequestBodyBytes:  maxRequestBodyBytes,
		MaxResponseBodyBytes: maxResponseBodyBytes,
	}
	applyResolvedHTTPConnection(req, resolved)
	return req, nil
}

func (t *HTTPRequestTool) resolveConnection(ctx context.Context, input *httpRequestInput) (*connection.ResolvedHTTPConnection, error) {
	connectionRef := strings.TrimSpace(input.ConnectionRef)
	if connectionRef == "" {
		return nil, nil
	}
	if t.resolver == nil {
		return nil, fmt.Errorf("connection_ref 已提供，但 connection resolver 未初始化")
	}
	if strings.TrimSpace(input.URL) != "" || strings.TrimSpace(input.BaseURL) != "" {
		return nil, fmt.Errorf("使用 connection_ref 时不允许同时传入 url 或 base_url")
	}
	return t.resolver.ResolveForHTTP(ctx, connection.HTTPResolveRequest{
		TenantID:      defaultTenantID(input.TenantID),
		ConnectionRef: connectionRef,
		ToolName:      "http_request",
		Operation:     strings.ToUpper(strings.TrimSpace(defaultHTTPMethod(input.Method))),
	})
}

func buildMultipartBody(input *multipartInput, maxBytes int64) ([]byte, string, error) {
	if input == nil {
		return nil, "", nil
	}
	buf := &limitedBuffer{maxBytes: maxBytes}
	writer := multipart.NewWriter(buf)
	for key, raw := range input.Fields {
		values, err := normalizeValues(raw)
		if err != nil {
			_ = writer.Close()
			return nil, "", fmt.Errorf("multipart.fields.%s 无效: %w", key, err)
		}
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				_ = writer.Close()
				return nil, "", fmt.Errorf("写入 multipart 字段失败: %w", err)
			}
		}
	}
	for _, file := range input.Files {
		if err := appendMultipartFile(writer, file); err != nil {
			_ = writer.Close()
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}
	return buf.Bytes(), writer.FormDataContentType(), nil
}

type limitedBuffer struct {
	buf      bytes.Buffer
	maxBytes int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.maxBytes > 0 && int64(b.buf.Len()+len(p)) > b.maxBytes {
		return 0, fmt.Errorf("multipart 请求体超过限制")
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func appendMultipartFile(writer *multipart.Writer, input multipartFileInput) error {
	field := strings.TrimSpace(input.Field)
	if field == "" {
		return fmt.Errorf("multipart file field 不能为空")
	}
	path, err := resolveHTTPToolReadPath(input.Path)
	if err != nil {
		return fmt.Errorf("multipart file path 无效: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开上传文件失败: %w", err)
	}
	defer file.Close()

	filename := strings.TrimSpace(input.Filename)
	if filename == "" {
		filename = filepath.Base(path)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     field,
		"filename": filename,
	}))
	if strings.TrimSpace(input.ContentType) != "" {
		header.Set("Content-Type", strings.TrimSpace(input.ContentType))
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("创建 multipart 文件字段失败: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("写入 multipart 文件失败: %w", err)
	}
	return nil
}

func saveDownloadedBody(input *downloadInput, body []byte) (*downloadResult, error) {
	if input == nil {
		return nil, nil
	}
	path, err := resolveHTTPToolWritePath(input.SaveAs)
	if err != nil {
		return nil, fmt.Errorf("download.save_as 无效: %w", err)
	}
	if input.MaxBytes > 0 && int64(len(body)) > input.MaxBytes {
		return nil, fmt.Errorf("下载内容超过限制")
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		return nil, fmt.Errorf("写入下载文件失败: %w", err)
	}
	return &downloadResult{SavedAs: path, Bytes: len(body)}, nil
}

func resolveHTTPToolPath(rawPath string) (string, error) {
	return resolveHTTPToolPathLexical(rawPath)
}

func resolveHTTPToolReadPath(rawPath string) (string, error) {
	rootAbs, candidateAbs, err := resolveHTTPToolPathParts(rawPath)
	if err != nil {
		return "", err
	}
	resolved := candidateAbs
	if info, err := os.Lstat(candidateAbs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err = filepath.EvalSymlinks(candidateAbs)
		if err != nil {
			return "", fmt.Errorf("解析真实路径失败: %w", err)
		}
		resolved = filepath.Clean(resolved)
		if err := ensurePathInside(rootAbs, resolved); err != nil {
			return "", err
		}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("读取文件信息失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("只能读取普通文件")
	}
	return resolved, nil
}

func resolveHTTPToolWritePath(rawPath string) (string, error) {
	rootAbs, candidateAbs, err := resolveHTTPToolPathParts(rawPath)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(candidateAbs)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}
	parentReal := parent
	if info, err := os.Lstat(parent); err == nil && info.Mode()&os.ModeSymlink != 0 {
		parentReal, err = filepath.EvalSymlinks(parent)
		if err != nil {
			return "", fmt.Errorf("解析目录真实路径失败: %w", err)
		}
		parentReal = filepath.Clean(parentReal)
		if err := ensurePathInside(rootAbs, parentReal); err != nil {
			return "", err
		}
	}
	finalPath := filepath.Join(parentReal, filepath.Base(candidateAbs))
	if info, err := os.Lstat(finalPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("拒绝写入符号链接")
		}
		if info.IsDir() {
			return "", fmt.Errorf("下载目标不能是目录")
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("读取目标文件信息失败: %w", err)
	}
	if err := ensurePathInside(rootAbs, finalPath); err != nil {
		return "", err
	}
	return finalPath, nil
}

func resolveHTTPToolPathLexical(rawPath string) (string, error) {
	_, candidateAbs, err := resolveHTTPToolPathParts(rawPath)
	if err != nil {
		return "", err
	}
	return candidateAbs, nil
}

func resolveHTTPToolPathParts(rawPath string) (string, string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", "", fmt.Errorf("路径不能为空")
	}
	rootAbs, err := resolveHTTPToolRoot()
	if err != nil {
		return "", "", err
	}
	candidate := trimmed
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootAbs, candidate)
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", fmt.Errorf("解析路径失败: %w", err)
	}
	candidateAbs = filepath.Clean(candidateAbs)
	if err := ensurePathInside(rootAbs, candidateAbs); err != nil {
		return "", "", err
	}
	return rootAbs, candidateAbs, nil
}

func resolveHTTPToolRoot() (string, error) {
	root := strings.TrimSpace(os.Getenv(httpToolWorkspaceEnv))
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("读取工作目录失败: %w", err)
		}
		root = cwd
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("解析工作区失败: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	if err := os.MkdirAll(rootAbs, 0700); err != nil {
		return "", fmt.Errorf("创建工作区失败: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return rootAbs, nil
	}
	return filepath.Clean(resolved), nil
}

func ensurePathInside(rootAbs string, candidateAbs string) error {
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return fmt.Errorf("校验路径失败: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("路径必须位于工作区内")
	}
	return nil
}

func applyResolvedHTTPConnection(req *platformhttp.Request, resolved *connection.ResolvedHTTPConnection) {
	if resolved == nil {
		return
	}
	req.BaseURL = resolved.BaseURL
	req.Headers = mergeHeaders(resolved.Headers, req.Headers)
	if req.Auth == nil {
		req.Auth = resolved.Auth
	}
	if req.Timeout <= 0 {
		req.Timeout = resolved.Timeout
	}
	if req.Retry == nil {
		req.Retry = resolved.Retry
	}
}

func buildQueryValues(values map[string]any) (url.Values, error) {
	if len(values) == 0 {
		return nil, nil
	}
	query := make(url.Values, len(values))
	for key, raw := range values {
		items, err := normalizeValues(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		for _, item := range items {
			query.Add(key, item)
		}
	}
	return query, nil
}

func buildHeaders(values map[string]any) (http.Header, error) {
	if len(values) == 0 {
		return nil, nil
	}
	headers := make(http.Header, len(values))
	for key, raw := range values {
		items, err := normalizeValues(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		for _, item := range items {
			headers.Add(key, item)
		}
	}
	return headers, nil
}

func normalizeValues(raw any) ([]string, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{value}, nil
	case bool:
		return []string{strconv.FormatBool(value)}, nil
	case float64:
		return []string{strconv.FormatFloat(value, 'f', -1, 64)}, nil
	case json.Number:
		return []string{value.String()}, nil
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			nested, err := normalizeValues(item)
			if err != nil {
				return nil, err
			}
			items = append(items, nested...)
		}
		return items, nil
	case []string:
		return value, nil
	default:
		return nil, fmt.Errorf("仅支持 string、number、bool 或数组")
	}
}

func buildAuthConfig(input *httpRequestAuthInput) (*platformhttp.AuthConfig, error) {
	if input == nil || strings.TrimSpace(input.Type) == "" || strings.EqualFold(input.Type, "none") {
		return nil, nil
	}
	return &platformhttp.AuthConfig{
		Type:       platformhttp.AuthType(strings.ToLower(strings.TrimSpace(input.Type))),
		HeaderName: strings.TrimSpace(input.HeaderName),
		QueryName:  strings.TrimSpace(input.QueryName),
		Username:   input.Username,
		Password:   input.Password,
		Token:      input.Token,
		Value:      input.Value,
	}, nil
}

func mergeHeaders(base http.Header, override http.Header) http.Header {
	if len(base) == 0 {
		return cloneHTTPHeader(override)
	}
	out := cloneHTTPHeader(base)
	for key, values := range override {
		out.Del(key)
		for _, value := range values {
			out.Add(key, value)
		}
	}
	return out
}

func cloneHTTPHeader(input http.Header) http.Header {
	if len(input) == 0 {
		return nil
	}
	out := make(http.Header, len(input))
	for key, values := range input {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func defaultHTTPMethod(method string) string {
	if strings.TrimSpace(method) == "" {
		return http.MethodGet
	}
	return method
}

func defaultTenantID(tenantID string) string {
	if strings.TrimSpace(tenantID) == "" {
		return "dev"
	}
	return strings.TrimSpace(tenantID)
}

func firstHeader(header http.Header, keys ...string) string {
	for _, key := range keys {
		if value := header.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func looksLikeJSON(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "json") {
		return true
	}
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
