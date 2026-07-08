// Package model 定义运行时 Capability Registry 的产品无关模型。
package model

import "time"

// CapabilityType 描述运行时原子能力类型。
type CapabilityType string

const (
	CapabilityTypeSkill         CapabilityType = "skill"
	CapabilityTypeSkillResource CapabilityType = "skill-resource"
	CapabilityTypeTool          CapabilityType = "tool"
	CapabilityTypeMCP           CapabilityType = "mcp"
	CapabilityTypeSubAgent      CapabilityType = "subagent"
	CapabilityTypeResource      CapabilityType = "resource"
)

// Permission 描述 Package 或 Capability 在安装前需要展示和治理的权限请求。
type Permission struct {
	Type        string         `json:"type"`
	Scope       string         `json:"scope,omitempty"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Signature 描述 Package 的供应链签名信息。验证策略由产品装配层注入。
type Signature struct {
	Algorithm string `json:"algorithm,omitempty"`
	Value     string `json:"value,omitempty"`
	KeyID     string `json:"key_id,omitempty"`
}

// CapabilityManifest 是 Package manifest 中声明的运行时能力。
type CapabilityManifest struct {
	Type        CapabilityType `json:"type"`
	Name        string         `json:"name"`
	Path        string         `json:"path,omitempty"`
	Description string         `json:"description,omitempty"`
	Entrypoint  string         `json:"entrypoint,omitempty"`
	Runtime     string         `json:"runtime,omitempty"`
	Products    []string       `json:"products,omitempty"`
	Permissions []Permission   `json:"permissions,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// CapabilityQuery 是运行时能力索引的过滤条件。
type CapabilityQuery struct {
	Query           string           `json:"query,omitempty"`
	Types           []CapabilityType `json:"types,omitempty"`
	PackageTypes    []string         `json:"package_types,omitempty"`
	Scope           string           `json:"scope,omitempty"`
	Product         string           `json:"product,omitempty"`
	IncludeDisabled bool             `json:"include_disabled,omitempty"`
}

// CapabilityIndexRecord 是安装态 Package 投影到运行时能力索引后的记录。
type CapabilityIndexRecord struct {
	ID               string            `json:"id"`
	Type             CapabilityType    `json:"type"`
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	Package          string            `json:"package"`
	PackageType      string            `json:"package_type"`
	Marketplace      string            `json:"marketplace"`
	Spec             string            `json:"spec"`
	Scope            string            `json:"scope"`
	Enabled          bool              `json:"enabled"`
	ResourcePath     string            `json:"resource_path,omitempty"`
	Entrypoint       string            `json:"entrypoint,omitempty"`
	Runtime          string            `json:"runtime,omitempty"`
	Products         []string          `json:"products,omitempty"`
	Permissions      []Permission      `json:"permissions,omitempty"`
	InstallRoot      string            `json:"install_root,omitempty"`
	SourceProvenance *SourceProvenance `json:"source_provenance,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at,omitempty"`
	ManifestMetadata map[string]any    `json:"manifest_metadata,omitempty"`
}

// SourceProvenance 是 Capability 索引用于审计来源的轻量投影，避免运行时能力域反向依赖 marketplace。
type SourceProvenance struct {
	Type                  string `json:"type"`
	Address               string `json:"address,omitempty"`
	Domain                string `json:"domain,omitempty"`
	Repo                  string `json:"repo,omitempty"`
	URL                   string `json:"url,omitempty"`
	Path                  string `json:"path,omitempty"`
	Ref                   string `json:"ref,omitempty"`
	SubPath               string `json:"sub_path,omitempty"`
	PackageSource         string `json:"package_source,omitempty"`
	Marketplace           string `json:"marketplace,omitempty"`
	MarketplaceSourcePath string `json:"marketplace_source_path,omitempty"`
	ResolvedRevision      string `json:"resolved_revision,omitempty"`
	ContentHash           string `json:"content_hash,omitempty"`
}
