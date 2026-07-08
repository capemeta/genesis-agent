package card

import (
	"testing"
	"testing/fstest"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

func TestValidateMissingSkillCardWarns(t *testing.T) {
	result := NewValidator().ValidateFS(fstest.MapFS{})
	if result.Found {
		t.Fatal("missing skill-card.md should not be marked found")
	}
	if len(result.Findings) != 1 || result.Findings[0].Code != "skill_card_missing" {
		t.Fatalf("unexpected findings: %+v", result.Findings)
	}
	if result.HasErrors() {
		t.Fatal("missing skill-card.md should warn, not error")
	}
}

func TestRenderAndValidateSkillCard(t *testing.T) {
	content, err := Render(TemplateDataFromMetadata(skillmodel.Metadata{
		Name:        "demo-skill",
		Description: "Use this skill for repeatable demo workflows.",
		Version:     "1.2.3",
	}, "Team AI", "Apache-2.0", ""))
	if err != nil {
		t.Fatalf("render skill card: %v", err)
	}
	result := NewValidator().ValidateFS(fstest.MapFS{
		SkillCardPath: {Data: []byte(content)},
	})
	if !result.Found {
		t.Fatal("skill-card.md should be found")
	}
	if result.HasErrors() {
		t.Fatalf("rendered card should not have errors: %+v", result.Findings)
	}
	for _, finding := range result.Findings {
		if finding.Code == "skill_card_section_missing" {
			t.Fatalf("rendered card should contain all required sections: %+v", result.Findings)
		}
	}
}

func TestValidateSkillCardMissingSections(t *testing.T) {
	result := NewValidator().ValidateFS(fstest.MapFS{
		SkillCardPath: {Data: []byte("# Demo\n\n## Description\n\nOnly one section.\n")},
	})
	if !result.Found {
		t.Fatal("skill-card.md should be found")
	}
	missing := 0
	for _, finding := range result.Findings {
		if finding.Code == "skill_card_section_missing" {
			missing++
		}
	}
	if missing == 0 {
		t.Fatalf("expected missing section warnings: %+v", result.Findings)
	}
}
