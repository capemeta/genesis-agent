package serpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"genesis-agent/internal/capabilities/web/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

type Provider struct {
	client  platformhttp.Client
	apiKey  string
	baseURL string
}

type SerpAPIResultItem struct {
	Title    string `json:"title"`
	Link     string `json:"link"`
	Snippet  string `json:"snippet"`
	Position int    `json:"position"`
}

type SerpAPIResponse struct {
	OrganicResults []SerpAPIResultItem `json:"organic_results"`
}

func NewProvider(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://serpapi.com/search"
	}
	return &Provider{
		client:  platformhttp.New(),
		apiKey:  apiKey,
		baseURL: baseURL,
	}
}

func (p *Provider) Metadata() contract.ProviderMetadata {
	return contract.ProviderMetadata{
		ID:                 "serpapi",
		Name:               "SerpAPI Search Engine",
		Description:        "SerpAPI wraps multiple search engines (Google, Bing, Yahoo) into a clean API response.",
		SupportedRegions:   []string{"US", "GLOBAL"},
		SupportedLanguages: []string{"en", "zh"},
		IsChinaCompliance:  false,
		IsSelfHosted:       false,
	}
}

func (p *Provider) ValidateCredentials(ctx context.Context, creds map[string]string) (bool, error) {
	key := creds[contract.CredentialKeySerpAPIKey]
	if key == "" {
		return false, fmt.Errorf("serpapi_api_key is empty")
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
	start := time.Now()
	apiKey := p.apiKey
	if apiKey == "" && req.Credentials != nil {
		apiKey = req.Credentials[contract.CredentialKeySerpAPIKey]
	}
	if apiKey == "" {
		return contract.SearchResult{}, fmt.Errorf("SerpAPI API Key is not configured")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	u, err := url.Parse(p.baseURL)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("invalid SerpAPI baseURL: %w", err)
	}

	q := u.Query()
	q.Set("api_key", apiKey)
	q.Set("q", req.Query)
	q.Set("engine", "google")
	q.Set("num", strconv.Itoa(limit))
	if req.Locale != "" {
		q.Set("hl", req.Locale)
	}
	if req.Region != "" {
		q.Set("gl", req.Region)
	}

	u.RawQuery = q.Encode()

	platReq := &platformhttp.Request{
		Method:  "GET",
		BaseURL: u.String(),
	}

	platResp, err := p.client.Do(ctx, platReq)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("SerpAPI Search request failed: %w", err)
	}

	var serpResp SerpAPIResponse
	if err := json.Unmarshal(platResp.Body, &serpResp); err != nil {
		return contract.SearchResult{}, fmt.Errorf("failed to decode SerpAPI search response: %w", err)
	}

	var hits []contract.SearchHit
	for _, item := range serpResp.OrganicResults {
		hit := contract.SearchHit{
			Title:   item.Title,
			URL:     item.Link,
			Snippet: item.Snippet,
			Source:  "google",
			Rank:    item.Position,
		}
		hits = append(hits, hit)
	}

	return contract.SearchResult{
		Query:    req.Query,
		Provider: "serpapi",
		Results:  hits,
		Duration: time.Since(start),
	}, nil
}
