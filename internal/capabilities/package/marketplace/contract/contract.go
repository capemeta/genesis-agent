// Package contract 定义 Package Marketplace 的产品无关端口。
package contract

import (
	"context"
	capmodel "genesis-agent/internal/capabilities/capability/model"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

// RegistryStore 管理已添加的 marketplace 来源与 cache 位置。
type RegistryStore interface {
	List(ctx context.Context) ([]marketmodel.MarketplaceRecord, error)
	Get(ctx context.Context, name string) (marketmodel.MarketplaceRecord, bool, error)
	Put(ctx context.Context, record marketmodel.MarketplaceRecord) error
	Delete(ctx context.Context, name string) (marketmodel.MarketplaceRecord, bool, error)
}

// InstallStore 管理已安装 package 的状态。
type InstallStore interface {
	List(ctx context.Context) ([]marketmodel.InstallRecord, error)
	Get(ctx context.Context, spec string) (marketmodel.InstallRecord, bool, error)
	Put(ctx context.Context, record marketmodel.InstallRecord) error
	Delete(ctx context.Context, spec string) (marketmodel.InstallRecord, bool, error)
}

// CapabilityIndexStore 管理已安装 Package 投影出的运行时能力索引。
type CapabilityIndexStore interface {
	List(ctx context.Context) ([]capmodel.CapabilityIndexRecord, error)
	PutPackageCapabilities(ctx context.Context, spec string, records []capmodel.CapabilityIndexRecord) error
	SetPackageEnabled(ctx context.Context, spec string, enabled bool) error
	SetCapabilityEnabled(ctx context.Context, id string, enabled bool) (capmodel.CapabilityIndexRecord, bool, error)
	DeletePackage(ctx context.Context, spec string) error
}

// SourceParser 把 CLI/Desktop 输入解析成结构化 marketplace source。
type SourceParser interface {
	Parse(input string) (marketmodel.MarketplaceSource, error)
}

// AllowedSourcePolicy 校验来源是否允许安装（产品注入；Enterprise 默认 deny）。
type AllowedSourcePolicy interface {
	Check(ctx context.Context, source marketmodel.MarketplaceSource, product string) error
}

// CatalogReloader 安装后刷新运行时 Catalog；未注入则 Effective=next_turn。
type CatalogReloader interface {
	Reload(ctx context.Context) error
}

// Fetcher 把 marketplace source 拉取或导入到产品侧 cache，并返回 manifest。
// 若目录无 marketplace manifest 但可含 Skill，Manifest.Packages 可为空，由 Service Detect。
type Fetcher interface {
	Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)
	RemoveCache(ctx context.Context, record marketmodel.MarketplaceRecord) error
}

// InstallFromSourceRequest 是从 URL / github source / package@marketplace 安装的请求。
type InstallFromSourceRequest struct {
	SourceInput string
	Scope       marketmodel.InstallScope
	Force       bool
	Package     string
	SkillPath   string
	SkillPaths  []string
	AllowURL    bool
	Product     string
}

// InstallFromSourceResult 是 InstallFromSource 的结构化结果。
type InstallFromSourceResult struct {
	Records     []marketmodel.InstallRecord
	Skills      []string
	Specs       []string
	NeedsChoice bool
	Candidates  []string
	Effective   string
	Message     string
	FailureKind string
}

type FetchRequest struct {
	Source   marketmodel.MarketplaceSource
	Existing *marketmodel.MarketplaceRecord
	Refresh  bool
}

type FetchResult struct {
	Manifest        marketmodel.Manifest
	InstallLocation string
	LastRevision    string
	ContentHash     string
}

// Installer 负责把一个 package 安装到具体产品 scope。
type Installer interface {
	Install(ctx context.Context, req InstallRequest) (marketmodel.InstallRecord, error)
	Uninstall(ctx context.Context, record marketmodel.InstallRecord) error
}

type InstallRequest struct {
	Marketplace marketmodel.MarketplaceRecord
	Manifest    marketmodel.Manifest
	Package     marketmodel.Package
	Scope       marketmodel.InstallScope
	Force       bool
	Enabled     bool
}
