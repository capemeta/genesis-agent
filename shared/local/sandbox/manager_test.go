package sandbox

import (
	"context"
	"errors"
	"testing"
)

type fakeBackend struct {
	plan *Plan
	err  error
}

func (b fakeBackend) Detect(ctx context.Context) ([]Capability, error) {
	return []Capability{{Type: TypeLinuxBubblewrap, Available: b.err == nil, Enforcement: EnforcementFilesystemNetwork}}, nil
}

func (b fakeBackend) BuildPlan(ctx context.Context, req BuildRequest) (*Plan, error) {
	if b.err != nil {
		return nil, b.err
	}
	plan := b.plan
	if plan == nil {
		plan = &Plan{Type: TypeLinuxBubblewrap, Enforcement: EnforcementFilesystemNetwork, Command: req.Command.Clone(), FileSystemPolicy: req.Profile.FileSystem, NetworkPolicy: req.Profile.Network, ProcessPolicy: req.Profile.Process, EffectiveSandboxProfile: req.Profile}
	}
	plan.CompleteAuditTags(req.Preference)
	return plan, nil
}

func TestBuildPlanDisabledReturnsDirectPlan(t *testing.T) {
	mgr := NewManagerWithBackend(fakeBackend{err: errors.New("should not be called")})
	plan, err := mgr.BuildPlan(context.Background(), BuildRequest{Preference: PreferenceDisabled, Command: CommandSpec{Argv: []string{"echo", "hi"}}})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.Type != TypeNone || plan.Enforcement != EnforcementNone {
		t.Fatalf("direct plan type/enforcement = %s/%s", plan.Type, plan.Enforcement)
	}
	if got := plan.Command.Argv; len(got) != 2 || got[0] != "echo" || got[1] != "hi" {
		t.Fatalf("argv changed: %#v", got)
	}
}

func TestBuildPlanAutoDegradesWithWarning(t *testing.T) {
	mgr := NewManagerWithBackend(fakeBackend{err: NewError(ErrCodeSandboxUnavailable, nil).WithReason("no bwrap")})
	plan, err := mgr.BuildPlan(context.Background(), BuildRequest{Preference: PreferenceAuto, Command: CommandSpec{Argv: []string{"echo", "hi"}}, Profile: Profile{Network: NetworkDisabled}})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if !plan.Degraded || plan.Type != TypeNone || len(plan.Warnings) == 0 {
		t.Fatalf("expected degraded direct plan, got %#v", plan)
	}
}

func TestBuildPlanRequiredFailsClosed(t *testing.T) {
	mgr := NewManagerWithBackend(fakeBackend{err: NewError(ErrCodeSandboxUnavailable, nil).WithReason("no bwrap")})
	_, err := mgr.BuildPlan(context.Background(), BuildRequest{Preference: PreferenceRequired, Command: CommandSpec{Argv: []string{"echo", "hi"}}})
	if !IsUnavailable(err) {
		t.Fatalf("expected unavailable error, got %v", err)
	}
}

func TestBuildPlanInvalidRelativeCwd(t *testing.T) {
	mgr := NewManagerWithBackend(fakeBackend{})
	_, err := mgr.BuildPlan(context.Background(), BuildRequest{Command: CommandSpec{Argv: []string{"echo"}, Cwd: "relative"}})
	if CodeOf(err) != ErrCodeInvalidInput {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestDirectPlanAuditIncludesPreference(t *testing.T) {
	mgr := NewManagerWithBackend(fakeBackend{})
	plan, err := mgr.BuildPlan(context.Background(), BuildRequest{Preference: PreferenceDisabled, Command: CommandSpec{Argv: []string{"echo"}}})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if got := plan.AuditTags["sandbox.preference"]; got != string(PreferenceDisabled) {
		t.Fatalf("sandbox.preference audit tag = %q", got)
	}
}

func TestFileSystemPolicyRequiresSandboxForReadOnlyRootsEvenWithFullWrite(t *testing.T) {
	policy := FileSystemPolicy{AllowFullDiskRead: true, AllowFullDiskWrite: true, ReadOnlyRoots: []string{"/work/.git"}}
	if !policy.RequiresFilesystemSandbox() {
		t.Fatalf("expected readonly roots to require filesystem sandbox")
	}
}

func TestProtectedMetadataPathsDefaultsUnderWritableRoots(t *testing.T) {
	paths := protectedMetadataPaths(FileSystemPolicy{WritableRoots: []string{"/work"}})
	want := map[string]bool{"/work/.git": false, "/work/.codex": false, "/work/.agents": false}
	for _, path := range paths {
		if _, ok := want[path]; ok {
			want[path] = true
		}
	}
	for path, seen := range want {
		if !seen {
			t.Fatalf("missing protected metadata path %s in %#v", path, paths)
		}
	}
}

func TestAuditTagsIncludeEffectiveNetworkPolicy(t *testing.T) {
	mgr := NewManagerWithBackend(fakeBackend{})
	plan, err := mgr.BuildPlan(context.Background(), BuildRequest{
		Preference: PreferenceDisabled,
		Command:    CommandSpec{Argv: []string{"echo"}},
		Profile:    Profile{Network: NetworkDisabled},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if got := plan.AuditTags["sandbox.effective_network_policy"]; got != string(NetworkDisabled) {
		t.Fatalf("sandbox.effective_network_policy audit tag = %q, want %q", got, NetworkDisabled)
	}
}

func TestAuditTagsIncludeWarningsWhenDegraded(t *testing.T) {
	mgr := NewManagerWithBackend(fakeBackend{err: NewError(ErrCodeSandboxUnavailable, nil).WithReason("no bwrap")})
	plan, err := mgr.BuildPlan(context.Background(), BuildRequest{
		Preference: PreferenceAuto,
		Command:    CommandSpec{Argv: []string{"echo"}},
		Profile:    Profile{Network: NetworkDisabled},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if !plan.Degraded {
		t.Fatal("expected degraded plan")
	}
	if got := plan.AuditTags["sandbox.warnings"]; got == "" {
		t.Fatal("expected sandbox.warnings audit tag to be set for degraded plan")
	}
}

