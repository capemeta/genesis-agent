package contract

import (
	"context"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ProducedResourceStore persists immutable descriptors.
// Create 对 (id) 与 (logical_ref) 均排他；同槽位内容变更请用 UpsertCurrent。
// ListByRun 只返回每个 logical_ref 的当前 head（已被 supersede 的历史版本仍可通过 Get 读取）。
type ProducedResourceStore interface {
	Create(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error
	// UpsertCurrent 写入新的不可变 descriptor，并将其设为 LogicalRef 的当前 head。
	// 旧 head 仍可按 ID 读取，但不再出现在 ListByRun 中。
	UpsertCurrent(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error
	Get(ctx context.Context, tenantID, runID, producedResourceID string) (workmodel.ProducedResourceDescriptor, error)
	GetByLogicalRef(ctx context.Context, tenantID, runID, logicalRef string) (workmodel.ProducedResourceDescriptor, error)
	ListByRun(ctx context.Context, tenantID, runID string) ([]workmodel.ProducedResourceDescriptor, error)
}

// RegisterProducedResourceRequest contains discovery facts, never a backend locator.
type RegisterProducedResourceRequest struct {
	TenantID     string
	RunID        string
	BindingID    string
	LogicalRef   string
	ObservedPath workmodel.WorkspacePath
	ObservedName string
	MediaType    string
	Size         int64
	Availability workmodel.ResourceAvailability
	ExpiresAt    *time.Time
}

// BackendResourceRequest is passed only to a trusted backend adapter.
type BackendResourceRequest struct {
	TenantID     string
	RunID        string
	Execution    workmodel.PreparedExecutionSnapshot
	LogicalRef   string
	ObservedPath workmodel.WorkspacePath
	ObservedName string
	MediaType    string
	Size         int64
	Availability workmodel.ResourceAvailability
	ExpiresAt    *time.Time
	// PreferSource 为同槽位已有 head 的 Source。内容指纹仍匹配时应复用该 locator，
	// 禁止再 Create 孤儿 locator；仅允许刷新 leased ExpiresAt 等元数据。
	PreferSource *workmodel.ResourceRef
}

// BackendResourceResolver converts trusted execution observations into an opaque, versioned locator.
type BackendResourceResolver interface {
	ResolveProducedResource(ctx context.Context, req BackendResourceRequest) (workmodel.ResourceRef, error)
}

// BackendResourceResolverRoute 把明确 backend kind 与 availability 绑定到一个 locator resolver。
type BackendResourceResolverRoute struct {
	Backend      execmodel.BackendKind
	Availability workmodel.ResourceAvailability
	Resolver     BackendResourceResolver
}

// ProducedResourceRegistrar is the only Harness-facing descriptor creation port.
type ProducedResourceRegistrar interface {
	RegisterProducedResource(ctx context.Context, req RegisterProducedResourceRequest) (workmodel.ProducedResourceDescriptor, error)
}

// ResourceReaderRoute binds one exact authority/scheme pair to a reader.
type ResourceReaderRoute struct {
	Authority string
	Scheme    string
	Reader    ResourceReader
}

// ResourceReaderRouter selects a reader from trusted persisted backend/source identities.
type ResourceReaderRouter interface {
	Open(ctx context.Context, backend execmodel.ExecutionBackendRef, source workmodel.ResourceRef) (ResourceHandle, error)
}
