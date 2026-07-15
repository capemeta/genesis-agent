package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadLLMProvidersModelsRouter(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("QWEN_API_KEY", "qwen-key")

	dir := t.TempDir()
	llmContent := `
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
`
	configContent := `
agent:
  max_iterations: 10
  system_prompt: test
log:
  level: info
server:
  host: 127.0.0.1
  port: 8080
`
	writeTestConfig(t, dir, configContent, llmContent)

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: t.TempDir()})
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

func TestLoadLocalOverridesLLMProviderKey(t *testing.T) {
	dir := t.TempDir()
	local := `
llm:
  providers:
    qwen:
      auth:
        api_key: local-key
`
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())
	if err := os.WriteFile(filepath.Join(dir, "config.local.yaml"), []byte(local), 0644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: t.TempDir()})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	chat, err := cfg.LLM.ResolveRoute("chat")
	if err != nil {
		t.Fatalf("ResolveRoute(chat) error = %v", err)
	}
	if chat.APIKey != "local-key" {
		t.Fatalf("APIKey = %q, want local-key", chat.APIKey)
	}
}

func TestLoadHTTPClientDefaults(t *testing.T) {
	dir := t.TempDir()
	content := `
agent:
  max_iterations: 10
  system_prompt: test
log:
  level: info
server:
  host: 127.0.0.1
  port: 8080
`
	writeTestConfig(t, dir, content, minimalLLMConfig())

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
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())

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
	writeTestConfig(t, dir, minimalConfig(policy, ""), minimalLLMConfig())

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
	writeTestConfig(t, dir, minimalConfig(policy, ""), minimalLLMConfig())

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
	writeTestConfig(t, dir, minimalConfig(policy, ""), minimalLLMConfig())

	if _, err := Load(dir); err == nil {
		t.Fatal("Load() expected invalid grant scope error")
	}
}

func TestLoadSecretsDefaults(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())

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
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())

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
	writeTestConfig(t, dir, content, minimalLLMConfig())

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
	writeTestConfig(t, dir, content, minimalLLMConfig())

	if _, err := Load(dir); err == nil {
		t.Fatal("Load() expected sandbox base_url validation error")
	}
}

func TestLoadProjectLocalOverridesProjectAndUserConfig(t *testing.T) {
	dir := t.TempDir()
	configHome := t.TempDir()
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())
	local := `
web:
  tavily_api_key: local-key
llm:
  providers:
    qwen:
      auth:
        api_key: local-key
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
llm:
  providers:
    qwen:
      auth:
        api_key: user-key
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
	if cfg.Web.TavilyAPIKey != "local-key" {
		t.Fatalf("tavily key = %q, want local-key", cfg.Web.TavilyAPIKey)
	}
	chat, err := cfg.LLM.ResolveRoute("chat")
	if err != nil {
		t.Fatalf("ResolveRoute(chat) error = %v", err)
	}
	if chat.APIKey != "local-key" {
		t.Fatalf("LLM API key = %q, want local-key", chat.APIKey)
	}
	if len(cfg.Skills.Sources) != 1 || cfg.Skills.Sources[0].Path != `D:\skills` {
		t.Fatalf("skill sources = %+v", cfg.Skills.Sources)
	}
}

func TestLoadProjectConfigOverridesUserConfig(t *testing.T) {
	dir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("GENESIS_TEST_QWEN_KEY", "project-key")
	project := minimalConfig("", "") + `
web:
  tavily_api_key: project-key
skills:
  sources: []
`
	writeTestConfig(t, dir, project, minimalLLMConfig())

	userDir := filepath.Join(configHome, "cli")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("mkdir user config: %v", err)
	}
	user := `
web:
  tavily_api_key: user-key
llm:
  providers:
    qwen:
      auth:
        api_key: user-key
skills:
  sources:
    - scope: user
      path: D:\skills
