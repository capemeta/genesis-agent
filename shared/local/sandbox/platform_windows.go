//go:build windows

package sandbox

import (
	"context"
	"strings"

	win "genesis-agent/shared/local/sandbox/windows"
)

func defaultPlatformBackend() platformBackend { return windowsBackend{} }

type windowsBackend struct{}

func (b windowsBackend) Detect(ctx context.Context) ([]Capability, error) {
	ok, reason := win.Detect(ctx)
	app := win.EvaluateAppContainerSupport()
	appReason := strings.Join(app.Reasons, "; ")
	return []Capability{
		{Type: TypeWindowsProcessConstrained, Available: ok, Enforcement: EnforcementProcessConstrained, Reason: reason},
		{Type: TypeWindowsAppContainer, Available: app.Supported, Enforcement: EnforcementFilesystemNetwork, Reason: appReason},
	}, nil
}

func (b windowsBackend) BuildPlan(ctx context.Context, req BuildRequest) (*Plan, error) {
	ok, reason := win.Detect(ctx)
	if !ok {
		return nil, NewError(ErrCodeSandboxUnavailable, nil).WithReason(reason)
	}
	support := win.EvaluateProcessConstrainedSupport(win.FileSystemPolicy{RequiresFilesystem: req.Profile.FileSystem.RequiresFilesystemSandbox()}, win.NetworkMode(req.Profile.Network), win.ProcessPolicy{KillProcessTree: req.Profile.Process.KillProcessTree, ConstrainToken: req.Profile.Process.ConstrainToken})
	if !support.Supported {
		return nil, NewError(ErrCodePolicyUnsupported, nil).WithReason(strings.Join(support.Reasons, "; "))
	}
	argv, err := win.BuildProcessConstrainedPlan(req.Command.Argv)
	if err != nil {
		return nil, NewError(ErrCodeSandboxInitFailed, err)
	}
	plan := &Plan{Type: TypeWindowsProcessConstrained, Enforcement: EnforcementProcessConstrained, Command: CommandSpec{Argv: argv, Env: req.Command.Env, Cwd: req.Command.Cwd}, FileSystemPolicy: req.Profile.FileSystem, NetworkPolicy: req.Profile.Network, ProcessPolicy: req.Profile.Process, EffectiveSandboxProfile: req.Profile}
	plan.CompleteAuditTags(req.Preference)
	return plan, nil
}
