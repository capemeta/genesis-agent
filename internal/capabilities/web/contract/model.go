package contract

import (
	"time"
)

type SearchMode string

const (
	SearchModeDisabled SearchMode = "disabled"
	SearchModeCached   SearchMode = "cached"
	SearchModeIndexed  SearchMode = "indexed"
	SearchModeLive     SearchMode = "live"
)

type FetchFormat string

const (
	FetchFormatMarkdown FetchFormat = "markdown"
	FetchFormatText     FetchFormat = "text"
	FetchFormatHTML     FetchFormat = "html"
	FetchFormatMetadata FetchFormat = "metadata"
)

type RenderMode string

const (
	RenderModeHTTP    RenderMode = "http"
	RenderModeBrowser RenderMode = "browser"
	RenderModeSandbox RenderMode = "sandbox"
)

type SearchRequest struct {
	TenantID       string            `json:"tenant_id"`
	Query          string            `json:"query"`
	Limit          int               `json:"limit"`
	RecencyDays    int               `json:"recency_days"`
	AllowedDomains []string          `json:"allowed_domains"`
	BlockedDomains []string          `json:"blocked_domains"`
	Locale         string            `json:"locale"`
	Region         string            `json:"region"`
	SafeSearch     string            `json:"safe_search"`
	Mode           SearchMode        `json:"mode"`
	ContextSize    string            `json:"context_size"`
	Credentials    map[string]string `json:"credentials"`
}

type SearchResult struct {
	Query    string        `json:"query"`
	Provider string        `json:"provider"`
	Results  []SearchHit   `json:"results"`
	Cached   bool          `json:"cached"`
	Duration time.Duration `json:"duration"`
}

type SearchHit struct {
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Snippet     string     `json:"snippet"`
	Source      string     `json:"source"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Rank        int        `json:"rank"`
}

type FetchRequest struct {
	TenantID        string            `json:"tenant_id"`
	URL             string            `json:"url"`
	Prompt          string            `json:"prompt"`
	Format          FetchFormat       `json:"format"`
	MaxBytes        int64             `json:"max_bytes"`
	MaxChars        int               `json:"max_chars"`
	Timeout         time.Duration     `json:"timeout"`
	AllowedDomains  []string          `json:"allowed_domains"`
	BlockedDomains  []string          `json:"blocked_domains"`
	FollowRedirects bool              `json:"follow_redirects"`
	RenderMode      RenderMode        `json:"render_mode"`
	Credentials     map[string]string `json:"credentials"`
}

type FetchResult struct {
	URL         string        `json:"url"`
	FinalURL    string        `json:"final_url"`
	StatusCode  int           `json:"status_code"`
	ContentType string        `json:"content_type"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Markdown    string        `json:"markdown"`
	Text        string        `json:"text"`
	Citations   []Citation    `json:"citations"`
	Bytes       int           `json:"bytes"`
	Cached      bool          `json:"cached"`
	Truncated   bool          `json:"truncated"`
	Duration    time.Duration `json:"duration"`
}

type FetchedDocument struct {
	URL         string
	StatusCode  int
	ContentType string
	Body        []byte
}

type ExtractedDocument struct {
	Title       string
	Description string
	Markdown    string
	Text        string
}

type Citation struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	StartOffset int    `json:"start_offset"`
	EndOffset   int    `json:"end_offset"`
}

type SummarizeRequest struct {
	Content string
	Prompt  string
}

type SummarizeResult struct {
	Summary string
}

type PolicyDecision struct {
	Allowed bool
	Reason  string
}
