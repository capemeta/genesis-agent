package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadLLMProvidersModelsRouter(t *testing.T) {
	t.Setenv("QWEN_API_KEY", "qwen-key")

	dir := t.TempDir()
	content := `
llm:
  providers:
    qwen:
      base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
      auth:
        type: api_key
        api_key: ${QWEN_API_KEY}
    openai:
      base_url: https://api.openai.com/v1
      auth:
        type: api_key
        api_key: ${GENESIS_TEST_OPENAI_MISSING_API_KEY}
  models:
    fast:
      provider: qwen
      model: qwen-turbo
      strategy: low_cost
    default:
      provider: qwen
      model: qwen-plus
      strategy: balanced
    reasoning:
      provider: openai
      model: gpt-4.1
      strategy: high_quality
      timeout: 180s
  router:
    tool_call: fast
    chat: default
    planning: reasoning
agent:
  max_iterations: 10
  system_prompt: test
log:
  level: info
server:
  host: 127.0.0.1
  port: 8080
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	chat, err := cfg.LLM.ResolveRoute("chat")
	if err != nil {
		t.Fatalf("ResolveRoute(chat) error = %v", err)
	}
	if chat.Alias != "default" || chat.ProviderName != "qwen" || chat.APIKey != "qwen-key" {
		t.Fatalf("chat route = %+v", chat)
	}

	planning, err := cfg.LLM.ResolveRoute("planning")
	if err != nil {
		t.Fatalf("ResolveRoute(planning) error = %v", err)
	}
	if planning.ProviderName != "openai" || planning.APIKey != "" {
		t.Fatalf("planning route should allow empty non-created provider key: %+v", planning)
	}
	if planning.Timeout != 180*time.Second {
		t.Fatalf("planning timeout = %s, want 180s", planning.Timeout)
	}

	unknown, err := cfg.LLM.ResolveRoute("unknown")
	if err != nil {
		t.Fatalf("ResolveRoute(unknown) error = %v", err)
	}
	if unknown.Alias != "default" {
		t.Fatalf("unknown route alias = %q, want default", unknown.Alias)
	}
}

func TestLoadLLMLegacyAPIKeyFallback(t *testing.T) {
	dir := t.TempDir()
	content := `
llm:
  providers:
    qwen:
      type: openai
      base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
      auth:
        type: api_key
  models:
    default:
      provider: qwen
      model: qwen-plus
      strategy: balanced
  router:
    chat: default
agent:
  max_iterations: 10
  system_prompt: test
log:
  level: info
server:
  host: 127.0.0.1
  port: 8080
`
	local := `
llm:
  api_key: legacy-key
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.local.yaml"), []byte(local), 0644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	chat, err := cfg.LLM.ResolveRoute("chat")
	if err != nil {
		t.Fatalf("ResolveRoute(chat) error = %v", err)
	}
	if chat.APIKey != "legacy-key" {
		t.Fatalf("APIKey = %q, want legacy-key", chat.APIKey)
	}
}

