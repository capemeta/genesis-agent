package command

import (
	"encoding/json"
	capmodel "genesis-agent/internal/capabilities/capability/model"
	"os"
	"path/filepath"
	"strings"
	"testing"

	marketmodel "genesis-agent/internal/capabilities/package/marketplace/model"
)

func TestSkillCreateAndPackageCommands(t *testing.T) {
	tmp := t.TempDir()
	create := newSkillCreateCmd()
	create.SetArgs([]string{"Demo Skill", "--path", tmp, "--description", "Use this skill for repeatable demo workflows.", "--resources", "references,scripts", "--evals"})
	if err := create.Execute(); err != nil {
		t.Fatalf("skill create failed: %v", err)
	}

	skillDir := filepath.Join(tmp, "demo-skill")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md was not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "references")); err != nil {
		t.Fatalf("references dir was not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "evals", "evals.json")); err != nil {
		t.Fatalf("evals draft was not created: %v", err)
	}
	evalValidate := newSkillEvalValidateCmd()
	evalValidate.SetArgs([]string{skillDir})
	if err := evalValidate.Execute(); err != nil {
		t.Fatalf("skill eval validate failed: %v", err)
	}

	outDir := filepath.Join(tmp, "dist")
	pack := newSkillPackageCmd()
	pack.SetArgs([]string{skillDir, "--out", outDir, "--force"})
	if err := pack.Execute(); err != nil {
		t.Fatalf("skill package failed: %v", err)
	}

	manifestPath := filepath.Join(outDir, ".genesis", "marketplace.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("marketplace manifest was not written: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("marketplace manifest is invalid json: %v", err)
	}
	if raw["$schema"] == "" {
		t.Fatalf("marketplace manifest should write $schema: %s", string(data))
	}
	var manifest marketmodel.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("marketplace manifest is invalid json: %v", err)
	}
	if manifest.Name != "demo-skill-marketplace" || len(manifest.Packages) != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	pkg := manifest.Packages[0]
	if pkg.Name != "demo-skill" || pkg.Type != marketmodel.PackageTypeSkillPackage || len(pkg.Capabilities) != 1 {
		t.Fatalf("unexpected package entry: %+v", pkg)
	}
	capability := pkg.Capabilities[0]
	if capability.Type != capmodel.CapabilityTypeSkill || capability.Name != "demo-skill" || capability.Path != "./skills/demo-skill" {
		t.Fatalf("unexpected capability entry: %+v", capability)
	}
	if _, err := os.Stat(filepath.Join(outDir, "skills", "demo-skill", "SKILL.md")); err != nil {
		t.Fatalf("packaged SKILL.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "skills", "demo-skill", "evals")); !os.IsNotExist(err) {
		t.Fatalf("root evals directory should not be packaged, err=%v", err)
	}
}

func TestSkillEvalValidateRunCommand(t *testing.T) {
	tmp := t.TempDir()
	grading := `{"expectations":[{"text":"The report exists","passed":true,"evidence":"outputs/report.md exists"}],"summary":{"passed":1,"failed":0,"total":1,"pass_rate":1}}`
	if err := os.WriteFile(filepath.Join(tmp, "grading.json"), []byte(grading), 0o644); err != nil {
		t.Fatalf("write grading.json: %v", err)
	}
	cmd := newSkillEvalValidateRunCmd()
	cmd.SetArgs([]string{tmp})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill eval validate-run failed: %v", err)
	}
}
func TestSkillCardCommands(t *testing.T) {
	tmp := t.TempDir()
	create := newSkillCreateCmd()
	create.SetArgs([]string{"Card Demo", "--path", tmp, "--description", "Use this skill when preparing release metadata."})
	if err := create.Execute(); err != nil {
		t.Fatalf("skill create failed: %v", err)
	}
	skillDir := filepath.Join(tmp, "card-demo")

	validateMissing := newSkillCardValidateCmd()
	validateMissing.SetArgs([]string{skillDir})
	if err := validateMissing.Execute(); err != nil {
		t.Fatalf("missing skill card should warn but not fail: %v", err)
	}

	generate := newSkillCardGenerateCmd()
	generate.SetArgs([]string{skillDir, "--owner", "Team AI", "--license", "Apache-2.0"})
	if err := generate.Execute(); err != nil {
		t.Fatalf("skill card generate failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "skill-card.md")); err != nil {
		t.Fatalf("skill-card.md was not created: %v", err)
	}

	validate := newSkillCardValidateCmd()
	validate.SetArgs([]string{skillDir})
	if err := validate.Execute(); err != nil {
		t.Fatalf("skill card validate failed: %v", err)
	}
}
func TestSkillCardValidateRequiresValidSkill(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "skill-card.md"), []byte("# Demo\n\n## Description\n\nOnly card.\n"), 0o644); err != nil {
		t.Fatalf("write skill-card.md: %v", err)
	}
	cmd := newSkillCardValidateCmd()
	cmd.SetArgs([]string{tmp})
	if err := cmd.Execute(); err == nil {
		t.Fatal("skill card validate should fail when SKILL.md is invalid or missing")
	}
}
func TestEnsureCreatableDirRejectsDangerousForceTarget(t *testing.T) {
	if err := ensureCreatableDir(".", true); err == nil {
		t.Fatal("expected current directory force overwrite to be rejected")
	}
}
func TestNormalizeSkillName(t *testing.T) {
	got := normalizeSkillName("  Demo__Skill!! 2026  ")
	if got != "demo-skill-2026" {
		t.Fatalf("normalizeSkillName() = %q", got)
	}
	if strings.Contains(normalizeSkillName("bad/name"), "/") {
		t.Fatal("normalized skill name should not contain path separators")
	}
}
