// Package contract 定义通用 Capability Registry 与运行时适配端口。
package contract

import (
	"context"
	capmodel "genesis-agent/internal/capabilities/capability/model"
)

// RegistryStore 管理已安装 Package 投影出的运行时能力索引。
type RegistryStore interface {
	List(ctx context.Context) ([]capmodel.CapabilityIndexRecord, error)
	PutPackageCapabilities(ctx context.Context, spec string, records []capmodel.CapabilityIndexRecord) error
	SetPackageEnabled(ctx context.Context, spec string, enabled bool) error
	SetCapabilityEnabled(ctx context.Context, id string, enabled bool) (capmodel.CapabilityIndexRecord, bool, error)
	DeletePackage(ctx context.Context, spec string) error
}

// Registry 提供运行时能力查询与启停能力，不关心 Package 来源实现。
type Registry interface {
	ListCapabilities(ctx context.Context, query capmodel.CapabilityQuery) ([]capmodel.CapabilityIndexRecord, error)
	SetCapabilityEnabled(ctx context.Context, id string, enabled bool) (capmodel.CapabilityIndexRecord, error)
}

// RuntimeAdapter 是非 Skill 能力接入实际运行时的架构端口。
type RuntimeAdapter interface {
	CapabilityType() capmodel.CapabilityType
	Register(ctx context.Context, capability capmodel.CapabilityIndexRecord) error
	Unregister(ctx context.Context, capability capmodel.CapabilityIndexRecord) error
	SetEnabled(ctx context.Context, capability capmodel.CapabilityIndexRecord, enabled bool) error
}

// RuntimeAdapterRegistry 保存 Tool/MCP/SubAgent 等 runtime adapter。
type RuntimeAdapterRegistry interface {
	RegisterAdapter(adapter RuntimeAdapter) error
	AdapterFor(typ capmodel.CapabilityType) (RuntimeAdapter, bool)
}
