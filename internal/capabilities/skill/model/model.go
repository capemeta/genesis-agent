// Package model 定义 Skill 能力的公共模型。
package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	profilemodel "genesis-agent/internal/capabilities/profile/model"
)

const (
	MaxNameLen             = 64
	MaxQualifiedNameLen    = 128
	MaxDescriptionLen      = 1024
	MaxPromptBytes         = 8 * 1024
	MaxAvailableSkillsSize = 8 * 1024
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Scope 描述 Skill 所属治理范围。
type Scope string

const (
	ScopeSystem  Scope = "system"
	ScopeAdmin   Scope = "admin"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopePlugin  Scope = "plugin"
	ScopeTenant  Scope = "tenant"
	ScopeOrg     Scope = "org"
	ScopeAgent   Scope = "agent"
	ScopeSession Scope = "session"
)

// SourceKind 描述 Skill 来源类型。
type SourceKind string

const (
	SourceKindHost         SourceKind = "host"
	SourceKindEmbedded     SourceKind = "embedded"
	SourceKindExecutor     SourceKind = "executor"
	SourceKindOrchestrator SourceKind = "orchestrator"
	SourceKindEnterpriseDB SourceKind = "enterprise_db"
	SourceKindCustom       SourceKind = "custom"
)

// Authority 是 Skill 来源的稳定身份。调用方必须通过同一 authority 读取资源。
type Authority struct {
	Kind SourceKind `json:"kind"`
	ID   string     `json:"id"`
}

func (a Authority) String() string {
	if a.ID == "" {
		return string(a.Kind)
	}
	return string(a.Kind) + ":" + a.ID
}

// PackageID 是不透明包 ID，调用方不能从中解析本地路径或 DB 主键后自行读取。
type PackageID string

// ResourceID 是 Skill 包内不透明资源 ID。
type ResourceID string

// ContextMode 描述 Skill 注入方式。
type ContextMode string

const (
	ContextModeInline ContextMode = "inline"
	ContextModeFork   ContextMode = "fork"
)

// Policy 描述 Skill 选择和可见性策略。
type Policy struct {
	AllowImplicitInvocation *bool                      `json:"allow_implicit_invocation,omitempty"`
	Products                []profilemodel.ChannelType `json:"products,omitempty"`
	DisableModelInvocation  bool                       `json:"disable_model_invocation,omitempty"`
}

// AllowsImplicitInvocation 返回是否允许隐式调用。
func (p Policy) AllowsImplicitInvocation() bool {
	if p.AllowImplicitInvocation == nil {
		return true
	}
	return *p.AllowImplicitInvocation
}

// MatchesProduct 判断 Skill 是否适用于当前产品。
func (p Policy) MatchesProduct(channel profilemodel.ChannelType) bool {
	if len(p.Products) == 0 {
		return true
	}
	for _, product := range p.Products {
		if product == channel {
			return true
		}
	}
	return false
}

// Interface 描述 UI 展示和默认提示。
type Interface struct {
	DisplayName      string `json:"display_name,omitempty"`
	ShortDescription string `json:"short_description,omitempty"`
	IconSmall        string `json:"icon_small,omitempty"`
	IconLarge        string `json:"icon_large,omitempty"`
	BrandColor       string `json:"brand_color,omitempty"`
	DefaultPrompt    string `json:"default_prompt,omitempty"`
}

// Dependencies 描述 Skill 依赖。
type Dependencies struct {
	Tools []ToolDependency `json:"tools,omitempty"`
}

// ToolDependency 描述工具、MCP、连接等依赖。
type ToolDependency struct {
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Transport   string `json:"transport,omitempty"`
	Command     string `json:"command,omitempty"`
	URL         string `json:"url,omitempty"`
}

// LocatorScheme 是显式 Skill 资源定位器前缀。
const LocatorScheme = "skill://"

// NormalizeResourceLocator 去掉 skill:// 前缀。ResourceID 本身仍保持 opaque；
// 该函数只服务于显式 mention/URI 匹配，不把 locator 解析为本地路径。
func NormalizeResourceLocator(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), LocatorScheme) {
		return value[len(LocatorScheme):]
	}
	return value
}

