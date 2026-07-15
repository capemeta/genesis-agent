package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadHookConfigUsesProjectOnlyForCLI(t *testing.T) {
	root := t.TempDir()
	configs := filepath.Join(root, "configs")
	if err := os.MkdirAll(filepath.Join(root, ".genesis"), 0755); err != nil {
		t.Fatal(err)
	}
	content := "events:\n  RunStart:\n    - handlers:\n        - name: project\n          type: builtin\n          builtin: git_branch_guard\n"
	if err := os.WriteFile(filepath.Join(root, ".genesis", "hooks.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cli, err := LoadHookConfig(configs, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(cli.Events["RunStart"]) != 1 {
		t.Fatalf("CLI did not load project hooks: %+v", cli.Events)
	}
	enterprise, err := LoadHookConfig(configs, "enterprise")
	if err != nil {
		t.Fatal(err)
	}
	if len(enterprise.Events["RunStart"]) != 0 || !enterprise.AllowManagedOnly || enterprise.Execution != "sandbox" {
		t.Fatalf("unexpected Enterprise Hook config: %+v", enterprise)
	}
}

func TestLoadHookConfigPrecedence(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	configDir := filepath.Join(root, "configs")
	projectDir := filepath.Join(root, ".genesis")
	userDir := filepath.Join(home, ".genesis-agent", "cli")
	for _, dir := range []string{configDir, projectDir, userDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	writeHookConfig := func(path, name, timeout string) {
		t.Helper()
		content := "default_timeout: " + timeout + "\nevents:\n  RunStart:\n    - handlers:\n        - name: " + name + "\n          type: builtin\n          builtin: git_branch_guard\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	writeHookConfig(filepath.Join(userDir, "hooks.yaml"), "user", "1s")
	writeHookConfig(filepath.Join(configDir, "hooks.yaml"), "project-shared", "2s")
	writeHookConfig(filepath.Join(projectDir, "hooks.yaml"), "project", "3s")
	writeHookConfig(filepath.Join(configDir, "hooks.local.yaml"), "project-local", "4s")

	cfg, err := LoadHookConfig(configDir, "cli")
	if err != nil {
		t.Fatalf("LoadHookConfig() error = %v", err)
	}
	if cfg.DefaultTimeout != 4*time.Second {
		t.Fatalf("DefaultTimeout = %s, want 4s", cfg.DefaultTimeout)
	}
	groups := cfg.Events["RunStart"]
	if len(groups) != 4 {
		t.Fatalf("RunStart groups = %+v", groups)
	}
	want := []string{"user", "project-shared", "project", "project-local"}
	for i, name := range want {
		if len(groups[i].Handlers) != 1 || groups[i].Handlers[0].Name != name {
			t.Fatalf("RunStart group %d = %+v, want %s", i, groups[i], name)
		}
	}
}
