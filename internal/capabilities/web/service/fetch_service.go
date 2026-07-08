package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/web/contract"
)

type FetchService struct {
	fetcher   contract.Fetcher
	extractor contract.Extractor
	policy    contract.WebPolicy
	cache     contract.Cache
}

func NewFetchService(f contract.Fetcher, ext contract.Extractor, pol contract.WebPolicy, c contract.Cache) *FetchService {
	return &FetchService{
		fetcher:   f,
		extractor: ext,
		policy:    pol,
		cache:     c,
	}
}

func (s *FetchService) Fetch(ctx context.Context, req contract.FetchRequest) (contract.FetchResult, error) {
	start := time.Now()

	// 1. URL parsing and basic safety validation
	u, err := url.Parse(req.URL)
	if err != nil {
		return contract.FetchResult{}, fmt.Errorf("invalid URL: %w", err)
	}

	if err := s.validateURLSafety(ctx, u); err != nil {
		return contract.FetchResult{}, err
	}

	// 2. Authorization
	decision, err := s.policy.AuthorizeFetch(ctx, req)
	if err != nil {
		return contract.FetchResult{}, fmt.Errorf("policy evaluation error: %w", err)
	}
	if !decision.Allowed {
		return contract.FetchResult{}, fmt.Errorf("fetch blocked by policy: %s", decision.Reason)
	}

	// 3. Cache lookup
	if s.cache != nil {
		if cached, found, err := s.cache.GetFetch(ctx, req); err == nil && found {
			cached.Cached = true
			cached.Duration = time.Since(start)
			return cached, nil
		}
	}

	// 4. Fetch the document
	doc, err := s.fetcher.Fetch(ctx, req)
	if err != nil {
		return contract.FetchResult{}, fmt.Errorf("fetching failed: %w", err)
	}

	// Check if the fetcher returned a redirect instruction
	// (some fetchers might detect redirects and return them if follow_redirects is false or cross-host)
	// We handle it cleanly by converting the response to a result.
	if doc.StatusCode == 301 || doc.StatusCode == 302 || doc.StatusCode == 307 || doc.StatusCode == 308 {
		redirectURL := string(doc.Body)
		result := contract.FetchResult{
			URL:        req.URL,
			FinalURL:   redirectURL,
			StatusCode: doc.StatusCode,
			Markdown:   fmt.Sprintf("REDIRECT DETECTED: The URL redirects to a different host.\n\nOriginal URL: %s\nRedirect URL: %s\n\nTo complete your request, please fetch again from the redirected URL.", req.URL, redirectURL),
			Text:       fmt.Sprintf("Redirected to %s", redirectURL),
			Bytes:      len(doc.Body),
			Duration:   time.Since(start),
		}
		return result, nil
	}

	// 5. Extract Markdown/Text
	extDoc, err := s.extractor.Extract(ctx, doc, req)
	if err != nil {
		return contract.FetchResult{}, fmt.Errorf("extraction failed: %w", err)
	}

	// 6. Handle truncation
	truncated := false
	markdown := extDoc.Markdown
	text := extDoc.Text
	if req.MaxChars > 0 {
		if len(markdown) > req.MaxChars {
			markdown = markdown[:req.MaxChars] + "\n...[content truncated]"
			truncated = true
		}
		if len(text) > req.MaxChars {
			text = text[:req.MaxChars] + "\n...[content truncated]"
			truncated = true
		}
	}

	result := contract.FetchResult{
		URL:         req.URL,
		FinalURL:    doc.URL,
		StatusCode:  doc.StatusCode,
		ContentType: doc.ContentType,
		Title:       extDoc.Title,
		Description: extDoc.Description,
		Markdown:    markdown,
		Text:        text,
		Citations: []contract.Citation{
			{
				URL:         doc.URL,
				Title:       extDoc.Title,
				StartOffset: 0,
				EndOffset:   len(markdown),
			},
		},
		Bytes:     len(doc.Body),
		Truncated: truncated,
		Duration:  time.Since(start),
	}

	// 7. Save to cache
	if s.cache != nil {
		_ = s.cache.SetFetch(ctx, req, result)
	}

	return result, nil
}

func (s *FetchService) validateURLSafety(ctx context.Context, u *url.URL) error {
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("forbidden scheme: %s. Only http and https are allowed", scheme)
	}

	if u.User != nil {
		return errors.New("URL credentials are not allowed")
	}

	hostname := u.Hostname()
	if hostname == "" {
		return errors.New("missing host in URL")
	}

	// Resolve IPs for the host
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", hostname)
	if err != nil {
		// If we cannot resolve, block it to be safe
		return fmt.Errorf("failed to resolve host %s: %w", hostname, err)
	}

	for _, ip := range ips {
		if !IsIPSafe(ip) {
			return fmt.Errorf("unsafe IP address resolved: %s", ip.String())
		}
	}

	return nil
}

var AllowLoopbackForTest = false

// IsIPSafe checks whether an IP address is safe for outbound web fetch requests (prevents SSRF).
func IsIPSafe(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// 1. Loopback
	if ip.IsLoopback() {
		return AllowLoopbackForTest
	}
	// Let's write the checks correctly:
	// A safe IP must NOT be loopback, private, link-local, multicast, or unspecified.

	// Check IPv4
	if ip4 := ip.To4(); ip4 != nil {
		// Unspecified
		if ip4.IsUnspecified() {
			return false
		}
		// Local identification subnet: 0.0.0.0/8
		if ip4[0] == 0 {
			return false
		}
		// Loopback: 127.0.0.0/8
		if ip4[0] == 127 {
			return false
		}
		// Private Class A: 10.0.0.0/8
		if ip4[0] == 10 {
			return false
		}
		// Private Class B: 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return false
		}
		// Private Class C: 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return false
		}
		// Link Local: 169.254.0.0/16
		if ip4[0] == 169 && ip4[1] == 254 {
			return false
		}
		// Carrier-Grade NAT: 100.64.0.0/10
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return false
		}
		// Multicast: 224.0.0.0/4
		if ip4[0] >= 224 && ip4[0] <= 239 {
			return false
		}
		// Reserved/Broadcast: 240.0.0.0/4
		if ip4[0] >= 240 {
			return false
		}
		return true
	}

	// Check IPv6
	if ip16 := ip.To16(); ip16 != nil {
		if ip.IsUnspecified() {
			return false
		}
		if ip.IsLoopback() {
			return false
		}
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return false
		}
		// Unique Local Address (ULA): fc00::/7
		if ip16[0]&0xfe == 0xfc {
			return false
		}
		if ip.IsMulticast() {
			return false
		}
		return true
	}

	return false
}
