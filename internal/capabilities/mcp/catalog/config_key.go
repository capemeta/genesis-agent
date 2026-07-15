package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"genesis-agent/internal/capabilities/mcp/model"
)

// ComputeConfigKey 生成配置指纹（用于检测同名 server 配置变更）。
func ComputeConfigKey(def model.McpServerDefinition) string {
	payload := struct {
		Name           string            `json:"name"`
		Type           string            `json:"type"`
		Command        string            `json:"command,omitempty"`
		Args           []string          `json:"args,omitempty"`
		Env            map[string]string `json:"env,omitempty"`
		Cwd            string            `json:"cwd,omitempty"`
		URL            string            `json:"url,omitempty"`
		BearerTokenEnv string            `json:"bearer_token_env,omitempty"`
		CredentialRef  string            `json:"credential_ref,omitempty"`
		Headers        map[string]string `json:"headers,omitempty"`
		EnvHeaders     map[string]string `json:"env_headers,omitempty"`
		Enabled        bool              `json:"enabled"`
		Required       bool              `json:"required"`
		EnabledTools   []string          `json:"enabled_tools,omitempty"`
		DisabledTools  []string          `json:"disabled_tools,omitempty"`
		Origin         string            `json:"origin"`
	}{
		Name:           def.Config.Name,
		Type:           string(def.Config.Type),
		Command:        def.Config.Command,
		Args:           def.Config.Args,
		Env:            def.Config.Env,
		Cwd:            def.Config.Cwd,
		URL:            def.Config.URL,
		BearerTokenEnv: def.Config.BearerTokenEnv,
		CredentialRef:  def.Config.CredentialRef,
		Headers:        def.Config.Headers,
		EnvHeaders:     def.Config.EnvHeaders,
		Enabled:        def.Config.Enabled,
		Required:       def.Config.Required,
		EnabledTools:   def.Config.EnabledTools,
		DisabledTools:  def.Config.DisabledTools,
		Origin:         string(def.Origin),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%s:fallback", def.Config.Name)
	}
	sum := sha256.Sum256(raw)
	return def.Config.Name + ":" + hex.EncodeToString(sum[:8])
}
