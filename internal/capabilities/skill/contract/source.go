// Package contract 定义 Skill 能力契约。
package contract

import (
	"context"
	"time"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
	"genesis-agent/internal/capabilities/skill/model"
)

// Source 是 Skill 来源。List/Read/Search 必须由同一 Authority 路由。
type Source interface {
	Authority() model.Authority
	List(ctx context.Context, query ListQuery) (ListResult, error)
	Read(ctx context.Context, req ReadRequest) (ReadResult, error)
	ListResources(ctx context.Context, req SourceListResourcesRequest) (ListResourcesResult, error)
	Search(ctx context.Context, req SearchRequest) (SearchResult, error)
}

// PackageSnapshotSource 由能够返回原始包字节的 Source 实现。普通 Read 会按
// Skill 语义返回去除 frontmatter 的正文，不能替代不可变包快照读取。
type PackageSnapshotSource interface {
	ReadPackageSnapshot(ctx context.Context, expected model.SkillPackageSnapshot) ([]model.SkillPackageFile, error)
}

// Watcher 是可选的本地变更监听器，不强加给 DB/远程 source。
type Watcher interface {
	Watch(ctx context.Context, roots []WatchRoot) (<-chan ChangeEvent, error)
}

type WatchRoot struct {
	Path      string
	Scope     model.Scope
	Recursive bool
}

type ChangeEvent struct {
	Root      WatchRoot
	Path      string
	ChangedAt time.Time
}

type ListQuery struct {
	Product     profilemodel.ChannelType
	TenantID    string
	ProjectID   string
	AgentID     string
	UserID      string
	RoleIDs     []string
	Environment profilemodel.RuntimeEnvironment
}

type ListResult struct {
	Packages []model.PhysicalSkillDefinition
	Errors   []model.Error
	Warnings []string
	Version  string
}

type ReadRequest struct {
	Authority model.Authority
	PackageID model.PackageID
	Resource  model.ResourceID
	Version   string
	MaxBytes  int
}

type ReadResult struct {
	Metadata  model.Metadata
	Resource  model.ResourceID
	Content   string
	Version   string
	Truncated bool
}

type SourceListResourcesRequest struct {
	Authority model.Authority
	PackageID model.PackageID
	Version   string
}

type ListResourcesResult struct {
	Metadata  model.Metadata
	Resources []model.ResourceInfo
	Version   string
}

type SearchRequest struct {
	Authority model.Authority
	PackageID model.PackageID
	Query     string
	Limit     int
}

type SearchResult struct {
	Matches []model.SearchMatch
}
