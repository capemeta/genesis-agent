package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"genesis-agent/internal/capabilities/web/contract"
	"genesis-agent/internal/capabilities/web/service"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

type Fetcher struct {
	client platformhttp.Client
}

func NewFetcher() *Fetcher {
	// Create custom http.Client that does not follow redirects automatically.
	// We handle redirects manually to check SSRF safety on each hop.
	customHTTPClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	client := platformhttp.New(
		platformhttp.WithHTTPClient(customHTTPClient),
	)

	return &Fetcher{
		client: client,
	}
}

func (f *Fetcher) Fetch(ctx context.Context, req contract.FetchRequest) (contract.FetchedDocument, error) {
	currentURL := req.URL
	maxRedirects := 5
	redirectCount := 0

	for {
		u, err := url.Parse(currentURL)
		if err != nil {
			return contract.FetchedDocument{}, fmt.Errorf("failed to parse current URL: %w", err)
		}

		// Re-validate IP safety before making request
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", u.Hostname())
		if err != nil {
			return contract.FetchedDocument{}, fmt.Errorf("failed to resolve host %s: %w", u.Hostname(), err)
		}
		for _, ip := range ips {
			if !service.IsIPSafe(ip) {
				return contract.FetchedDocument{}, fmt.Errorf("unsafe IP resolved: %s", ip.String())
			}
		}

		platReq := &platformhttp.Request{
			Method:               "GET",
			BaseURL:              currentURL,
			MaxResponseBodyBytes: req.MaxBytes,
			Headers: http.Header{
				"User-Agent": []string{"genesis-agent/web-fetch"},
				"Accept":     []string{"text/html,text/plain,text/markdown,application/xhtml+xml"},
			},
		}

		platResp, err := f.client.Do(ctx, platReq)
		if err != nil {
			return contract.FetchedDocument{}, fmt.Errorf("http request failed: %w", err)
		}

		// Check if it is a redirect
		if isRedirect(platResp.StatusCode) {
			location := platResp.Headers.Get("Location")
			if location == "" {
				return contract.FetchedDocument{}, fmt.Errorf("redirect status %d received but Location header is empty", platResp.StatusCode)
			}

			// Resolve location against currentURL
			locURL, err := u.Parse(location)
			if err != nil {
				return contract.FetchedDocument{}, fmt.Errorf("invalid redirect URL %q: %w", location, err)
			}
			redirectURL := locURL.String()

			// Check if host changes
			if !strings.EqualFold(u.Hostname(), locURL.Hostname()) {
				if !req.FollowRedirects {
					// Return special 3xx result indicating redirect required, to be handled by FetchService
					return contract.FetchedDocument{
						URL:         currentURL,
						StatusCode:  platResp.StatusCode,
						ContentType: "text/plain",
						Body:        []byte(redirectURL),
					}, nil
				}
			}

			redirectCount++
			if redirectCount > maxRedirects {
				return contract.FetchedDocument{}, errors.New("stopped after exceeding maximum redirects limit")
			}

			currentURL = redirectURL
			continue
		}

		return contract.FetchedDocument{
			URL:         currentURL,
			StatusCode:  platResp.StatusCode,
			ContentType: platResp.Headers.Get("Content-Type"),
			Body:        platResp.Body,
		}, nil
	}
}

func isRedirect(status int) bool {
	return status == 301 || status == 302 || status == 303 || status == 307 || status == 308
}
