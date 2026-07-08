package searxng

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"genesis-agent/internal/capabilities/web/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

type Provider struct {
	client  platformhttp.Client
	baseURL string
}

type SearXNGResponse struct {
	Results []SearXNGResultItem `json:"results"`
}

type SearXNGResultItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Engine  string `json:"engine,omitempty"`
}

func NewProvider(baseURL string) *Provider {
	return &Provider{
		client:  platformhttp.New(),
		baseURL: baseURL,
	}
}

func (p *Provider) Metadata() contract.ProviderMetadata {
	return contract.ProviderMetadata{
		ID:                 "searxng",
		Name:               "SearXNG Search",
		Description:        "Self-hosted metasearch engine proxying multiple search providers.",
		SupportedRegions:   []string{"GLOBAL"},
		SupportedLanguages: []string{"en", "zh"},
		IsChinaCompliance:  true, // Depends on hosting environment, but generally compliant for private self-hosted
		IsSelfHosted:       true,
	}
}

func (p *Provider) ValidateCredentials(ctx context.Context, creds map[string]string) (bool, error) {
	urlStr := creds[contract.CredentialKeySearXNGBaseURL]
	if urlStr == "" {
		return false, fmt.Errorf("searxng_base_url is empty")
	}
	_, err := url.Parse(urlStr)
	if err != nil {
		return false, fmt.Errorf("invalid searxng_base_url: %w", err)
	}
	return true, nil
}

func (p *Provider) HealthCheck(ctx context.Context) (bool, error) {
	if p.baseURL == "" {
		return false, fmt.Errorf("SearXNG baseURL is not configured")
	}
	return true, nil
}

func (p *Provider) Search(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, error) {
	baseURL := p.baseURL
	if baseURL == "" && req.Credentials != nil {
		baseURL = req.Credentials[contract.CredentialKeySearXNGBaseURL]
	}
	if baseURL == "" {
		return contract.SearchResult{}, fmt.Errorf("SearXNG baseURL is not configured")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("invalid SearXNG baseURL: %w", err)
	}

	q := u.Query()
	q.Set("q", req.Query)
	q.Set("format", "json")
	q.Set("categories", "general")
	if req.Locale != "" {
		q.Set("locale", req.Locale)
	}
	if req.RecencyDays > 0 {
		// SearXNG supports time_range: day, week, month, year
		if req.RecencyDays <= 1 {
			q.Set("time_range", "day")
		} else if req.RecencyDays <= 7 {
			q.Set("time_range", "week")
		} else if req.RecencyDays <= 30 {
			q.Set("time_range", "month")
		} else {
			q.Set("time_range", "year")
		}
	}
	u.RawQuery = q.Encode()

	platReq := &platformhttp.Request{
		Method:  "GET",
		BaseURL: u.String(),
	}

	platResp, err := p.client.Do(ctx, platReq)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("SearXNG request failed: %w", err)
	}

	var searxResp SearXNGResponse
	if err := json.Unmarshal(platResp.Body, &searxResp); err != nil {
		return contract.SearchResult{}, fmt.Errorf("failed to decode SearXNG search response: %w", err)
	}

	var hits []contract.SearchHit
	for i, item := range searxResp.Results {
		hit := contract.SearchHit{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Content,
			Source:  extractSource(item.URL),
			Rank:    i + 1,
		}
		hits = append(hits, hit)
	}

	return contract.SearchResult{
		Query:    req.Query,
		Provider: "searxng",
		Results:  hits,
	}, nil
}

func extractSource(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
