package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/web/contract"
	"genesis-agent/internal/platform/logger"
)

type SearchService struct {
	providers        map[string]contract.SearchProvider
	defaultProvider  string
	policy           contract.WebPolicy
	cache            contract.Cache
	log              logger.Logger
}

func NewSearchService(providers []contract.SearchProvider, defaultProvider string, pol contract.WebPolicy, c contract.Cache, log logger.Logger) *SearchService {
	pm := make(map[string]contract.SearchProvider)
	for _, p := range providers {
		pm[p.Metadata().ID] = p
	}
	if log == nil {
		log = logger.NewNop()
	}
	return &SearchService{
		providers:       pm,
		defaultProvider: defaultProvider,
		policy:          pol,
		cache:           c,
		log:             log,
	}
}

func (s *SearchService) Search(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, error) {
	start := time.Now()

	// 1. Basic input validation
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		return contract.SearchResult{}, fmt.Errorf("search query cannot be empty")
	}

	// 2. Policy Authorization
	decision, err := s.policy.AuthorizeSearch(ctx, req)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("policy evaluation error: %w", err)
	}
	if !decision.Allowed {
		return contract.SearchResult{}, fmt.Errorf("search blocked by policy: %s", decision.Reason)
	}

	// 3. Cache lookup
	if s.cache != nil {
		if cached, found, err := s.cache.GetSearch(ctx, req); err == nil && found {
			cached.Cached = true
			cached.Duration = time.Since(start)
			return cached, nil
		}
	}

	// 4. Determine provider and fallback list
	providerIDs := []string{s.defaultProvider}
	// Fallback order: brave, searxng, duckduckgo
	for _, id := range []string{"brave", "searxng", "duckduckgo"} {
		if id != s.defaultProvider {
			providerIDs = append(providerIDs, id)
		}
	}

	var lastErr error
	var res contract.SearchResult
	var success bool

	for _, pID := range providerIDs {
		provider, exists := s.providers[pID]
		if !exists {
			continue
		}

		res, err = provider.Search(ctx, req)
		if err == nil {
			success = true
			res.Provider = pID
			break
		}
		lastErr = err
	}

	if !success {
		if lastErr != nil {
			s.log.Error("网络搜索执行失败", "query", req.Query, "error", lastErr)
			return contract.SearchResult{}, fmt.Errorf("all search providers failed. Last error: %w", lastErr)
		}
		s.log.Error("网络搜索未配置任何可用提供商")
		return contract.SearchResult{}, fmt.Errorf("no search providers configured")
	}

	// 5. Post-filter results based on allowed/blocked domains to guarantee strict adherence
	res.Results = s.filterHits(res.Results, req)

	// Trim results to limit
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if len(res.Results) > limit {
		res.Results = res.Results[:limit]
	}

	// Re-rank
	for i := range res.Results {
		res.Results[i].Rank = i + 1
	}

	res.Duration = time.Since(start)

	s.log.Info("网络搜索执行成功", "query", req.Query, "provider", res.Provider, "hits", len(res.Results), "duration_ms", res.Duration.Milliseconds())

	// 6. Save to cache
	if s.cache != nil {
		_ = s.cache.SetSearch(ctx, req, res)
	}

	return res, nil
}

func (s *SearchService) filterHits(hits []contract.SearchHit, req contract.SearchRequest) []contract.SearchHit {
	var filtered []contract.SearchHit

	for _, hit := range hits {
		u, err := url.Parse(hit.URL)
		if err != nil {
			continue
		}
		hostname := strings.ToLower(u.Hostname())
		if hostname == "" {
			continue
		}

		// Check blocked list
		blocked := false
		for _, pattern := range req.BlockedDomains {
			if matchDomain(hostname, pattern) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}

		// Check allowed list
		if len(req.AllowedDomains) > 0 {
			allowed := false
			for _, pattern := range req.AllowedDomains {
				if matchDomain(hostname, pattern) {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}

		filtered = append(filtered, hit)
	}

	return filtered
}
