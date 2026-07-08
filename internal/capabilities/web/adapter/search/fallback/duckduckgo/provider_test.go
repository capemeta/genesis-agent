package duckduckgo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"genesis-agent/internal/capabilities/web/contract"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

func TestProvider_Search(t *testing.T) {
	// Create mock HTTP server simulating DuckDuckGo response
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		html := `
		<html>
		<body>
			<div class="result web-result">
				<a class="result__a" href="https://example.com/item1">Item One Title</a>
				<span class="result__snippet">This is the first search snippet.</span>
			</div>
			<div class="result web-result">
				<a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample2.com%2Fitem2">Item Two Title</a>
				<span class="result__snippet">This is the second search snippet.</span>
			</div>
		</body>
		</html>
		`
		rw.Write([]byte(html))
	}))
	defer server.Close()

	// Create provider wrapping our test server
	p := NewProvider(server.URL)
	p.client = platformhttp.New(platformhttp.WithHTTPClient(server.Client()))

	req := contract.SearchRequest{
		Query: "test",
		Limit: 5,
	}

	res, err := p.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(res.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res.Results))
	}

	if res.Results[0].Title != "Item One Title" || res.Results[0].URL != "https://example.com/item1" {
		t.Errorf("unexpected first result: %+v", res.Results[0])
	}

	if res.Results[1].URL != "https://example2.com/item2" {
		t.Errorf("expected cleaned URL 'https://example2.com/item2', got %q", res.Results[1].URL)
	}
}
