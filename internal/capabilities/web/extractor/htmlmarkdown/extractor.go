package htmlmarkdown

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"

	"genesis-agent/internal/capabilities/web/contract"

	"golang.org/x/net/html"
)

type Extractor struct{}

func NewExtractor() *Extractor {
	return &Extractor{}
}

func (e *Extractor) Extract(ctx context.Context, doc contract.FetchedDocument, req contract.FetchRequest) (contract.ExtractedDocument, error) {
	reader := bytes.NewReader(doc.Body)
	root, err := html.Parse(reader)
	if err != nil {
		return contract.ExtractedDocument{}, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var title, description string
	var mdBuilder strings.Builder

	// Pre-resolve base URL for absolute links
	baseURL, _ := url.Parse(doc.URL)

	// Step 1: Find title and description
	findMetadata(root, &title, &description)

	// Step 2: Walk body to build markdown
	bodyNode := findBody(root)
	if bodyNode == nil {
		bodyNode = root // Fallback to root
	}

	walk(bodyNode, &mdBuilder, baseURL)

	markdown := mdBuilder.String()
	markdown = cleanMarkdown(markdown)

	// Simple text extraction by stripping markdown symbols or just reusing markdown/simple parser
	text := extractPlaintext(markdown)

	return contract.ExtractedDocument{
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		Markdown:    markdown,
		Text:        text,
	}, nil
}

func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && strings.ToLower(n.Data) == "body" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if res := findBody(c); res != nil {
			return res
		}
	}
	return nil
}

func findMetadata(n *html.Node, title *string, description *string) {
	if n.Type == html.ElementNode {
		name := strings.ToLower(n.Data)
		if name == "title" && n.FirstChild != nil {
			*title = n.FirstChild.Data
		} else if name == "meta" {
			var metaName, metaProp, metaContent string
			for _, attr := range n.Attr {
				k := strings.ToLower(attr.Key)
				if k == "name" {
					metaName = strings.ToLower(attr.Val)
				} else if k == "property" {
					metaProp = strings.ToLower(attr.Val)
				} else if k == "content" {
					metaContent = attr.Val
				}
			}
			if (metaName == "description" || metaProp == "og:description") && *description == "" {
				*description = metaContent
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		findMetadata(c, title, description)
	}
}

// walk recursive helper to render node tree as Markdown
func walk(n *html.Node, w *strings.Builder, baseURL *url.URL) {
	if n == nil {
		return
	}

	// Skip non-visible or script/style elements
	if n.Type == html.ElementNode {
		tag := strings.ToLower(n.Data)
		if tag == "script" || tag == "style" || tag == "noscript" || tag == "iframe" ||
			tag == "svg" || tag == "canvas" || tag == "select" || tag == "form" ||
			tag == "button" || tag == "head" {
			return
		}
	}

	if n.Type == html.TextNode {
		text := n.Data
		// Normalize whitespace
		if strings.TrimSpace(text) == "" {
			return
		}
		w.WriteString(text)
		return
	}

	if n.Type == html.ElementNode {
		tag := strings.ToLower(n.Data)
		switch tag {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			level := int(tag[1] - '0')
			w.WriteString("\n\n")
			w.WriteString(strings.Repeat("#", level))
			w.WriteString(" ")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}
			w.WriteString("\n\n")

		case "p", "div":
			w.WriteString("\n\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}
			w.WriteString("\n\n")

		case "br":
			w.WriteString("\n")

		case "strong", "b":
			w.WriteString("**")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}
			w.WriteString("**")

		case "em", "i":
			w.WriteString("*")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}
			w.WriteString("*")

		case "code":
			// If inside pre, it's a code block; otherwise, it's inline.
			isPre := false
			if n.Parent != nil && strings.ToLower(n.Parent.Data) == "pre" {
				isPre = true
			}
			if isPre {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, w, baseURL)
				}
			} else {
				w.WriteString("`")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, w, baseURL)
				}
				w.WriteString("`")
			}

		case "pre":
			w.WriteString("\n\n```\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}
			w.WriteString("\n```\n\n")

		case "a":
			var href string
			for _, attr := range n.Attr {
				if strings.ToLower(attr.Key) == "href" {
					href = attr.Val
					break
				}
			}
			// Resolve relative URL
			if href != "" && baseURL != nil {
				if u, err := url.Parse(href); err == nil {
					href = baseURL.ResolveReference(u).String()
				}
			}
			if href == "" || strings.HasPrefix(href, "javascript:") {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, w, baseURL)
				}
			} else {
				w.WriteString("[")
				var linkText strings.Builder
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, &linkText, baseURL)
				}
				lt := strings.TrimSpace(linkText.String())
				if lt == "" {
					lt = href
				}
				w.WriteString(lt)
				w.WriteString("](")
				w.WriteString(href)
				w.WriteString(")")
			}

		case "ul":
			w.WriteString("\n\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}
			w.WriteString("\n\n")

		case "ol":
			w.WriteString("\n\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}
			w.WriteString("\n\n")

		case "li":
			w.WriteString("\n- ")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}

		case "img":
			var src, alt string
			for _, attr := range n.Attr {
				k := strings.ToLower(attr.Key)
				if k == "src" {
					src = attr.Val
				} else if k == "alt" {
					alt = attr.Val
				}
			}
			if src != "" {
				if baseURL != nil {
					if u, err := url.Parse(src); err == nil {
						src = baseURL.ResolveReference(u).String()
					}
				}
				if alt == "" {
					alt = "image"
				}
				w.WriteString(fmt.Sprintf("![%s](%s)", alt, src))
			}

		default:
			// For generic elements, just walk children
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, w, baseURL)
			}
		}
	}
}

func cleanMarkdown(md string) string {
	// Normalize excessive line breaks
	lines := strings.Split(md, "\n")
	var result []string
	consecutiveNewlines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			consecutiveNewlines++
			if consecutiveNewlines <= 1 {
				result = append(result, "")
			}
		} else {
			consecutiveNewlines = 0
			result = append(result, line)
		}
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

func extractPlaintext(md string) string {
	// A simple heuristic plaintext extraction from markdown.
	// We can strip links, codes, headers markdown annotations to make it cleaner.
	txt := md
	// Remove image syntax
	// Simple replacement or parser can be added, but for basic search index/plaintext, simple is good.
	return txt
}
