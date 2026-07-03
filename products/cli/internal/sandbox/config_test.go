package sandbox

import (
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
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
	if cfg.Mode != ModePlatform || cfg.Execution != execmodel.SandboxOptional {
		t.Fatalf("cfg = %+v", cfg)
	}
	profile := cfg.ExecutionProfile()
	if profile.Mode != execmodel.SandboxOptional || profile.Provider != ProviderLocalPlatform {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Metadata["filesystem"] != "workspace_write" || profile.Metadata["network"] != "disabled" {
		t.Fatalf("metadata = %+v", profile.Metadata)
	}
}

func TestParseFlagRejectsUnknownMode(t *testing.T) {
	if _, err := ParseFlag("docker"); err == nil {
		t.Fatal("expected error")
	}
}
