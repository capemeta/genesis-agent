//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"

	"genesis-agent/shared/local/sandbox/bubblewrap"
	"genesis-agent/shared/local/sandbox/landlock"
	"genesis-agent/shared/local/sandbox/pathutil"
)

func defaultPlatformBackend() platformBackend { return linuxBackend{} }

type linuxBackend struct{}

func (b linuxBackend) Detect(ctx context.Context) ([]Capability, error) {
	home, _ := os.UserHomeDir()
	bwrapPath, bwrapOK, bwrapReason := bubblewrap.Detect(ctx, "", bubblewrap.HelperTrustOptions{TempRoots: bubblewrap.DefaultTempRoots(), HomeDir: home})
	landlockOK, landlockReason := landlock.Detect(ctx)
	return []Capability{
		{Type: TypeLinuxBubblewrap, Available: bwrapOK, Enforcement: EnforcementFilesystemNetwork, Reason: bwrapReason, HelperPath: bwrapPath},
		{Type: TypeLinuxLandlock, Available: landlockOK, Enforcement: EnforcementFilesystem, Reason: landlockReason},
	}, nil
}

func (b linuxBackend) BuildPlan(ctx context.Context, req BuildRequest) (*Plan, error) {
	home, _ := os.UserHomeDir()
	preferredBwrap := "" // 产品 bootstrap 可通过自定义 backend 注入绝对路径
	bwrapPath, bwrapOK, bwrapReason := bubblewrap.Detect(ctx, preferredBwrap, bubblewrap.HelperTrustOptions{
		WorkspaceRoots: req.WorkspaceRoots,
		WritableRoots:  append(req.Writables, req.Profile.FileSystem.WritableRoots...),
		TempRoots:      bubblewrap.DefaultTempRoots(),
		HomeDir:        home,
	})

	if bwrapOK {
		return b.buildBubblewrapPlan(ctx, req, bwrapPath)
	}

	// bwrap 不可用，尝试 Landlock fallback
	landlockOK, landlockReason := landlock.Detect(ctx)
	if landlockOK {
		return b.buildLandlockFallbackPlan(ctx, req, bwrapReason, landlockReason)
	}

	// 两者均不可用
	return nil, NewError(ErrCodeSandboxUnavailable, nil).WithReason(
		fmt.Sprintf("bubblewrap不可用: %s; landlock不可用: %s", bwrapReason, landlockReason),
	)
}

func (b linuxBackend) buildBubblewrapPlan(_ context.Context, req BuildRequest, bwrapPath string) (*Plan, error) {
	// 路径规范化：解析软链接/相对路径，避免策略被绕过
	fs := req.Profile.FileSystem
	fs.WritableRoots = pathutil.NormalizeListBestEffort(fs.WritableRoots)
	fs.ReadOnlyRoots = pathutil.NormalizeListBestEffort(fs.ReadOnlyRoots)
	normalizedProtected := pathutil.NormalizeListBestEffort(protectedMetadataPaths(fs))
	// 将规范化路径转换为 bwrap DenyReadPaths mask
	unreadableMasks := make([]bubblewrap.PathMask, 0, len(fs.UnreadablePaths))
	for _, rule := range fs.UnreadablePaths {
		if rule.Path == "" {
			continue
		}
		norm, err2 := pathutil.Normalize(rule.Path) //nolint:govet
		if err2 != nil {
			norm = rule.Path
		}
		unreadableMasks = append(unreadableMasks, bubblewrap.PathMask{Path: norm, Kind: bubblewrap.PathMaskAuto})
	}

	built, err := bubblewrap.Build(bubblewrap.BuildOptions{
		BwrapPath:     bwrapPath,
		Command:       req.Command.Argv,
		Cwd:           req.Command.Cwd,
		WritableRoots: fs.WritableRoots,
		ReadOnlyRoots: append(fs.ReadOnlyRoots, normalizedProtected...),
		DenyReadPaths: unreadableMasks,
		Network:       bubblewrap.NetworkMode(req.Profile.Network),
		MountProc:     true,
		NoNewPrivs:    true,
	})
	if err != nil {
		return nil, NewError(ErrCodeSandboxInitFailed, err)
	}
	plan := &Plan{
		Type:                    TypeLinuxBubblewrap,
		Enforcement:             EnforcementFilesystemNetwork,
		Command:                 CommandSpec{Argv: append([]string{built.Program}, built.Args...), Env: req.Command.Env, Cwd: req.Command.Cwd},
		HelperPath:              bwrapPath,
		FileSystemPolicy:        fs,
		NetworkPolicy:           req.Profile.Network,
		ProcessPolicy:           req.Profile.Process,
		EffectiveSandboxProfile: req.Profile,
	}
	plan.CompleteAuditTags(req.Preference)
	return plan, nil
}

func (b linuxBackend) buildLandlockFallbackPlan(_ context.Context, req BuildRequest, bwrapReason, _ string) (*Plan, error) {
	// 检查策略是否在 Landlock 支持范围内
	support := landlock.EvaluateSupport(landlock.FileSystemPolicy{
		WritableRoots:          req.Profile.FileSystem.WritableRoots,
		UnreadablePaths:        pathRules(req.Profile.FileSystem.UnreadablePaths),
		ProtectedMetadataPaths: protectedMetadataPaths(req.Profile.FileSystem),
		AllowFullDiskRead:      req.Profile.FileSystem.AllowFullDiskRead,
		AllowFullDiskWrite:     req.Profile.FileSystem.AllowFullDiskWrite,
	}, landlock.NetworkMode(req.Profile.Network))
	if !support.Supported {
		return nil, NewError(ErrCodePolicyUnsupported, nil).WithReason(
			fmt.Sprintf("Landlock fallback无法表达请求策略: %s", strings.Join(support.Reasons, "; ")),
		)
	}

	// 构造 Landlock plan（不改写 argv，ApplyFn 由 runner 在进程内调用）
	_, err := landlock.Build(landlock.BuildOptions{WritableRoots: req.Profile.FileSystem.WritableRoots})
	if err != nil {
		return nil, NewError(ErrCodeSandboxInitFailed, err)
	}

	warnings := []string{
		fmt.Sprintf("bubblewrap不可用（%s），已降级为Landlock fallback", bwrapReason),
		"Landlock fallback不能提供网络隔离和mount视图，请评估安全影响",
	}

	plan := &Plan{
		Type:                    TypeLinuxLandlock,
		Enforcement:             EnforcementFilesystem,
		Command:                 req.Command.Clone(),
		FileSystemPolicy:        req.Profile.FileSystem,
		NetworkPolicy:           NetworkFullAccess, // Landlock 无法做网络隔离，effective 为 full_access
		ProcessPolicy:           req.Profile.Process,
		Warnings:                warnings,
		Degraded:                true,
		EffectiveSandboxProfile: req.Profile,
	}
	plan.CompleteAuditTags(req.Preference)
	return plan, nil
}