func TestLoadHTTPClientDefaults(t *testing.T) {
	dir := t.TempDir()
	content := `
llm:
  providers:
    qwen:
      base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
      auth:
        type: api_key
        api_key: ${GENESIS_TEST_QWEN_KEY}
  models:
    default:
      provider: qwen
      model: qwen-plus
      strategy: balanced
  router:
    chat: default
agent:
  max_iterations: 10
  system_prompt: test
log:
  level: info
server:
  host: 127.0.0.1
  port: 8080
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPClient.DefaultTimeout != 30*time.Second {
		t.Fatalf("DefaultTimeout = %s, want 30s", cfg.HTTPClient.DefaultTimeout)
	}
	if cfg.HTTPClient.MaxResponseBodyBytes != 4<<20 {
		t.Fatalf("MaxResponseBodyBytes = %d, want %d", cfg.HTTPClient.MaxResponseBodyBytes, 4<<20)
	}
	if cfg.HTTPClient.Retry.MaxAttempts != 3 {
		t.Fatalf("Retry.MaxAttempts = %d, want 3", cfg.HTTPClient.Retry.MaxAttempts)
	}
}

func TestLoadPolicyDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig("", "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Policy.Defaults.Unknown != "ask" || cfg.Policy.Defaults.Critical != "deny" {
		t.Fatalf("policy defaults = %+v", cfg.Policy.Defaults)
	}
	if cfg.Policy.Files.Workspace.Write != "allow" || cfg.Policy.Files.External.Delete != "deny" {
		t.Fatalf("file policy defaults = %+v", cfg.Policy.Files)
	}
	if cfg.Policy.Commands.Default != "ask" || cfg.Policy.Web.Fetch.Default != "ask" || cfg.Policy.Sandbox.DefaultMode != "disabled" {
		t.Fatalf("reserved policy defaults = %+v", cfg.Policy)
	}
}

func TestLoadPolicyFromYAML(t *testing.T) {
	dir := t.TempDir()
	policy := `
policy:
  defaults:
    unknown: deny
    allowed_grant_scopes: [once, session]
  files:
    workspace:
      write: ask
    external:
      read: deny
    allow_paths:
      - path: D:\tmp
        operations: [read, list]
  sandbox:
    default_mode: optional
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig(policy, "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Policy.Defaults.Unknown != "deny" {
		t.Fatalf("Unknown = %q, want deny", cfg.Policy.Defaults.Unknown)
	}
	if cfg.Policy.Files.Workspace.Write != "ask" || cfg.Policy.Files.External.Read != "deny" {
		t.Fatalf("file overrides = %+v", cfg.Policy.Files)
	}
	if len(cfg.Policy.Files.AllowPaths) != 1 || cfg.Policy.Files.AllowPaths[0].Path != `D:\tmp` {
		t.Fatalf("allow paths = %+v", cfg.Policy.Files.AllowPaths)
	}
	if cfg.Policy.Sandbox.DefaultMode != "optional" {
		t.Fatalf("sandbox default mode = %q", cfg.Policy.Sandbox.DefaultMode)
	}
}

func TestLoadPolicyRejectsInvalidDecision(t *testing.T) {
	dir := t.TempDir()
	policy := `
policy:
  files:
    workspace:
      write: maybe
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig(policy, "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(dir); err == nil {
		t.Fatal("Load() expected invalid policy decision error")
	}
}

func TestLoadPolicyRejectsTenantGlobalGrantScope(t *testing.T) {
	dir := t.TempDir()
	policy := `
policy:
  defaults:
    allowed_grant_scopes: [once, tenant]
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig(policy, "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(dir); err == nil {
		t.Fatal("Load() expected invalid grant scope error")
	}
}

func TestLoadSecretsDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig("", "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Secrets.DataDir != "data" || cfg.Secrets.MasterKeyEnv != "GENESIS_MASTER_KEY" {
		t.Fatalf("secrets defaults = %+v", cfg.Secrets)
	}
}

func TestLoadSandboxDefaultsDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig("", "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Sandbox.Enabled || cfg.Sandbox.Mode != "local_host" || cfg.Sandbox.DefaultExecution != "disabled" {
		t.Fatalf("sandbox defaults = %+v", cfg.Sandbox)
	}
	if cfg.Sandbox.APIKeyEnv != "GENESIS_SANDBOX_API_KEY" || cfg.Sandbox.DefaultRuntimeProfile != "code-polyglot-basic" {
		t.Fatalf("sandbox defaults = %+v", cfg.Sandbox)
	}
}

func TestLoadSandboxExternalConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GENESIS_TEST_SANDBOX_KEY", "sandbox-key")
	content := minimalConfig("", "") + `
sandbox:
  enabled: true
  mode: docker_sandbox
  default_execution: optional
  allow_session_override: true
  base_url: http://127.0.0.1:18010
  api_key_env: GENESIS_TEST_SANDBOX_KEY
  workspace_id: dev-workspace
  default_runtime_profile: code-polyglot-basic
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Sandbox.Enabled || cfg.Sandbox.Mode != "docker_sandbox" || cfg.Sandbox.APIKey != "sandbox-key" {
		t.Fatalf("sandbox config = %+v", cfg.Sandbox)
	}
	if cfg.Sandbox.WorkspaceID != "dev-workspace" || cfg.Sandbox.DefaultExecution != "optional" {
		t.Fatalf("sandbox config = %+v", cfg.Sandbox)
	}
}

func TestLoadSandboxExternalRequiresBaseURL(t *testing.T) {
	dir := t.TempDir()
	content := minimalConfig("", "") + `