`
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte(user), 0644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: configHome})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if cfg.Web.TavilyAPIKey != "project-key" {
		t.Fatalf("tavily key = %q, want project-key", cfg.Web.TavilyAPIKey)
	}
	if len(cfg.Skills.Sources) != 0 {
		t.Fatalf("explicit empty project skill sources should clear user value: %+v", cfg.Skills.Sources)
	}
	chat, err := cfg.LLM.ResolveRoute("chat")
	if err != nil {
		t.Fatalf("ResolveRoute(chat) error = %v", err)
	}
	if chat.APIKey != "project-key" {
		t.Fatalf("LLM API key = %q, want project-key", chat.APIKey)
	}
}

func TestLoadUnresolvedProjectPlaceholderFallsBackToUser(t *testing.T) {
	dir := t.TempDir()
	configHome := t.TempDir()
	const envName = "GENESIS_TEST_UNSET_PROJECT_KEY"
	oldValue, existed := os.LookupEnv(envName)
	if err := os.Unsetenv(envName); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(envName, oldValue)
		} else {
			_ = os.Unsetenv(envName)
		}
	})
	project := minimalConfig("", "") + `
web:
  brave_api_key: ""
  tavily_api_key: ${GENESIS_TEST_UNSET_PROJECT_KEY}
`
	projectLLM := strings.Replace(minimalLLMConfig(), "GENESIS_TEST_QWEN_KEY", envName, 1)
	writeTestConfig(t, dir, project, projectLLM)

	userDir := filepath.Join(configHome, "cli")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("mkdir user config: %v", err)
	}
	user := `
web:
  brave_api_key: user-key
  tavily_api_key: user-key
llm:
  providers:
    qwen:
      auth:
        api_key: user-key
`
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte(user), 0644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: configHome})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if cfg.Web.TavilyAPIKey != "user-key" {
		t.Fatalf("unresolved placeholder should preserve user value, got %q", cfg.Web.TavilyAPIKey)
	}
	if cfg.Web.BraveAPIKey != "" {
		t.Fatalf("explicit empty project value should clear user value, got %q", cfg.Web.BraveAPIKey)
	}
	chat, err := cfg.LLM.ResolveRoute("chat")
	if err != nil {
		t.Fatalf("ResolveRoute(chat) error = %v", err)
	}
	if chat.APIKey != "user-key" {
		t.Fatalf("unresolved LLM placeholder should preserve user value, got %q", chat.APIKey)
	}
}

func TestLoadAgentEnvOverridesUserConfig(t *testing.T) {
	dir := t.TempDir()
	configHome := t.TempDir()
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())
	local := "web:\n  brave_api_key: local-key\nllm:\n  providers:\n    qwen:\n      auth:\n        api_key: local-key\n"
	if err := os.WriteFile(filepath.Join(dir, "config.local.yaml"), []byte(local), 0644); err != nil {
		t.Fatalf("write local config: %v", err)
	}
	userDir := filepath.Join(configHome, "cli")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("mkdir user config: %v", err)
	}
	user := "web:\n  brave_api_key: user-key\nllm:\n  providers:\n    qwen:\n      auth:\n        api_key: user-key\n"
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte(user), 0644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	t.Setenv("AGENT_WEB_BRAVE_API_KEY", "agent-env-key")
	t.Setenv("AGENT_LLM_PROVIDERS_QWEN_AUTH_API_KEY", "agent-env-key")

	cfg, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: configHome})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if cfg.Web.BraveAPIKey != "agent-env-key" {
		t.Fatalf("brave key = %q, want agent-env-key", cfg.Web.BraveAPIKey)
	}
	chat, err := cfg.LLM.ResolveRoute("chat")
	if err != nil {
		t.Fatalf("ResolveRoute(chat) error = %v", err)
	}
	if chat.APIKey != "agent-env-key" {
		t.Fatalf("LLM API key = %q, want agent-env-key", chat.APIKey)
	}
}

func TestLoadWebEnvPlaceholders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAVILY_API_KEY", "script-env-key")
	content := minimalConfig("", "") + `
web:
  tavily_api_key: ${TAVILY_API_KEY}
`
	writeTestConfig(t, dir, content, minimalLLMConfig())

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
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())

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
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())

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
func TestLoadLogDefaultsAndPathCompat(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	content := `
agent:
  max_iterations: 10
  system_prompt: test
