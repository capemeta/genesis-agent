package contract

import "context"

// Searcher defines the web search interface.
type Searcher interface {
	Search(ctx context.Context, req SearchRequest) (SearchResult, error)
}

// Fetcher defines the web page content fetching interface.
type Fetcher interface {
	Fetch(ctx context.Context, req FetchRequest) (FetchedDocument, error)
}

// FetchService defines the high-level orchestrated web fetch service.
type FetchService interface {
	Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)
}

// Extractor defines the interface to convert raw fetched document to Markdown or text.
type Extractor interface {
	Extract(ctx context.Context, doc FetchedDocument, req FetchRequest) (ExtractedDocument, error)
}

// Summarizer defines the optional interface for summarizing fetched web pages.
type Summarizer interface {
	Summarize(ctx context.Context, req SummarizeRequest) (SummarizeResult, error)
}

// WebPolicy defines the access policy/governance interface for search and fetch requests.
type WebPolicy interface {
	AuthorizeSearch(ctx context.Context, req SearchRequest) (PolicyDecision, error)
	AuthorizeFetch(ctx context.Context, req FetchRequest) (PolicyDecision, error)
}

// Cache defines the cache interface for search and fetch responses.
type Cache interface {
	GetSearch(ctx context.Context, req SearchRequest) (SearchResult, bool, error)
	SetSearch(ctx context.Context, req SearchRequest, res SearchResult) error
	GetFetch(ctx context.Context, req FetchRequest) (FetchResult, bool, error)
	SetFetch(ctx context.Context, req FetchRequest, res FetchResult) error
	Clear(ctx context.Context) error
}
