package service

import (
	"context"
	"net/url"
	"strings"

	"genesis-agent/internal/capabilities/web/contract"
)

type Policy struct {
	allowedDomains []string
	blockedDomains []string
}

func NewPolicy(allowed, blocked []string) *Policy {
	return &Policy{
		allowedDomains: allowed,
		blockedDomains: blocked,
	}
}

func (p *Policy) AuthorizeSearch(ctx context.Context, req contract.SearchRequest) (contract.PolicyDecision, error) {
	// Query check: empty query is not allowed
	if strings.TrimSpace(req.Query) == "" {
		return contract.PolicyDecision{Allowed: false, Reason: "query is empty"}, nil
	}

	// Merge request domain filters with policy domain filters. Deny list has precedence.
	// For search, allowed/blocked domains could filter search results or check query context,
	// but we can authorize the request itself if it's targeted.
	return contract.PolicyDecision{Allowed: true, Reason: "search is authorized"}, nil
}

func (p *Policy) AuthorizeFetch(ctx context.Context, req contract.FetchRequest) (contract.PolicyDecision, error) {
	u, err := url.Parse(req.URL)
	if err != nil {
		return contract.PolicyDecision{Allowed: false, Reason: "invalid url format"}, nil
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return contract.PolicyDecision{Allowed: false, Reason: "missing hostname"}, nil
	}

	// 1. Check Blocked List (Deny-list takes precedence)
	// Combine request-level blocked domains and policy-level blocked domains
	allBlocked := append(p.blockedDomains, req.BlockedDomains...)
	for _, pattern := range allBlocked {
		if matchDomain(hostname, pattern) {
			return contract.PolicyDecision{Allowed: false, Reason: "domain is blocked"}, nil
		}
	}

	// 2. Check Allowed List
	// Combine request-level allowed domains and policy-level allowed domains
	allAllowed := append(p.allowedDomains, req.AllowedDomains...)
	if len(allAllowed) > 0 {
		matched := false
		for _, pattern := range allAllowed {
			if matchDomain(hostname, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			return contract.PolicyDecision{Allowed: false, Reason: "domain is not in the allowed list"}, nil
		}
	}

	return contract.PolicyDecision{Allowed: true, Reason: "fetch is authorized"}, nil
}

func matchDomain(domain, pattern string) bool {
	domain = strings.ToLower(domain)
	pattern = strings.ToLower(pattern)

	if pattern == "*" || pattern == "*.*" {
		return true
	}

	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		base := pattern[2:]   // "example.com"
		return domain == base || strings.HasSuffix(domain, suffix)
	}

	return domain == pattern
}