log:
  level: debug
  path: custom/logs/agent.log
server:
  host: 127.0.0.1
  port: 8080
`
	writeTestConfig(t, dir, content, minimalLLMConfig())
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Log.Dir != "custom/logs" {
		t.Fatalf("dir = %q", cfg.Log.Dir)
	}
	agent := cfg.Log.Channels["agent"]
	if agent.File != "agent.log" || agent.Format != "text" || agent.RetainDays != 14 {
		t.Fatalf("agent channel = %+v", agent)
	}
	audit := cfg.Log.Channels["audit"]
	if audit.File != "audit.log" || audit.RetainDays != 90 || audit.Format != "jsonl" {
		t.Fatalf("audit channel = %+v", audit)
	}
	if !cfg.Log.Rotate.DailyEnabled() || cfg.Log.Rotate.MaxSizeMB != 100 {
		t.Fatalf("rotate = %+v", cfg.Log.Rotate)
	}
}

func minimalConfig(extraPolicy string, extraSecrets string) string {
	return extraSecrets + extraPolicy + `
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

func minimalLLMConfig() string {
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
`
}

func writeTestConfig(t *testing.T, dir, configContent, llmContent string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "llm.yaml"), []byte(llmContent), 0644); err != nil {
		t.Fatalf("write llm.yaml: %v", err)
	}
}

func TestLoadMCPFragment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GENESIS_TEST_MCP_PASSWORD", "expanded-secret")
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())
	mcpContent := `
mcp:
  enabled: true
  connect_mode: eager
  servers:
    DemoServer:
      type: stdio
      command: demo-mcp
      env:
        PASSWORD: ${GENESIS_TEST_MCP_PASSWORD}
`
	if err := os.WriteFile(filepath.Join(dir, "mcp.yaml"), []byte(mcpContent), 0644); err != nil {
		t.Fatalf("write mcp.yaml: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.MCP.Enabled || cfg.MCP.ConnectMode != "eager" || cfg.MCP.Servers["DemoServer"].Command != "demo-mcp" {
		t.Fatalf("mcp config = %+v", cfg.MCP)
	}
	if cfg.MCP.Servers["DemoServer"].Env["PASSWORD"] != "expanded-secret" {
		t.Fatalf("mcp env = %+v", cfg.MCP.Servers["DemoServer"].Env)
	}
}

func TestLoadRequiresLLMFragmentWhenBaseExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(minimalConfig("", "")), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "llm.yaml") {
		t.Fatalf("Load() error = %v, want missing llm.yaml", err)
	}
}

func TestLoadRejectsMovedSectionInBaseConfig(t *testing.T) {
	dir := t.TempDir()
	content := minimalConfig("", "") + "\nllm:\n  timeout: 10s\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "llm.yaml"), []byte(minimalLLMConfig()), 0644); err != nil {
		t.Fatalf("write llm.yaml: %v", err)
	}

	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "不允许顶层配置") {
		t.Fatalf("Load() error = %v, want section ownership error", err)
	}
}

func TestLoadRejectsMalformedOptionalFragment(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())
	if err := os.WriteFile(filepath.Join(dir, "mcp.yaml"), []byte("mcp: ["), 0644); err != nil {
		t.Fatalf("write mcp.yaml: %v", err)
	}

	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "mcp.yaml") {
		t.Fatalf("Load() error = %v, want malformed mcp.yaml error", err)
	}
}

func TestLoadRejectsHookSectionInGeneralOverride(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, minimalConfig("", ""), minimalLLMConfig())
	if err := os.WriteFile(filepath.Join(dir, "config.local.yaml"), []byte("hooks:\n  enabled: false\n"), 0644); err != nil {
		t.Fatalf("write config.local.yaml: %v", err)
	}

	_, err := LoadWithOptions(dir, LoadOptions{Product: "cli", ConfigHome: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "不允许顶层配置") {
		t.Fatalf("LoadWithOptions() error = %v, want hooks ownership error", err)
	}
}
