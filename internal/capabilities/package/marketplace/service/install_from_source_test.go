package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	marketcontract "genesis-agent/internal/capabilities/package/marketplace/contract"
	marketservice "genesis-agent/internal/capabilities/package/marketplace/service"
	"genesis-agent/shared/local/skillmarket"
)

func TestInstallFromSourceSingleSkillDir(t *testing.T) {
	ctx := context.Background()
	svc, roots := newTestService(t)
	skillDir := filepath.Join(t.TempDir(), "demo-skill")
	writeSkill(t, skillDir, "demo-skill")

	result := svc.InstallFromSource(ctx, marketcontract.InstallFromSourceRequest{
		SourceInput: "dir:" + skillDir,
		Scope:       "user",
		Product:     "cli",
	})
	if result.FailureKind != "" {
		t.Fatalf("install failed: %+v", result)
	}
	if len(result.Skills) != 1 || result.Skills[0] != "demo-skill" {
		t.Fatalf("skills=%v", result.Skills)
	}
	if result.Effective != "next_turn" {
		t.Fatalf("effective=%s", result.Effective)
	}
	_ = roots
}

func TestInstallFromSourceMultiSkillNeedsChoice(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "alpha"), "alpha")
	writeSkill(t, filepath.Join(root, "beta"), "beta")

	result := svc.InstallFromSource(ctx, marketcontract.InstallFromSourceRequest{
		SourceInput: "dir:" + root,
		Product:     "cli",
	})
	if !result.NeedsChoice || result.FailureKind != "needs_choice" {
		t.Fatalf("expected needs_choice, got %+v", result)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates=%v", result.Candidates)
	}
}

func TestInstallFromSourcePolicyDenied(t *testing.T) {
	ctx := context.Background()
	cache := t.TempDir()
	svc, err := marketservice.New(marketservice.Options{
		Registry:     skillmarket.NewRegistryStore(filepath.Join(cache, "m.json")),
		Installs:     skillmarket.NewInstallStore(filepath.Join(cache, "i.json")),
		Capabilities: skillmarket.NewCapabilityIndexStore(filepath.Join(cache, "c.json")),
		Parser:       skillmarket.NewParser(),
		Fetcher:      skillmarket.NewFetcher(filepath.Join(cache, "cache"), nil),
		Installer: skillmarket.NewInstaller(skillmarket.InstallerOptions{
			UserInstalledDir: filepath.Join(cache, "installed"),
		}),
		Policy: skillmarket.DenyAllRemotePolicy{},
	})
	if err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(t.TempDir(), "x")
	writeSkill(t, skillDir, "x")
	result := svc.InstallFromSource(ctx, marketcontract.InstallFromSourceRequest{
		SourceInput: "dir:" + skillDir,
		Product:     "enterprise",
	})
	if result.FailureKind != "policy_denied" {
		t.Fatalf("expected policy_denied, got %+v", result)
	}
}

func newTestService(t *testing.T) (*marketservice.Service, string) {
	t.Helper()
	cache := t.TempDir()
	svc, err := marketservice.New(marketservice.Options{
		Registry:     skillmarket.NewRegistryStore(filepath.Join(cache, "m.json")),
		Installs:     skillmarket.NewInstallStore(filepath.Join(cache, "i.json")),
		Capabilities: skillmarket.NewCapabilityIndexStore(filepath.Join(cache, "c.json")),
		Parser:       skillmarket.NewParser(),
		Fetcher:      skillmarket.NewFetcher(filepath.Join(cache, "cache"), nil),
		Installer: skillmarket.NewInstaller(skillmarket.InstallerOptions{
			UserInstalledDir: filepath.Join(cache, "installed"),
		}),
		Policy: skillmarket.AllowGitHubPolicy{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, cache
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: test skill for " + name + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
