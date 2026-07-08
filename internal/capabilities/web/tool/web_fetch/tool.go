package web_fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/tool/contract"
	webcontract "genesis-agent/internal/capabilities/web/contract"
)

type Tool struct {
	svc webcontract.FetchService
}

type fetchInput struct {
	URL             string   `json:"url"`
	Prompt          string   `json:"prompt,omitempty"`
	Format          string   `json:"format,omitempty"`
	MaxBytes        int64    `json:"max_bytes,omitempty"`
	MaxChars        int      `json:"max_chars,omitempty"`
	AllowedDomains  []string `json:"allowed_domains,omitempty"`
	BlockedDomains  []string `json:"blocked_domains,omitempty"`
	FollowRedirects bool     `json:"follow_redirects,omitempty"`
	RenderMode      string   `json:"render_mode,omitempty"`
}

func New(svc webcontract.FetchService) (*Tool, error) {
	if svc == nil {
		return nil, errors.New("fetch service cannot be nil")
	}
	return &Tool{svc: svc}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "web_fetch",
		Description: "获取公开网页内容，将 HTML 正文抽取并转换为 Markdown。会自动拦截私有 IP 以防 SSRF。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"url":              {Type: "string", Description: "要抓取的 HTTP/HTTPS 网页完整 URL"},
				"prompt":           {Type: "string", Description: "抽取信息的额外提示词（可选）"},
				"format":           {Type: "string", Enum: []string{"markdown", "text", "html", "metadata"}, Description: "返回结果格式，默认 markdown"},
				"max_bytes":        {Type: "integer", Description: "最大读取响应体字节数，默认 4MB"},
				"max_chars":        {Type: "integer", Description: "HTML 转 Markdown 后限制的最大字符长度"},
				"allowed_domains":  {Type: "array", Items: &tool.ParameterSchema{Type: "string"}, Description: "允许抓取的域名列表"},
				"blocked_domains":  {Type: "array", Items: &tool.ParameterSchema{Type: "string"}, Description: "禁止抓取的域名列表"},
				"follow_redirects": {Type: "boolean", Description: "是否跟随跨域名重定向，默认为 false"},
				"render_mode":      {Type: "string", Enum: []string{"http", "browser", "sandbox"}, Description: "抓取渲染模式，当前第一阶段默认 http"},
			},
			Required: []string{"url"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in fetchInput
	if err := decodeParams(params, &in); err != nil {
		return "", err
	}

	format := webcontract.FetchFormatMarkdown
	switch strings.ToLower(in.Format) {
	case "text":
		format = webcontract.FetchFormatText
	case "html":
		format = webcontract.FetchFormatHTML
	case "metadata":
		format = webcontract.FetchFormatMetadata
	}

	renderMode := webcontract.RenderModeHTTP
	switch strings.ToLower(in.RenderMode) {
	case "browser":
		renderMode = webcontract.RenderModeBrowser
	case "sandbox":
		renderMode = webcontract.RenderModeSandbox
	}

	maxBytes := in.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 4 << 20 // 4MB
	}

	maxChars := in.MaxChars
	if maxChars <= 0 {
		maxChars = 100000 // default 100k chars limit
	}

	req := webcontract.FetchRequest{
		URL:             in.URL,
		Prompt:          in.Prompt,
		Format:          format,
		MaxBytes:        maxBytes,
		MaxChars:        maxChars,
		Timeout:         30 * time.Second,
		AllowedDomains:  in.AllowedDomains,
		BlockedDomains:  in.BlockedDomains,
		FollowRedirects: in.FollowRedirects,
		RenderMode:      renderMode,
	}

	// Wait, the search or fetcher interface implemented by the fetch service:
	// We pass a fetcher which is FetchService (it implements Fetcher)
	res, err := t.svc.Fetch(ctx, req)
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("failed to serialize fetch results: %w", err)
	}

	return string(data), nil
}

func decodeParams(params string, dst any) error {
	decoder := json.NewDecoder(strings.NewReader(params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid parameters: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("parameters must contain exactly one JSON object")
	}
	return nil
}