// Metadata 是发现阶段暴露给模型和 UI 的 Skill 摘要。
type Metadata struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	QualifiedName     string            `json:"qualified_name"`
	Description       string            `json:"description"`
	ShortDescription  string            `json:"short_description,omitempty"`
	Scope             Scope             `json:"scope"`
	Authority         Authority         `json:"authority"`
	PackageID         PackageID         `json:"package_id"`
	MainResource      ResourceID        `json:"main_resource"`
	DisplayPath       string            `json:"display_path,omitempty"`
	Version           string            `json:"version,omitempty"`
	Enabled           bool              `json:"enabled"`
	PromptVisible     bool              `json:"prompt_visible"`
	Policy            Policy            `json:"policy,omitempty"`
	Interface         Interface         `json:"interface,omitempty"`
	Dependencies      Dependencies      `json:"dependencies,omitempty"`
	AllowedTools      []string          `json:"allowed_tools,omitempty"`
	Context           ContextMode       `json:"context,omitempty"`
	Agent             string            `json:"agent,omitempty"`
	Model             string            `json:"model,omitempty"`
	MaxThinkingTokens int               `json:"max_thinking_tokens,omitempty"`
	SourceRef         map[string]string `json:"source_ref,omitempty"`
	UpdatedAt         time.Time         `json:"updated_at,omitempty"`
}

// Normalize 填充默认值。
func (m Metadata) Normalize() Metadata {
	if m.QualifiedName == "" {
		m.QualifiedName = m.Name
	}
	if m.ID == "" {
		m.ID = m.Authority.String() + ":" + string(m.PackageID)
	}
	if m.MainResource == "" {
		m.MainResource = ResourceID(string(m.PackageID) + "/SKILL.md")
	}
	if m.Context == "" {
		m.Context = ContextModeInline
	}
	if m.ShortDescription == "" {
		m.ShortDescription = m.Interface.ShortDescription
	}
	return m
}

// ValidateName 校验 Skill 名称。
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > MaxNameLen {
		return fmt.Errorf("skill name长度必须在1到%d之间", MaxNameLen)
	}
	if !skillNamePattern.MatchString(name) {
		return fmt.Errorf("skill name只能包含小写字母、数字和连字符，且不能以连字符开头或结尾")
	}
	return nil
}

// Error 描述来源加载中的非致命错误。
type Error struct {
	Source  Authority `json:"source"`
	Path    string    `json:"path,omitempty"`
	Message string    `json:"message"`
}

// Catalog 是某一上下文下的 Skill 快照。
type Catalog struct {
	Entries  []Metadata `json:"entries"`
	Errors   []Error    `json:"errors,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
}

// Injection 是加载 Skill 主体后的注入片段。
type Injection struct {
	Skill     Metadata   `json:"skill"`
	Resource  ResourceID `json:"resource"`
	Contents  string     `json:"contents"`
	Args      string     `json:"args,omitempty"`
	Truncated bool       `json:"truncated"`
}

// ResourceContent 是 Skill 包内资源读取结果。
type ResourceContent struct {
	Skill     Metadata   `json:"skill"`
	Resource  ResourceID `json:"resource"`
	Content   string     `json:"content"`
	Version   string     `json:"version,omitempty"`
	Truncated bool       `json:"truncated"`
}

// ResourceKind 描述 Skill 包内资源所属目录语义。
type ResourceKind string

const (
	ResourceKindReference ResourceKind = "reference"
	ResourceKindScript    ResourceKind = "script"
	ResourceKindAsset     ResourceKind = "asset"
)

// ResourceInfo 是 Skill 包内资源的可发现元数据。
type ResourceInfo struct {
	Resource ResourceID   `json:"resource"`
	Kind     ResourceKind `json:"kind"`
	Name     string       `json:"name"`
	Size     int64        `json:"size,omitempty"`
	Text     bool         `json:"text"`
}

// ResourceList 是某个 Skill 包内可发现资源清单。
type ResourceList struct {
	Skill     Metadata       `json:"skill"`
	Resources []ResourceInfo `json:"resources"`
}

// SearchResult 是 Skill 资源搜索结果。
type SearchResult struct {
	Skill   Metadata      `json:"skill"`
	Matches []SearchMatch `json:"matches"`
}

// SearchMatch 描述 Skill 内资源搜索结果。
type SearchMatch struct {
	Resource ResourceID `json:"resource"`
	Title    string     `json:"title"`
	Snippet  string     `json:"snippet"`
}
