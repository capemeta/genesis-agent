package web_search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"genesis-agent/internal/capabilities/tool/contract"
	webcontract "genesis-agent/internal/capabilities/web/contract"
)

type Tool struct {
	svc webcontract.Searcher
}

type searchInput struct {
	Query          string   `json:"query"`
	Limit          int      `json:"limit,omitempty"`
	RecencyDays    int      `json:"recency_days,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
	Locale         string   `json:"locale,omitempty"`
	Region         string   `json:"region,omitempty"`
	SafeSearch     string   `json:"safe_search,omitempty"`
}

func New(svc webcontract.Searcher) (*Tool, error) {
	if svc == nil {
		return nil, errors.New("search service cannot be nil")
	}
	return &Tool{svc: svc}, nil
}

func (t *Tool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "web_search",
		Description: "在互联网或特定域名中搜索相关网页。支持限制结果条数、时间范围，以及指定域名过滤。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"query":           {Type: "string", Description: "搜索关键字或查询内容"},
				"limit":           {Type: "integer", Description: "返回的搜索结果最大条数，默认 5，最大 20"},
				"recency_days":    {Type: "integer", Description: "只检索最近 N 天内的网页内容"},
				"allowed_domains": {Type: "array", Items: &tool.ParameterSchema{Type: "string"}, Description: "只允许搜索指定的域名"},
				"blocked_domains": {Type: "array", Items: &tool.ParameterSchema{Type: "string"}, Description: "禁止搜索指定的域名"},
				"locale":          {Type: "string", Description: "地区/语言代码，如 zh-CN, en-US"},
				"region":          {Type: "string", Description: "地区代码，如 CN, US"},
				"safe_search":     {Type: "string", Enum: []string{"off", "moderate", "strict"}, Description: "安全搜索级别，可选 off, moderate, strict"},
			},
			Required: []string{"query"},
		},
	}
}

func (t *Tool) Execute(ctx context.Context, params string) (string, error) {
	var in searchInput
	if err := decodeParams(params, &in); err != nil {
		return "", err
	}

	req := webcontract.SearchRequest{
		Query:          in.Query,
		Limit:          in.Limit,
		RecencyDays:    in.RecencyDays,
		AllowedDomains: in.AllowedDomains,
		BlockedDomains: in.BlockedDomains,
		Locale:         in.Locale,
		Region:         in.Region,
		SafeSearch:     in.SafeSearch,
		Mode:           webcontract.SearchModeLive,
	}

	res, err := t.svc.Search(ctx, req)
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("failed to serialize search results: %w", err)
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
