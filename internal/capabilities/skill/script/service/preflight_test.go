package service

import (
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

func TestResolveRuntimeProfileUsesDeclaredOfficeDeps(t *testing.T) {
	deps := skillmodel.RuntimeDeps{System: []skillmodel.RuntimePackage{{Name: "libreoffice", Command: "soffice"}}}
	taskType := resolveTaskType(deps)
	if taskType != execmodel.SandboxTaskOffice || resolveRuntimeProfile(taskType) != execmodel.RuntimeProfileOfficeBasic {
		t.Fatalf("task=%s profile=%s", taskType, resolveRuntimeProfile(taskType))
	}
}

func TestResolveRuntimeProfileFallsBackToPolyglot(t *testing.T) {
	taskType := resolveTaskType(skillmodel.RuntimeDeps{})
	if resolveRuntimeProfile(taskType) != execmodel.RuntimeProfileSkillPolyglotBasic {
		t.Fatalf("profile=%s", resolveRuntimeProfile(taskType))
	}
}
