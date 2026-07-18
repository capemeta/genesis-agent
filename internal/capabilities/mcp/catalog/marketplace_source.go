package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	capcontract "genesis-agent/internal/capabilities/capability/contract"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"genesis-agent/internal/capabilities/mcp/contract"
	"genesis-agent/internal/capabilities/mcp/model"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

const precedenceMarketplace = 50

// MarketplaceSource 从 capability 索引读取已安装 MCP package。
type MarketplaceSource struct {
	Registry capcontract.Registry
}

// NewMarketplaceSource 创建 marketplace 来源。
func NewMarketplaceSource(registry capcontract.Registry) *MarketplaceSource {
	return &MarketplaceSource{Registry: registry}
}

func (s *MarketplaceSource) Precedence() int { return precedenceMarketplace }

func (s *MarketplaceSource) List(ctx context.Context, env contract.RuntimeCatalogEnv) ([]model.McpServerDefinition, error) {
	_ = env
	if s == nil || s.Registry == nil {
		return nil, nil
	}
	records, err := s.Registry.ListCapabilities(ctx, capmodel.CapabilityQuery{
		Types:           []capmodel.CapabilityType{capmodel.CapabilityTypeMCP},
		IncludeDisabled: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]model.McpServerDefinition, 0, len(records))
	for _, rec := range records {
		def, err := DefinitionFromRecord(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	return out, nil
}

// DefinitionFromRecord 将 capability 索引记录投影为 MCP server 定义。
func DefinitionFromRecord(rec capmodel.CapabilityIndexRecord) (model.McpServerDefinition, error) {
	name := strings.TrimSpace(rec.Name)
	if name == "" {
		name = strings.TrimSpace(rec.ID)
	}
	if name == "" {
		return model.McpServerDefinition{}, fmt.Errorf("mcp capability 缺少 name/id")
	}
	cfg := model.McpServerConfig{
		Name:     name,
		Type:     model.McpTransportStdio,
		Enabled:  rec.Enabled,
		Command:  strings.TrimSpace(rec.Entrypoint),
		Exposure: tool.ToolExposureDirect,
	}
	if len(rec.ManifestMetadata) > 0 {
		raw, err := json.Marshal(rec.ManifestMetadata)
		if err == nil {
			var meta marketplaceMCPMeta
			if err := json.Unmarshal(raw, &meta); err == nil {
				if meta.Type != "" {
					cfg.Type = model.McpTransportType(meta.Type)
				}
				if meta.Placement != "" {
					cfg.Placement = model.McpPlacement(meta.Placement)
				}
				cfg.InheritEnv = append([]string(nil), meta.InheritEnv...)
				if len(meta.Args) > 0 {
					cfg.Args = append([]string(nil), meta.Args...)
				}
				if len(meta.Env) > 0 {
					cfg.Env = copyMap(meta.Env)
				}
				if meta.URL != "" {
					cfg.URL = meta.URL
				}
				if meta.BearerTokenEnv != "" {
					cfg.BearerTokenEnv = meta.BearerTokenEnv
				}
				if meta.CredentialRef != "" {
					cfg.CredentialRef = meta.CredentialRef
				}
				if meta.Cwd != "" {
					cfg.Cwd = meta.Cwd
				}
				if meta.Exposure != "" {
					cfg.Exposure = tool.ToolExposure(meta.Exposure)
				}
			}
		}
	}
	if cfg.Type == model.McpTransportStdio && cfg.Placement == "" {
		return model.McpServerDefinition{}, fmt.Errorf("mcp capability %s: stdio 必须显式声明 placement", name)
	}
	if cfg.Type == model.McpTransportStreamableHTTP && cfg.Placement == "" {
		cfg.Placement = model.McpPlacementStreamableHTTP
	}
	cfg.Defaults(0, 0)
	def := model.McpServerDefinition{Config: cfg, Origin: model.OriginMarketplace}
	def.ConfigKey = ComputeConfigKey(def)
	return def, nil
}

type marketplaceMCPMeta struct {
	Type           string            `json:"type"`
	Placement      string            `json:"placement"`
	InheritEnv     []string          `json:"inherit_env"`
	Args           []string          `json:"args"`
	Env            map[string]string `json:"env"`
	URL            string            `json:"url"`
	BearerTokenEnv string            `json:"bearer_token_env"`
	CredentialRef  string            `json:"credential_ref"`
	Cwd            string            `json:"cwd"`
	Exposure       string            `json:"exposure"`
}

var _ contract.DefinitionSource = (*MarketplaceSource)(nil)
