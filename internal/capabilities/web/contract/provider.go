package contract

import "context"

// ProviderMetadata details a search or fetch provider.
type ProviderMetadata struct {
	ID                 string
	Name               string
	Description        string
	SupportedRegions   []string
	SupportedLanguages []string
	IsChinaCompliance  bool // Indicates if it satisfies China data residency and compliance
	IsSelfHosted       bool
}

// SearchProvider represents a search provider implementation (e.g. Brave, SearXNG).
type SearchProvider interface {
	Searcher
	Metadata() ProviderMetadata
	ValidateCredentials(ctx context.Context, creds map[string]string) (bool, error)
	HealthCheck(ctx context.Context) (bool, error)
}