sandbox:
  enabled: true
  mode: remote_sandbox
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(dir); err == nil {
		t.Fatal("Load() expected sandbox base_url validation error")
	}
}

func TestLoadProductUserConfigOverridesLocal(t *testing.T) {
	dir := t.TempDir()
	configHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig("", "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	local := `
web:
  tavily_api_key: local-key
`
	if err := os.WriteFile(filepath.Join(dir, "config.local.yaml"), []byte(local), 0644); err != nil {
		t.Fatalf("write local config: %v", err)
	}
	userDir := filepath.Join(configHome, "cli")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("mkdir user config: %v", err)
	}
	user := `
web:
  tavily_api_key: user-key
skills:
  sources:
    - scope: user
      path: ${GENESIS_TEST_SKILL_ROOT}
`
	t.Setenv("GENESIS_TEST_SKILL_ROOT", `D:\skills`)
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte(user), 0644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: configHome})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if cfg.Web.TavilyAPIKey != "user-key" {
		t.Fatalf("tavily key = %q, want user-key", cfg.Web.TavilyAPIKey)
	}
	if len(cfg.Skills.Sources) != 1 || cfg.Skills.Sources[0].Path != `D:\skills` {
		t.Fatalf("skill sources = %+v", cfg.Skills.Sources)
	}
}

func TestLoadAgentEnvOverridesUserConfig(t *testing.T) {
	dir := t.TempDir()
	configHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig("", "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	userDir := filepath.Join(configHome, "cli")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("mkdir user config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte("web:\n  brave_api_key: user-key\n"), 0644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	t.Setenv("AGENT_WEB_BRAVE_API_KEY", "agent-env-key")

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: configHome})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if cfg.Web.BraveAPIKey != "agent-env-key" {
		t.Fatalf("brave key = %q, want agent-env-key", cfg.Web.BraveAPIKey)
	}
}

func TestLoadWebEnvPlaceholders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAVILY_API_KEY", "script-env-key")
	content := minimalConfig("", "") + `
web:
  tavily_api_key: ${TAVILY_API_KEY}
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: t.TempDir()})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if cfg.Web.TavilyAPIKey != "script-env-key" {
		t.Fatalf("tavily key = %q, want script-env-key", cfg.Web.TavilyAPIKey)
	}
}
func TestLoadEnsuresProductUserConfig(t *testing.T) {
	dir := t.TempDir()
	configHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig("", "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: configHome, EnsureUserConfig: true})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadWithOptions() returned nil config")
	}
	if _, err := os.Stat(filepath.Join(configHome, "cli", "config.yaml")); err != nil {
		t.Fatalf("cli user config was not created: %v", err)
	}
	if info, err := os.Stat(filepath.Join(configHome, "cli", "skills")); err != nil || !info.IsDir() {
		t.Fatalf("cli skills dir was not created: info=%v err=%v", info, err)
	}
}

func TestLoadEnsuresDesktopUserConfig(t *testing.T) {
	dir := t.TempDir()
	configHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig("", "")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadWithOptions(dir, LoadOptions{Product: "desktop", ConfigHome: configHome, EnsureUserConfig: true})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(configHome, "desktop", "config.yaml")); err != nil {
		t.Fatalf("desktop user config was not created: %v", err)
	}
	if info, err := os.Stat(filepath.Join(configHome, "desktop", "skills")); err != nil || !info.IsDir() {
		t.Fatalf("desktop skills dir was not created: info=%v err=%v", info, err)
	}
}
func minimalConfig(extraPolicy string, extraSecrets string) string {
	return `
llm:
  providers:
    qwen:
      base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
      auth:
        type: api_key
        api_key: ${GENESIS_TEST_QWEN_KEY}
  models:
    default:
      provider: qwen
      model: qwen-plus
      strategy: balanced
  router:
    chat: default
` + extraSecrets + extraPolicy + `
agent:
  max_iterations: 10
  system_prompt: test
log:
  level: info
server:
  host: 127.0.0.1
  port: 8080
`
}
