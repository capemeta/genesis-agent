package htmlmarkdown

import (
	"context"
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/web/contract"
)

func TestExtractor_Extract(t *testing.T) {
	htmlContent := `
	<!DOCTYPE html>
	<html>
	<head>
		<title>Test Page Title</title>
		<meta name="description" content="This is a description content.">
		<style>body { color: red; }</style>
		<script>console.log("hello");</script>
	</head>
	<body>
		<h1>Main Header</h1>
		<p>This is a paragraph with a <a href="/relative-link">link</a>.</p>
		<p>Another paragraph.</p>
		<ul>
			<li>First item</li>
			<li>Second item</li>
		</ul>
		<pre><code>func main() {}</code></pre>
	</body>
	</html>
	`

	doc := contract.FetchedDocument{
		URL:         "https://example.com/start",
		StatusCode:  200,
		ContentType: "text/html",
		Body:        []byte(htmlContent),
	}

	req := contract.FetchRequest{
		URL: "https://example.com/start",
	}

	extractor := NewExtractor()
	res, err := extractor.Extract(context.Background(), doc, req)
	if err != nil {
		t.Fatalf("failed to extract: %v", err)
	}

	if res.Title != "Test Page Title" {
		t.Errorf("expected Title 'Test Page Title', got '%s'", res.Title)
	}

	if res.Description != "This is a description content." {
		t.Errorf("expected Description 'This is a description content.', got '%s'", res.Description)
	}

	if !strings.Contains(res.Markdown, "# Main Header") {
		t.Error("markdown missing h1 header")
	}

	if !strings.Contains(res.Markdown, "[link](https://example.com/relative-link)") {
		t.Error("markdown failed to resolve relative link correctly")
	}

	if strings.Contains(res.Markdown, "console.log") {
		t.Error("markdown incorrectly kept script tags")
	}
}
