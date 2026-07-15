// Package model 定义 Package Marketplace 与运行时 Capability 投影的产品无关模型。
package model

import (
	"fmt"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"net/url"
	"strings"
	"time"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

// SourceType 描述 marketplace 的分发来源类型。
type SourceType string

const (
	SourceTypeGitHub     SourceType = "github"
	SourceTypeGit        SourceType = "git"
	SourceTypeURL        SourceType = "url"
	SourceTypeFile       SourceType = "file"
	SourceTypeDirectory  SourceType = "directory"
	SourceTypeEnterprise SourceType = "enterprise"
)

// PackageType 描述 marketplace 中可安装包的产品语义。
type PackageType string

const (
	PackageTypeSkillPackage PackageType = "skill-package"
	PackageTypePlugin       PackageType = "plugin"
	PackageTypeToolPackage  PackageType = "tool-package"
	PackageTypeMCPPackage   PackageType = "mcp-package"
	PackageTypeSubAgent     PackageType = "subagent-package"
)

// MarketplaceSource 是可刷新、可审计的分发来源描述。
type MarketplaceSource struct {
	Type    SourceType        `json:"type"`
	Host    string            `json:"host,omitempty"` // github 兼容主机，空则 github.com
	Repo    string            `json:"repo,omitempty"`
	URL     string            `json:"url,omitempty"`
	Path    string            `json:"path,omitempty"`
	Ref     string            `json:"ref,omitempty"`
	SubPath string            `json:"sub_path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Owner 描述 marketplace 发布方。
type Owner struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Manifest 描述一个 marketplace 暴露的可安装 Package 集合。
type Manifest struct {
	Schema      string         `json:"$schema,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Version     string         `json:"version,omitempty"`
	Owner       Owner          `json:"owner,omitempty"`
	Packages    []Package      `json:"packages,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Package 是 marketplace 中的最小安装单元；Plugin 也是一种组合 Package。
type Package struct {
	Name         string                        `json:"name"`
	Type         PackageType                   `json:"type"`
	Description  string                        `json:"description,omitempty"`
	Version      string                        `json:"version,omitempty"`
	Source       string                        `json:"source,omitempty"`
	Capabilities []capmodel.CapabilityManifest `json:"capabilities,omitempty"`
	Commands     []string                      `json:"commands,omitempty"`
	Dependencies skillmodel.Dependencies       `json:"dependencies,omitempty"`
	Products     []string                      `json:"products,omitempty"`
	Permissions  []capmodel.Permission         `json:"permissions,omitempty"`
	License      string                        `json:"license,omitempty"`
	Signature    *capmodel.Signature           `json:"signature,omitempty"`
	Metadata     map[string]any                `json:"metadata,omitempty"`
}

// ProductPersistenceProfile 描述产品侧持久化协议，不绑定具体 DB 实现。
type ProductPersistenceProfile struct {
	Product          string `json:"product"`
	Driver           string `json:"driver"`
	SchemaVersion    string `json:"schema_version,omitempty"`
	SupportsTenant   bool   `json:"supports_tenant,omitempty"`
	SupportsProjects bool   `json:"supports_projects,omitempty"`
}

// ProductCapabilityProtocol 描述 CLI/Desktop/Enterprise 复用 Package 与 Capability 服务时需要提供的装配协议。
type ProductCapabilityProtocol struct {
	Product      string                    `json:"product"`
	Scopes       []InstallScope            `json:"scopes,omitempty"`
	Persistence  ProductPersistenceProfile `json:"persistence"`
	DefaultScope InstallScope              `json:"default_scope,omitempty"`
}

// MarketplaceRecord 是产品侧持久化的 marketplace 配置与 cache 状态。
type MarketplaceRecord struct {
	Name            string            `json:"name"`
	Source          MarketplaceSource `json:"source"`
	InstallLocation string            `json:"install_location"`
	LastUpdated     time.Time         `json:"last_updated"`
	LastRevision    string            `json:"last_revision,omitempty"`
	AutoUpdate      bool              `json:"auto_update,omitempty"`
}

// InstallScope 描述安装目标范围。
type InstallScope string

const (
	InstallScopeUser    InstallScope = "user"
	InstallScopeProject InstallScope = "project"
	InstallScopeTenant  InstallScope = "tenant"
	InstallScopeOrg     InstallScope = "org"
	InstallScopeRole    InstallScope = "role"
)

// InstallRecord 记录某个 Package 的本地安装状态。
type InstallRecord struct {
	Package               string                           `json:"package"`
	PackageType           PackageType                      `json:"package_type"`
	Marketplace           string                           `json:"marketplace"`
	Spec                  string                           `json:"spec"`
	Scope                 InstallScope                     `json:"scope"`
	Enabled               bool                             `json:"enabled"`
	ProjectPath           string                           `json:"project_path,omitempty"`
	InstalledAt           time.Time                        `json:"installed_at"`
	UpdatedAt             time.Time                        `json:"updated_at,omitempty"`
	Version               string                           `json:"version,omitempty"`
	Skills                []string                         `json:"skills,omitempty"`
	SkillRoots            []string                         `json:"skill_roots,omitempty"`
	Commands              []string                         `json:"commands,omitempty"`
	Capabilities          []capmodel.CapabilityIndexRecord `json:"capabilities,omitempty"`
	SourceMarketplacePath string                           `json:"source_marketplace_path,omitempty"`
	SourceProvenance      *SourceProvenance                `json:"source_provenance,omitempty"`
	InstallRoot           string                           `json:"install_root,omitempty"`
	ContentHash           string                           `json:"content_hash,omitempty"`
}

// SourceProvenance 是安装时的来源快照，用于供应链审计和后续排查。
type SourceProvenance struct {
	Type                  SourceType `json:"type"`
	Address               string     `json:"address,omitempty"`
	Domain                string     `json:"domain,omitempty"`
	Repo                  string     `json:"repo,omitempty"`
	URL                   string     `json:"url,omitempty"`
	Path                  string     `json:"path,omitempty"`
	Ref                   string     `json:"ref,omitempty"`
	SubPath               string     `json:"sub_path,omitempty"`
	PackageSource         string     `json:"package_source,omitempty"`
	Marketplace           string     `json:"marketplace,omitempty"`
	MarketplaceSourcePath string     `json:"marketplace_source_path,omitempty"`
	ResolvedRevision      string     `json:"resolved_revision,omitempty"`
	ContentHash           string     `json:"content_hash,omitempty"`
}

// CatalogCard 是 Package Marketplace 列表视图的产品无关条目。
type CatalogCard struct {
	Package      Package      `json:"package"`
	Marketplace  string       `json:"marketplace"`
	Installed    bool         `json:"installed"`
	Enabled      bool         `json:"enabled"`
	InstallScope InstallScope `json:"install_scope,omitempty"`
	Availability string       `json:"availability,omitempty"`
	Warnings     []string     `json:"warnings,omitempty"`
}

// PackageView 是 Package 管理界面/命令使用的聚合视图。
type PackageView struct {
	Install      InstallRecord                    `json:"install"`
	Capabilities []capmodel.CapabilityIndexRecord `json:"capabilities,omitempty"`
}

// PluginView 是 Plugin 管理界面/命令使用的聚合视图。
type PluginView = PackageView

// PackageSpec 返回 <package>@<marketplace>。
func PackageSpec(pkg, marketplace string) string {
	return strings.TrimSpace(pkg) + "@" + strings.TrimSpace(marketplace)
}

// SplitPackageSpec 解析 <package>@<marketplace>。当 marketplace 为空时由调用方解析唯一匹配。
func SplitPackageSpec(spec string) (pkg string, marketplace string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", fmt.Errorf("package spec不能为空")
	}
	parts := strings.Split(spec, "@")
	if len(parts) > 2 {
		return "", "", fmt.Errorf("package spec格式应为 <package> 或 <package>@<marketplace>")
	}
	pkg = strings.TrimSpace(parts[0])
	if pkg == "" {
		return "", "", fmt.Errorf("package name不能为空")
	}
	if err := skillmodel.ValidateName(pkg); err != nil {
		return "", "", err
	}
	if len(parts) == 2 {
		marketplace = strings.TrimSpace(parts[1])
		if marketplace == "" {
			return "", "", fmt.Errorf("marketplace name不能为空")
		}
	}
	return pkg, marketplace, nil
}

func NewSourceProvenance(record MarketplaceRecord, pkg Package, resolvedRevision, contentHash string) SourceProvenance {
	source := record.Source
	address := SourceAddress(source)
	return SourceProvenance{
		Type:                  source.Type,
		Address:               address,
		Domain:                SourceDomain(source),
		Repo:                  source.Repo,
		URL:                   source.URL,
		Path:                  source.Path,
		Ref:                   source.Ref,
		SubPath:               source.SubPath,
		PackageSource:         pkg.Source,
		Marketplace:           record.Name,
		MarketplaceSourcePath: address,
		ResolvedRevision:      resolvedRevision,
		ContentHash:           contentHash,
	}
}

func SourceAddress(source MarketplaceSource) string {
	switch source.Type {
	case SourceTypeGitHub:
		if source.Repo == "" {
			return ""
		}
		host := strings.TrimSpace(source.Host)
		if host == "" {
			host = "github.com"
		}
		address := "https://" + host + "/" + source.Repo
		if source.Ref != "" {
			address += "@" + source.Ref
		}
		if source.SubPath != "" {
			address += "#" + source.SubPath
		}
		return address
	case SourceTypeGit, SourceTypeURL:
		return source.URL
	case SourceTypeFile, SourceTypeDirectory:
		return source.Path
	default:
		return firstNonEmpty(source.URL, source.Repo, source.Path, string(source.Type))
	}
}

func SourceDomain(source MarketplaceSource) string {
	switch source.Type {
	case SourceTypeGitHub:
		host := strings.TrimSpace(source.Host)
		if host == "" {
			return "github.com"
		}
		return strings.ToLower(host)
	case SourceTypeGit, SourceTypeURL:
		if parsed, err := url.Parse(source.URL); err == nil {
			return strings.ToLower(parsed.Hostname())
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
