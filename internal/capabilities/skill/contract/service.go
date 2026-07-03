package contract

import (
	"context"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/model"
)

// Service 聚合多来源 Skill，并提供发现、解析、加载和资源读取能力。
type Service interface {
	Catalog(ctx context.Context, req CatalogRequest) (model.Catalog, error)
	Resolve(ctx context.Context, req ResolveRequest) (model.Metadata, error)
	Load(ctx context.Context, req LoadRequest) (model.Injection, error)
	ReadResource(ctx context.Context, req ResourceRequest) (model.ResourceContent, error)
	SearchResources(ctx context.Context, req SearchResourcesRequest) (model.SearchResult, error)
	SelectForTurn(ctx context.Context, req SelectionRequest) ([]model.Metadata, error)
	RenderAvailableSkills(ctx context.Context, req CatalogRequest) (string, error)
	ClearCache()
}

type CatalogRequest struct {
	Product        profilemodel.ChannelType
	TenantID       string
	ProjectID      string
	AgentID        string
	UserID         string
	RoleIDs        []string
	Environment    profilemodel.RuntimeEnvironment
	EnabledSkills  []string
	DisabledSkills []string
	ForceReload    bool
}

type ResolveRequest struct {
	CatalogRequest
	Name      string
	Resource  string
	ModelCall bool
}

type LoadRequest struct {
	ResolveRequest
	Args string
}

type ResourceRequest struct {
	ResolveRequest
	PackageID model.PackageID
	Resource  model.ResourceID
	MaxBytes  int
}

type SearchResourcesRequest struct {
	ResolveRequest
	PackageID model.PackageID
	Query     string
	Limit     int
}

type SelectionRequest struct {
	CatalogRequest
	Text string
}

// Parser 解析 SKILL.md。
type Parser interface {
	ParseFrontmatter(data []byte, source ParseSource) (model.Metadata, error)
	ParseFull(data []byte, source ParseSource) (model.Metadata, string, error)
}

type ParseSource struct {
	Authority     model.Authority
	Scope         model.Scope
	PackageID     model.PackageID
	MainResource  model.ResourceID
	DisplayPath   string
	BaseDirectory string
	DirectoryName string
	Version       string
}
