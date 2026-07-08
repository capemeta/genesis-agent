package sandbox

import (
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	platformconfig "genesis-agent/internal/platform/config"
)

func TestParseFlagDefaultsToDisabledLocalHost(t *testing.T) {
	cfg, err := ParseFlag("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeLocalHost || cfg.Execution != execmodel.SandboxDisabled {
		t.Fatalf("cfg = %+v", cfg)
	}
	profile := cfg.ExecutionProfile()
	if profile.Mode != execmodel.SandboxDisabled || profile.Provider != "" {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestParseFlagOptionalUsesLocalPlatformSandbox(t *testing.T) {
	cfg, err := ParseFlag("optional")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "" || cfg.Execution != execmodel.SandboxOptional {
		t.Fatalf("cfg = %+v", cfg)
	}
	profile := MergeSessionOverride(DefaultConfig(), cfg).ExecutionProfile()
	if profile.Mode != execmodel.SandboxOptional || profile.Provider != ProviderLocalPlatform {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Metadata["filesystem"] != "workspace_write" || profile.Metadata["network"] != "disabled" {
		t.Fatalf("metadata = %+v", profile.Metadata)
	}
	if profile.RuntimeProfile != execmodel.RuntimeProfileCodePolyglotBasic ||
		profile.TaskType != execmodel.SandboxTaskShell ||
		profile.Operation != execmodel.SandboxOperationRunShell ||
		profile.Language != "shell" ||
		profile.RiskLevel != execmodel.SandboxRiskMedium {
		t.Fatalf("sandbox execution profile = %+v", profile)
	}
}

func TestMergeSessionOverridePreservesExternalMode(t *testing.T) {
	base := Config{
		Mode:        ModeDockerSandbox,
		Execution:   execmodel.SandboxDisabled,
		Endpoint:    "http://127.0.0.1:18010",
		WorkspaceID: "workspace-1",
	}
	override, err := ParseFlag("required")
	if err != nil {
		t.Fatal(err)
	}
	merged := MergeSessionOverride(base, override)
	profile := merged.ExecutionProfile()
	if merged.Mode != ModeDockerSandbox || profile.Provider != ProviderGenesisSandbox {
		t.Fatalf("merged=%+v profile=%+v", merged, profile)
	}
	if profile.Mode != execmodel.SandboxRequired || profile.WorkspaceID != "workspace-1" {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestParseFlagRejectsUnknownMode(t *testing.T) {
	if _, err := ParseFlag("docker"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFromRuntimeConfigDisabled(t *testing.T) {
	cfg, err := FromRuntimeConfig(platformconfig.SandboxConfig{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeLocalHost || cfg.Execution != execmodel.SandboxDisabled {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestFromRuntimeConfigDockerSandbox(t *testing.T) {
	cfg, err := FromRuntimeConfig(platformconfig.SandboxConfig{
		Enabled:               true,
		Mode:                  string(ModeDockerSandbox),
		DefaultExecution:      string(execmodel.SandboxRequired),
		BaseURL:               "http://127.0.0.1:18010",
		APIKey:                "token",
		WorkspaceID:           "workspace-1",
		DefaultRuntimeProfile: string(execmodel.RuntimeProfileCodePythonIsolated),
		AllowSessionOverride:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := cfg.ExecutionProfile()
	if cfg.Mode != ModeDockerSandbox || profile.Provider != ProviderGenesisSandbox {
		t.Fatalf("cfg=%+v profile=%+v", cfg, profile)
	}
	if profile.Mode != execmodel.SandboxRequired ||
		profile.WorkspaceID != "workspace-1" ||
		profile.RuntimeProfile != execmodel.RuntimeProfileCodePythonIsolated {
		t.Fatalf("profile = %+v", profile)
	}
}
