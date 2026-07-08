package exa

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

type ExaResultItem struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Score         float64 `json:"score"`
	PublishedDate string  `json:"publishedDate,omitempty"`
	Author        string  `json:"author,omitempty"`
}

type ExaResponse struct {
	Results []ExaResultItem `json:"results"`
}

func NewProvider(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.exa.ai/search"
	}
	return &Provider{
		client:  platformhttp.New(),
		apiKey:  apiKey,
		baseURL: baseURL,
	}
}

func (p *Provider) Metadata() contract.ProviderMetadata {
	return contract.ProviderMetadata{
		ID:                 "exa",
		Name:               "Exa Search API",
		Description:        "Exa (Metaphor) Search API provides neural search specifically optimized for LLMs.",
		SupportedRegions:   []string{"US", "GLOBAL"},
		SupportedLanguages: []string{"en"},
		IsChinaCompliance:  false,
		IsSelfHosted:       false,
	}
}

func (p *Provider) ValidateCredentials(ctx context.Context, creds map[string]string) (bool, error) {
	key := creds[contract.CredentialKeyExaAPIKey]
	if key == "" {
		return false, fmt.Errorf("exa_api_key is empty")
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
		apiKey = req.Credentials[contract.CredentialKeyExaAPIKey]
	}
	if apiKey == "" {
		return contract.SearchResult{}, fmt.Errorf("Exa API Key is not configured")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	bodyParams := map[string]interface{}{
		"query":      req.Query,
		"numResults": limit,
	}

	if len(req.AllowedDomains) > 0 {
		bodyParams["includeDomains"] = req.AllowedDomains
	}
	if len(req.BlockedDomains) > 0 {
		bodyParams["excludeDomains"] = req.BlockedDomains
	}
	if req.RecencyDays > 0 {
		// Calculate starting published date
		startDate := time.Now().AddDate(0, 0, -req.RecencyDays)
		bodyParams["startPublishedDate"] = startDate.Format(time.RFC3339)
	}

	bodyBytes, err := json.Marshal(bodyParams)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("failed to encode Exa request body: %w", err)
	}

	platReq := &platformhttp.Request{
		Method:  "POST",
		BaseURL: p.baseURL,
		Headers: http.Header{
			"x-api-key":    []string{apiKey},
			"Content-Type": []string{"application/json"},
			"Accept":       []string{"application/json"},
		},
		Body: bodyBytes,
	}

	platResp, err := p.client.Do(ctx, platReq)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("Exa Search request failed: %w", err)
	}

	if platResp.StatusCode != http.StatusOK {
		return contract.SearchResult{}, fmt.Errorf("Exa Search request failed with status: %d, body: %s", platResp.StatusCode, string(platResp.Body))
	}

	var exaResp ExaResponse
	if err := json.Unmarshal(platResp.Body, &exaResp); err != nil {
		return contract.SearchResult{}, fmt.Errorf("failed to decode Exa search response: %w", err)
	}

	var hits []contract.SearchHit
	for i, item := range exaResp.Results {
		hit := contract.SearchHit{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: fmt.Sprintf("Neural search result by %s (Score: %.4f)", item.Author, item.Score),
			Source:  "exa",
			Rank:    i + 1,
		}
		if item.PublishedDate != "" {
			if parsedTime, err := time.Parse(time.RFC3339, item.PublishedDate); err == nil {
				hit.PublishedAt = &parsedTime
			} else if parsedTime, err := time.Parse("2006-01-02", item.PublishedDate); err == nil {
				hit.PublishedAt = &parsedTime
			}
		}
		hits = append(hits, hit)
	}

	return contract.SearchResult{
		Query:    req.Query,
		Provider: "exa",
		Results:  hits,
		Duration: time.Since(start),
	}, nil
}
