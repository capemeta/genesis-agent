package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type session struct {
	name    string
	sdk     *mcp.ClientSession
	dialed  contract.DialedSession
	timeout model.McpServerConfig
}

func newSession(name string, dialed contract.DialedSession, cfg model.McpServerConfig) (*session, error) {
	sdk, ok := dialed.Underlying().(*mcp.ClientSession)
	if !ok || sdk == nil {
		_ = dialed.Close()
		return nil, fmt.Errorf("mcp session %q: 无效的底层会话", name)
	}
	return &session{name: name, sdk: sdk, dialed: dialed, timeout: cfg}, nil
}

func (s *session) Name() string { return s.name }

func (s *session) ListTools(ctx context.Context) ([]model.ToolSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	res, err := s.sdk.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q tools/list 失败: %w", s.name, err)
	}
	out := make([]model.ToolSnapshot, 0, len(res.Tools))
	for _, t := range res.Tools {
		if t == nil || strings.TrimSpace(t.Name) == "" {
			continue
		}
		snap := model.ToolSnapshot{
			Name:        t.Name,
			Description: t.Description,
		}
		if t.InputSchema != nil {
			raw, err := json.Marshal(t.InputSchema)
			if err == nil {
				snap.InputSchema = raw
			}
		}
		if t.Annotations != nil && t.Annotations.ReadOnlyHint {
			v := true
			snap.ReadOnlyHint = &v
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *session) CallTool(ctx context.Context, toolName string, args json.RawMessage) (model.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return model.ToolResult{}, err
	}
	var arguments any
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return model.ToolResult{}, fmt.Errorf("mcp tool %s/%s 参数不是合法 JSON: %w", s.name, toolName, err)
		}
	}
	res, err := s.sdk.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: arguments})
	if err != nil {
		return model.ToolResult{}, fmt.Errorf("mcp tool %s/%s 调用失败: %w", s.name, toolName, err)
	}
	content := normalizeContent(res)
	raw, _ := json.Marshal(res)
	return model.ToolResult{Content: content, IsError: res.IsError, RawJSON: raw}, nil
}

func (s *session) ListResources(ctx context.Context) ([]model.ResourceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	res, err := s.sdk.ListResources(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q resources/list 失败: %w", s.name, err)
	}
	out := make([]model.ResourceSnapshot, 0, len(res.Resources))
	for _, r := range res.Resources {
		if r == nil {
			continue
		}
		out = append(out, model.ResourceSnapshot{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MIMEType:    r.MIMEType,
		})
	}
	return out, nil
}

func (s *session) ReadResource(ctx context.Context, uri string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	res, err := s.sdk.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		return "", fmt.Errorf("mcp server %q resources/read 失败: %w", s.name, err)
	}
	parts := make([]string, 0, len(res.Contents))
	for _, c := range res.Contents {
		if c == nil {
			continue
		}
		if c.Text != "" {
			parts = append(parts, c.Text)
			continue
		}
		if len(c.Blob) > 0 {
			parts = append(parts, fmt.Sprintf("[blob mime=%s bytes=%d]", c.MIMEType, len(c.Blob)))
			continue
		}
		raw, _ := json.Marshal(c)
		parts = append(parts, string(raw))
	}
	return strings.Join(parts, "\n"), nil
}

func (s *session) Ping(ctx context.Context) error {
	return s.sdk.Ping(ctx, nil)
}

func (s *session) Close(ctx context.Context) error {
	_ = ctx
	if s == nil || s.dialed == nil {
		return nil
	}
	return s.dialed.Close()
}

func normalizeContent(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	parts := make([]string, 0, len(res.Content))
	for _, c := range res.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			parts = append(parts, v.Text)
		case *mcp.ImageContent:
			parts = append(parts, fmt.Sprintf("[image mime=%s]", v.MIMEType))
		case *mcp.AudioContent:
			parts = append(parts, fmt.Sprintf("[audio mime=%s]", v.MIMEType))
		default:
			raw, err := json.Marshal(c)
			if err != nil {
				parts = append(parts, fmt.Sprintf("%v", c))
			} else {
				parts = append(parts, string(raw))
			}
		}
	}
	if len(parts) == 0 && res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err == nil {
			return string(raw)
		}
	}
	return strings.Join(parts, "\n")
}

var _ contract.Session = (*session)(nil)
