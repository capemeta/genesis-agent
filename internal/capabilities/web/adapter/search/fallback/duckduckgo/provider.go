package duckduckgo

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"genesis-agent/internal/capabilities/web/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"

	"golang.org/x/net/html"
)

type Provider struct {
	client  platformhttp.Client
	baseURL string
}

func NewProvider(baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://html.duckduckgo.com/html/"
	}
	return &Provider{
		client:  platformhttp.New(),
		baseURL: baseURL,
	}
}

func (p *Provider) Metadata() contract.ProviderMetadata {
	return contract.ProviderMetadata{
		ID:                 "duckduckgo",
		Name:               "DuckDuckGo HTML Search",
		Description:        "Fallback HTML search provider with no API keys required.",
		SupportedRegions:   []string{"US", "CN", "GLOBAL"},
		SupportedLanguages: []string{"en", "zh"},
		IsChinaCompliance:  false,
		IsSelfHosted:       false,
	}
}

func (p *Provider) ValidateCredentials(ctx context.Context, creds map[string]string) (bool, error) {
	return true, nil // No keys needed
}

func (p *Provider) HealthCheck(ctx context.Context) (bool, error) {
	return true, nil
}

func (p *Provider) Search(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, error) {
	u, err := url.Parse(p.baseURL)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("invalid DuckDuckGo baseURL: %w", err)
	}

	q := u.Query()
	q.Set("q", req.Query)
	u.RawQuery = q.Encode()
	queryURL := u.String()

	platReq := &platformhttp.Request{
		Method: "GET",
		BaseURL: queryURL,
		Headers: http.Header{
			"User-Agent": []string{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36"},
		},
	}

	platResp, err := p.client.Do(ctx, platReq)
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("DuckDuckGo request failed: %w", err)
	}

	doc, err := html.Parse(bytes.NewReader(platResp.Body))
	if err != nil {
		return contract.SearchResult{}, fmt.Errorf("failed to parse DDG HTML: %w", err)
	}

	hits := parseDDGHTML(doc)

	return contract.SearchResult{
		Query:    req.Query,
		Provider: "duckduckgo",
		Results:  hits,
	}, nil
}

func parseDDGHTML(n *html.Node) []contract.SearchHit {
	var hits []contract.SearchHit
	var currentRank = 1

	// Helper to walk DOM and extract result nodes
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			classVal := getAttr(n, "class")
			if strings.Contains(classVal, "result") && strings.Contains(classVal, "web-result") {
				// We found a web result container!
				title, link, snippet := extractResultDetails(n)
				if title != "" && link != "" {
					hits = append(hits, contract.SearchHit{
						Title:   title,
						URL:     link,
						Snippet: snippet,
						Source:  extractSource(link),
						Rank:    currentRank,
					})
					currentRank++
				}
				return // Don't descend into this container's children for more containers
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(n)
	return hits
}

func extractResultDetails(node *html.Node) (title string, link string, snippet string) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			classVal := getAttr(n, "class")
			if n.Data == "a" && strings.Contains(classVal, "result__a") {
				title = getElementText(n)
				rawLink := getAttr(n, "href")
				link = cleanDDGLink(rawLink)
			} else if (n.Data == "a" || n.Data == "span" || n.Data == "div") && strings.Contains(classVal, "result__snippet") {
				snippet = getElementText(n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)
	return
}

func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.ToLower(attr.Key) == key {
			return attr.Val
		}
	}
	return ""
}

func getElementText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

func cleanDDGLink(link string) string {
	if strings.HasPrefix(link, "https://duckduckgo.com/l/?uddg=") {
		u, err := url.Parse(link)
		if err == nil {
			uddg := u.Query().Get("uddg")
			if uddg != "" {
				return uddg
			}
		}
	}
	// Also clean potential relative DDG links or redirection wrappers
	if strings.HasPrefix(link, "//") {
		return "https:" + link
	}
	return link
}

func extractSource(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
