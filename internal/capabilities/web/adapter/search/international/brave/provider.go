package brave

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/web/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

type Provider struct {
	client  platformhttp.Client
	apiKey  string
	baseURL string
}

type BraveWebResponse struct {
	Web struct {
		Results []BraveResultItem `json:"results"`
	} `json:"web"`
}

type BraveResultItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	PageAge     string `json:"page_age,omitempty"`
}

func NewProvider(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.search.brave.com/res/v1/web/search"
	}
	return &Provider{
		client:  platformhttp.New(),
		apiKey:  apiKey,
		baseURL: baseURL,
	}
}

func (p *Provider) Metadata() contract.ProviderMetadata {
	return contract.ProviderMetadata{
		ID:                 "brave",
		Name:               "Brave Web Search API",
		Description:        "Brave Search API provides private, clean and fast search results.",
		SupportedRegions:   []string{"US", "GLOBAL"},
		SupportedLanguages: []string{"en"},
		IsChinaCompliance:  false,
		IsSelfHosted:       false,
	}
}

func (p *Provider) ValidateCredentials(ctx context.Context, creds map[string]string) (bool, error) {
	key := creds[contract.CredentialKeyBraveSearchAPIKey]
	if key == "" {
		return false, fmt.Errorf("brave_search_api_key is empty")
	}
	return true, nil
}

func (p *Provider) HealthCheck(ctx context.Context) (bool, error) {
	if p.apiKey == "" {
		return false, fmt.Errorf("api key not set")
	}
	return true, nil
}

func (p *Provider) Search(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, error) {
	apiKey := p.apiKey
	if apiKey == "" && req.Credentials != nil {
		apiKey = req.Credentials[contract.CredentialKeyBraveSearchAPIKey]
	}
	if apiKey == "" {
		return contract.SearchResult{}, fmt.Errorf("Brave API Key is not configured")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	u, err := url.Parse(p.baseURL)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("invalid Brave baseURL: %w", err)
	}

	q := u.Query()
	q.Set("q", req.Query)
	q.Set("count", strconv.Itoa(limit))
	if req.Locale != "" {
		q.Set("country", req.Locale)
	}
	if req.SafeSearch != "" {
		q.Set("safesearch", req.SafeSearch)
	}
	u.RawQuery = q.Encode()

	platReq := &platformhttp.Request{
		Method:  "GET",
		BaseURL: u.String(),
		Headers: http.Header{
			"X-Subscription-Token": []string{apiKey},
			"Accept":               []string{"application/json"},
		},
	}

	platResp, err := p.client.Do(ctx, platReq)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("Brave Search request failed: %w", err)
	}

	var braveResp BraveWebResponse
	if err := json.Unmarshal(platResp.Body, &braveResp); err != nil {
		return contract.SearchResult{}, fmt.Errorf("failed to decode Brave search response: %w", err)
	}

	var hits []contract.SearchHit
	for i, item := range braveResp.Web.Results {
		hit := contract.SearchHit{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Description,
			Source:  extractSource(item.URL),
			Rank:    i + 1,
		}
		if item.PageAge != "" {
			if t, err := time.Parse(time.RFC3339, item.PageAge); err == nil {
				hit.PublishedAt = &t
			}
		}
		hits = append(hits, hit)
	}

	return contract.SearchResult{
		Query:    req.Query,
		Provider: "brave",
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
