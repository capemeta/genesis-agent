package contract

import (
	"context"
	"errors"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

var ErrInvocationBindingNotFound = errors.New("skill invocation binding not found")
var ErrSkillPackageSnapshotNotFound = errors.New("skill package snapshot not found")

// Service 聚合多来源 Skill，并提供发现、解析、加载和资源读取能力。
type Service interface {
	Catalog(ctx context.Context, req CatalogRequest) (model.Catalog, error)
	Resolve(ctx context.Context, req ResolveRequest) (model.ResolvedInvocation, error)
	CreateBinding(ctx context.Context, req BindingRequest) (model.InvocationBinding, error)
	GetBinding(ctx context.Context, req BindingLookup) (model.InvocationBinding, error)
	Load(ctx context.Context, req LoadRequest) (model.Injection, error)
	ReadResource(ctx context.Context, req ResourceRequest) (model.ResourceContent, error)
	ListResources(ctx context.Context, req ListResourcesRequest) (model.ResourceList, error)
	SearchResources(ctx context.Context, req SearchResourcesRequest) (model.SearchResult, error)
	ReadBoundResource(ctx context.Context, req BoundResourceRequest) (model.ResourceContent, error)
	ListBoundResources(ctx context.Context, binding model.InvocationBinding) (model.ResourceList, error)
	SearchBoundResources(ctx context.Context, req BoundResourceSearchRequest) (model.SearchResult, error)
	SelectForTurn(ctx context.Context, req SelectionRequest) ([]model.InvocationMetadata, error)
	RenderAvailableSkills(ctx context.Context, req CatalogRequest) (string, error)
	ClearCache()
}

// InvocationBindingStore 持久化解析后的不可变运行事实。生产装配必须使用持久实现；
// 内存实现仅供测试和短生命周期嵌入场景。
type InvocationBindingStore interface {
	SaveBinding(context.Context, model.InvocationBinding) (model.InvocationBinding, error)
	GetBinding(context.Context, string) (model.InvocationBinding, error)
	GetBindingByIdempotencyKey(context.Context, string) (model.InvocationBinding, error)
	ListBindingsByRun(context.Context, string, string) ([]model.InvocationBinding, error)
}

// SkillPackageSnapshotStore 是按 package digest 寻址的不可变包存储。Binding
// 落盘前必须先保存包快照，执行阶段只允许从这里读取，不能回读可变 Source。
type SkillPackageSnapshotStore interface {
	SavePackageSnapshot(context.Context, model.SkillPackageSnapshot, []model.SkillPackageFile) error
	GetPackageSnapshot(context.Context, string) (model.SkillPackageSnapshot, []model.SkillPackageFile, error)
}

// PackageSnapshotReader 是执行侧对 Skill Service 的最小只读扩展。
type PackageSnapshotReader interface {
	GetPackageSnapshot(context.Context, string) (model.SkillPackageSnapshot, []model.SkillPackageFile, error)
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
	Name       string
	Resource   string
	ModelCall  bool
	Invocation string
}

type LoadRequest struct {
	Resolved model.ResolvedInvocation
	Binding  model.InvocationBinding
}

// ExplicitLoadRequest 描述用户显式选择 Skill 的内部加载请求。
// 它不进入 LLM function schema，只在 runtime / 产品装配层传递调用方身份。
type ExplicitLoadRequest struct {
	Skill    string
	Resource string
	Task     string
	Inputs   []string
}

type BindingRequest struct {
	Resolved              model.ResolvedInvocation
	TenantID              string
	RunID                 string
	ParentRunID           string
	Task                  string
	Inputs                []workmodel.ResourceRef
	ToolPolicy            model.EffectiveToolPolicy
	ExecutionPolicy       model.EffectiveExecutionPolicy
	Capabilities          model.EffectiveCapabilitySnapshot
	PolicySnapshotVersion string
}

type BindingLookup struct {
	TenantID string
	RunID    string
	Handle   string
	ID       string
}

type ResourceRequest struct {
	ResolveRequest
	PackageID model.PackageID
	Resource  model.ResourceID
	MaxBytes  int
}

// BoundResourceRequest/BoundResourceSearchRequest 强制从 InvocationBinding 固定的
// package digest 快照读取，执行阶段不得重新解析当前 Catalog/Source。
type BoundResourceRequest struct {
	Binding  model.InvocationBinding
	Resource model.ResourceID
	MaxBytes int
}

type BoundResourceSearchRequest struct {
	Binding model.InvocationBinding
	Query   string
	Limit   int
}

type ListResourcesRequest struct {
	ResolveRequest
	PackageID model.PackageID
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
	ParseRuntimeManifest(data []byte, skillName string) (model.RuntimeManifest, error)
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
