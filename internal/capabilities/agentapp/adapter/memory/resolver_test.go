package memory

import (
	"context"
	"testing"

	agentappcontract "genesis-agent/internal/capabilities/agentapp/contract"
	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestResolverSelectsOnlyRegisteredEffectiveProfile(t *testing.T) {
	profiles := []agentappmodel.EffectiveProfile{
		{ID: "code", Version: "1", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeProject, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeProject}, DefaultAccess: execmodel.WorkspaceAccessReadWrite}},
		{ID: "doc-review", Version: "2", Workspace: agentappmodel.WorkspaceSpec{DefaultMode: execmodel.WorkspaceModeTask, AllowedModes: []execmodel.WorkspaceMode{execmodel.WorkspaceModeTask}, DefaultAccess: execmodel.WorkspaceAccessReadOnly}},
	}
	resolver, err := NewResolver("code", profiles)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := resolver.ResolveEffective(context.Background(), agentappcontract.ResolveRequest{AppID: "doc-review"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "doc-review" || selected.Workspace.DefaultAccess != execmodel.WorkspaceAccessReadOnly {
		t.Fatalf("selected = %+v", selected)
	}
	if _, err := resolver.ResolveEffective(context.Background(), agentappcontract.ResolveRequest{AppID: "unknown"}); err == nil {
		t.Fatal("未注册 App 必须拒绝")
	}
}
