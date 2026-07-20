//go:build darwin

package sandbox

import (
	"context"

	"genesis-agent/shared/local/sandbox/pathutil"
	"genesis-agent/shared/local/sandbox/seatbelt"
)

func defaultPlatformBackend() platformBackend { return darwinBackend{} }

type darwinBackend struct{}

func (b darwinBackend) Detect(ctx context.Context) ([]Capability, error) {
	path, ok, reason := seatbelt.Detect(ctx)
	capability := Capability{Type: TypeMacOSSeatbelt, Available: ok, Enforcement: EnforcementFilesystemNetwork, Reason: reason, HelperPath: path}
	return []Capability{capability}, nil
}

func (b darwinBackend) BuildPlan(ctx context.Context, req BuildRequest) (*Plan, error) {
	path, ok, reason := seatbelt.Detect(ctx)
	if !ok {
		return nil, NewError(ErrCodeSandboxUnavailable, nil).WithReason(reason)
	}
	// 路径规范化：解析软链接/相对路径，避免策略被绕过
	fs := req.Profile.FileSystem
	fs.ReadableRoots = pathutil.NormalizeListBestEffort(fs.ReadableRoots)
	fs.WritableRoots = pathutil.NormalizeListBestEffort(fs.WritableRoots)
	fs.ReadOnlyRoots = pathutil.NormalizeListBestEffort(fs.ReadOnlyRoots)
	seatbeltFS := seatbelt.FileSystemPolicy{
		ReadableRoots:          fs.ReadableRoots,
		WritableRoots:          fs.WritableRoots,
		ReadOnlyRoots:          fs.ReadOnlyRoots,
		UnreadablePaths:        pathutil.NormalizeListBestEffort(pathRules(fs.UnreadablePaths)),
		ProtectedMetadataPaths: pathutil.NormalizeListBestEffort(protectedMetadataPaths(fs)),
		AllowFullDiskRead:      fs.AllowFullDiskRead,
		AllowFullDiskWrite:     fs.AllowFullDiskWrite,
	}
	built, err := seatbelt.Build(seatbelt.BuildOptions{
		Command:          seatbelt.CommandSpec{Argv: req.Command.Argv, Env: req.Command.Env, Cwd: req.Command.Cwd},
		FileSystem:       seatbeltFS,
		Network:          seatbelt.NetworkPolicy(req.Profile.Network),
		ProxyPorts:       req.Profile.ProxyPorts,
		AllowUnixSockets: req.Profile.AllowUnixSockets,
	})
	if err != nil {
		return nil, NewError(ErrCodeSandboxInitFailed, err)
	}

	cmdEnv := applyProxyEnv(req.Command.Env, req.Profile.Network, req.Profile.ProxyEnv)

	plan := &Plan{
		Type:                    TypeMacOSSeatbelt,
		Enforcement:             EnforcementFilesystemNetwork,
		Command:                 CommandSpec{Argv: append([]string{built.Program}, built.Args...), Env: cmdEnv, Cwd: req.Command.Cwd},
		HelperPath:              path,
		FileSystemPolicy:        fs,
		NetworkPolicy:           req.Profile.Network,
		ProcessPolicy:           req.Profile.Process,
		EffectiveSandboxProfile: req.Profile,
	}
	plan.CompleteAuditTags(req.Preference)
	return plan, nil
}
