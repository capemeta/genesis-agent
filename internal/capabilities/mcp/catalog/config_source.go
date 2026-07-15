package catalog

import (
	"context"
	"fmt"
	"strings"

	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	profilemodel "genesis-agent/internal/capabilities/profile/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
	platformconfig "genesis-agent/internal/platform/config"
)

const precedenceConfig = 20

// ConfigSource 从 platform/config 的 mcp.servers 读取定义。
type ConfigSource struct {
	Config *platformconfig.Config
	Origin model.DefinitionOrigin
}

// NewConfigSource 创建配置来源（默认 Origin=config）。
func NewConfigSource(cfg *platformconfig.Config) *ConfigSource {
	return &ConfigSource{Config: cfg, Origin: model.OriginConfig}
}

func (s *ConfigSource) Precedence() int { return precedenceConfig }

func (s *ConfigSource) List(ctx context.Context, env contract.RuntimeCatalogEnv) ([]model.McpServerDefinition, error) {
	_ = env
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.Config == nil || !s.Config.MCP.Enabled {
		return nil, nil
	}
	origin := s.Origin
	if origin == "" {
		origin = model.OriginConfig
	}
	out := make([]model.McpServerDefinition, 0, len(s.Config.MCP.Servers))
	for name, dto := range s.Config.MCP.Servers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		domainCfg, err := MapServerDTO(name, dto, s.Config.MCP)
		if err != nil {
			return nil, err
		}
		def := model.McpServerDefinition{Config: domainCfg, Origin: origin}
		def.ConfigKey = ComputeConfigKey(def)
		out = append(out, def)
	}
	return out, nil
}

// MapServerDTO 将 YAML DTO 映射为领域模型。
func MapServerDTO(name string, dto platformconfig.MCPServerConfig, mcp platformconfig.MCPConfig) (model.McpServerConfig, error) {
	typ := model.McpTransportType(strings.TrimSpace(dto.Type))
	if typ == "" {
		typ = model.McpTransportStdio
	}
	if typ != model.McpTransportStdio && typ != model.McpTransportStreamableHTTP {
		return model.McpServerConfig{}, fmt.Errorf("mcp.servers.%s: 不支持的 type %q", name, dto.Type)
	}
	if typ == model.McpTransportStdio && strings.TrimSpace(dto.Command) == "" {
		return model.McpServerConfig{}, fmt.Errorf("mcp.servers.%s: stdio 需要 command", name)
	}
	if typ == model.McpTransportStreamableHTTP && strings.TrimSpace(dto.URL) == "" {
		return model.McpServerConfig{}, fmt.Errorf("mcp.servers.%s: streamable_http 需要 url", name)
	}
	if strings.TrimSpace(dto.BearerToken) != "" {
		return model.McpServerConfig{}, fmt.Errorf("mcp.servers.%s: 禁止 inline bearer_token，请使用 bearer_token_env 或 credential_ref", name)
	}

	enabled := true
	if dto.Enabled != nil {
		enabled = *dto.Enabled
	}
	cfg := model.McpServerConfig{
		Name:           name,
		Type:           typ,
		Enabled:        enabled,
		Required:       dto.Required,
		Command:        dto.Command,
		Args:           append([]string(nil), dto.Args...),
		Env:            copyMap(dto.Env),
		Cwd:            dto.Cwd,
		URL:            dto.URL,
		BearerTokenEnv: dto.BearerTokenEnv,
		CredentialRef:  dto.CredentialRef,
		Headers:        copyMap(dto.Headers),
		EnvHeaders:     copyMap(dto.EnvHeaders),
		EnabledTools:   append([]string(nil), dto.EnabledTools...),
		DisabledTools:  append([]string(nil), dto.DisabledTools...),
		ApprovalMode:   model.ApprovalMode(dto.ApprovalMode),
		Exposure:       tool.ToolExposure(dto.Exposure),
		Scope:          mapScope(dto.Scope),
	}
	if dto.StartupTimeout > 0 {
		cfg.StartupTimeout = dto.StartupTimeout
	}
	if dto.ToolTimeout > 0 {
		cfg.ToolTimeout = dto.ToolTimeout
	}
	cfg.Defaults(mcp.DefaultStartupTimeout, mcp.DefaultToolTimeout)
	return cfg, nil
}

func mapScope(dto platformconfig.MCPScopeConfig) profilemodel.CapabilityScope {
	scope := profilemodel.CapabilityScope{
		TenantIDs:  append([]string(nil), dto.TenantIDs...),
		ProjectIDs: append([]string(nil), dto.ProjectIDs...),
		AgentIDs:   append([]string(nil), dto.AgentIDs...),
		UserIDs:    append([]string(nil), dto.UserIDs...),
		RoleIDs:    append([]string(nil), dto.RoleIDs...),
	}
	for _, ch := range dto.Channels {
		scope.Channels = append(scope.Channels, profilemodel.ChannelType(ch))
	}
	for _, env := range dto.Environments {
		scope.Environments = append(scope.Environments, profilemodel.RuntimeEnvironment(env))
	}
	return scope
}

func copyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var _ contract.DefinitionSource = (*ConfigSource)(nil)
