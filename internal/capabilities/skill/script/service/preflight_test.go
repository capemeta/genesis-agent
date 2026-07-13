package service

import (
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

func TestResolveRuntimeProfileUsesDeclaredOfficeDeps(t *testing.T) {
	meta := skillmodel.Metadata{Dependencies: skillmodel.Dependencies{Runtime: skillmodel.RuntimeDeps{System: []skillmodel.RuntimePackage{{Name: "libreoffice", Command: "soffice"}}}}}
	if got := resolveRuntimeProfile(meta, resolveTaskType(meta)); got != execmodel.RuntimeProfileOfficeBasic {
		t.Fatalf("profile=%s", got)
	}
}

func TestResolveRuntimeProfileFallsBackToPolyglot(t *testing.T) {
	meta := skillmodel.Metadata{}
	if got := resolveRuntimeProfile(meta, resolveTaskType(meta)); got != execmodel.RuntimeProfileSkillPolyglotBasic {
		t.Fatalf("profile=%s", got)
	}
}
