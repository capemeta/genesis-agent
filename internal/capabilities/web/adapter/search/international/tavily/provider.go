package tavily

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"genesis-agent/internal/capabilities/web/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

type Provider struct {
	client  platformhttp.Client
	apiKey  string
	baseURL string
}

type TavilyResultItem struct {
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Content      string  `json:"content"`
	Score        float64 `json:"score"`
	PublishedDate string `json:"published_date,omitempty"`
}

type TavilyResponse struct {
	Results []TavilyResultItem `json:"results"`
}

func NewProvider(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.tavily.com/search"
	}
	return &Provider{
		client:  platformhttp.New(),
		apiKey:  apiKey,
		baseURL: baseURL,
	}
}

func (p *Provider) Metadata() contract.ProviderMetadata {
	return contract.ProviderMetadata{
		ID:                 "tavily",
		Name:               "Tavily Search API",
		Description:        "Tavily Search API optimizes search specifically for LLMs and RAG applications.",
		SupportedRegions:   []string{"US", "GLOBAL"},
		SupportedLanguages: []string{"en", "zh"},
		IsChinaCompliance:  false,
		IsSelfHosted:       false,
	}
}

func (p *Provider) ValidateCredentials(ctx context.Context, creds map[string]string) (bool, error) {
	key := creds[contract.CredentialKeyTavilyAPIKey]
	if key == "" {
		return false, fmt.Errorf("tavily_api_key is empty")
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
		apiKey = req.Credentials[contract.CredentialKeyTavilyAPIKey]
	}
	if apiKey == "" {
		return contract.SearchResult{}, fmt.Errorf("Tavily API Key is not configured")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	bodyParams := map[string]interface{}{
		"api_key":      apiKey,
		"query":        req.Query,
		"max_results":  limit,
		"search_depth": "basic",
	}

	if len(req.AllowedDomains) > 0 {
		bodyParams["include_domains"] = req.AllowedDomains
	}
	if len(req.BlockedDomains) > 0 {
		bodyParams["exclude_domains"] = req.BlockedDomains
	}
	if req.RecencyDays > 0 {
		// Tavily supports time_range filter internally
		if req.RecencyDays <= 1 {
			bodyParams["time_range"] = "day"
		} else if req.RecencyDays <= 7 {
			bodyParams["time_range"] = "week"
		} else if req.RecencyDays <= 30 {
			bodyParams["time_range"] = "month"
		} else {
			bodyParams["time_range"] = "year"
		}
	}

	bodyBytes, err := json.Marshal(bodyParams)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("failed to encode Tavily request body: %w", err)
	}

	platReq := &platformhttp.Request{
		Method:  "POST",
		BaseURL: p.baseURL,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
			"Accept":       []string{"application/json"},
		},
		Body: bodyBytes,
	}

	platResp, err := p.client.Do(ctx, platReq)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("Tavily Search request failed: %w", err)
	}

	if platResp.StatusCode != http.StatusOK {
		return contract.SearchResult{}, fmt.Errorf("Tavily Search request failed with status: %d, body: %s", platResp.StatusCode, string(platResp.Body))
	}

	var tavilyResp TavilyResponse
	if err := json.Unmarshal(platResp.Body, &tavilyResp); err != nil {
		return contract.SearchResult{}, fmt.Errorf("failed to decode Tavily search response: %w", err)
	}

	var hits []contract.SearchHit
	for i, item := range tavilyResp.Results {
		hit := contract.SearchHit{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Content,
			Source:  "tavily",
			Rank:    i + 1,
		}
		if item.PublishedDate != "" {
			if parsedTime, err := time.Parse("2006-01-02", item.PublishedDate); err == nil {
				hit.PublishedAt = &parsedTime
			} else if parsedTime, err := time.Parse(time.RFC3339, item.PublishedDate); err == nil {
				hit.PublishedAt = &parsedTime
			}
		}
		hits = append(hits, hit)
	}

	return contract.SearchResult{
		Query:    req.Query,
		Provider: "tavily",
		Results:  hits,
		Duration: time.Since(start),
	}, nil
}
