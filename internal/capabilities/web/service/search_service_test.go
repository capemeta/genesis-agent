package service

import (
	"context"
	"testing"

	"genesis-agent/internal/capabilities/web/contract"
)

type mockSearchProvider struct {
	id   string
	fn   func(req contract.SearchRequest) (contract.SearchResult, error)
	meta contract.ProviderMetadata
}

func (m *mockSearchProvider) Search(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, error) {
	return m.fn(req)
}

func (m *mockSearchProvider) Metadata() contract.ProviderMetadata {
	return m.meta
}

func (m *mockSearchProvider) ValidateCredentials(ctx context.Context, creds map[string]string) (bool, error) {
	return true, nil
}

func (m *mockSearchProvider) HealthCheck(ctx context.Context) (bool, error) {
	return true, nil
}

func TestSearchService_Search(t *testing.T) {
	p1 := &mockSearchProvider{
		id: "brave",
		meta: contract.ProviderMetadata{ID: "brave"},
		fn: func(req contract.SearchRequest) (contract.SearchResult, error) {
			return contract.SearchResult{
				Query:    req.Query,
				Provider: "brave",
				Results: []contract.SearchHit{
					{Title: "Brave Hit 1", URL: "https://brave1.com", Snippet: "Brave snippet"},
				},
			}, nil
		},
	}

	pol := &mockPolicy{
		fnSearch: func(req contract.SearchRequest) (contract.PolicyDecision, error) {
			return contract.PolicyDecision{Allowed: true, Reason: ""}, nil
		},
	}

	svc := NewSearchService([]contract.SearchProvider{p1}, "brave", pol, nil, nil)
	req := contract.SearchRequest{
		Query: "hello",
		Limit: 5,
	}

	res, err := svc.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if res.Provider != "brave" {
		t.Errorf("expected provider 'brave', got %q", res.Provider)
	}

	if len(res.Results) != 1 || res.Results[0].Title != "Brave Hit 1" {
		t.Errorf("unexpected results: %+v", res.Results)
	}
}

func TestSearchService_Search_Fallback(t *testing.T) {
	p1 := &mockSearchProvider{
		id: "brave",
		meta: contract.ProviderMetadata{ID: "brave"},
		fn: func(req contract.SearchRequest) (contract.SearchResult, error) {
			return contract.SearchResult{}, error(nil) // Wait, let's trigger failure
		},
	}
	// Let's make it return an error to trigger fallback
	p1.fn = func(req contract.SearchRequest) (contract.SearchResult, error) {
		return contract.SearchResult{}, stringError("brave error")
	}

	p2 := &mockSearchProvider{
		id: "duckduckgo",
		meta: contract.ProviderMetadata{ID: "duckduckgo"},
		fn: func(req contract.SearchRequest) (contract.SearchResult, error) {
			return contract.SearchResult{
				Query:    req.Query,
				Provider: "duckduckgo",
				Results: []contract.SearchHit{
					{Title: "DDG Hit 1", URL: "https://ddg1.com", Snippet: "DDG snippet"},
				},
			}, nil
		},
	}

	pol := &mockPolicy{
		fnSearch: func(req contract.SearchRequest) (contract.PolicyDecision, error) {
			return contract.PolicyDecision{Allowed: true, Reason: ""}, nil
		},
	}

	svc := NewSearchService([]contract.SearchProvider{p1, p2}, "brave", pol, nil, nil)
	req := contract.SearchRequest{
		Query: "hello",
		Limit: 5,
	}

	res, err := svc.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if res.Provider != "duckduckgo" {
		t.Errorf("expected fallback provider 'duckduckgo', got %q", res.Provider)
	}
}

func TestSearchService_Search_Filters(t *testing.T) {
	p1 := &mockSearchProvider{
		id: "brave",
		meta: contract.ProviderMetadata{ID: "brave"},
		fn: func(req contract.SearchRequest) (contract.SearchResult, error) {
			return contract.SearchResult{
				Query:    req.Query,
				Provider: "brave",
				Results: []contract.SearchHit{
					{Title: "Allowed Site", URL: "https://allowed.com/page", Snippet: "Allowed"},
					{Title: "Blocked Site", URL: "https://blocked.com/page", Snippet: "Blocked"},
					{Title: "Another Site", URL: "https://other.com/page", Snippet: "Other"},
				},
			}, nil
		},
	}

	pol := &mockPolicy{
		fnSearch: func(req contract.SearchRequest) (contract.PolicyDecision, error) {
			return contract.PolicyDecision{Allowed: true, Reason: ""}, nil
		},
	}

	svc := NewSearchService([]contract.SearchProvider{p1}, "brave", pol, nil, nil)
	req := contract.SearchRequest{
		Query:          "hello",
		Limit:          5,
		AllowedDomains: []string{"allowed.com"},
		BlockedDomains: []string{"blocked.com"},
	}

	res, err := svc.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(res.Results) != 1 {
		t.Errorf("expected 1 result after filtering, got %d", len(res.Results))
	} else if res.Results[0].Title != "Allowed Site" {
		t.Errorf("expected result 'Allowed Site', got %q", res.Results[0].Title)
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }
