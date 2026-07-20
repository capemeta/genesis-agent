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

	requiresFS := req.Profile.FileSystem.RequiresFilesystemSandbox()
	requiresNet := req.Profile.Network == NetworkDisabled || req.Profile.Network == NetworkProxyOnly || req.Profile.Network == NetworkLoopback

	argv, err := win.BuildProcessConstrainedPlan(req.Command.Argv)
	if err != nil {
		return nil, NewError(ErrCodeSandboxInitFailed, err)
	}

	// Clone environment and prepare env map
	env := make(map[string]string)
	for k, v := range req.Command.Env {
		env[k] = v
	}

	var windowsLevel string
	var enforcement EnforcementLevel
	var warnings []string

	if requiresNet {
		if !win.IsWindowsNetworkSetupReady() {
			return nil, NewError(ErrCodePolicyUnsupported, nil).WithReason(
				"Windows本地平台沙箱网络隔离未就绪。要执行网络隔离，请以管理员方式打开终端运行 'genesis-cli sandbox windows-setup --network' 来初始化网络沙箱规则，或者在配置文件中配置远程沙箱服务(mode: remote_sandbox/docker_sandbox)。",
			)
		}
		if win.IsFirewallUnsupported() {
			warnings = append(warnings, "Windows本地平台沙箱网络隔离因家庭版系统限制未生效，已自动降级为仅启用文件与 Token 隔离。")
		}

		workspacePath := "."
		if len(req.WorkspaceRoots) > 0 {
			workspacePath = req.WorkspaceRoots[0]
		}

		// Apply ACLs to write roots and deny read paths for the sandbox user
		var unreadables []string
		for _, rule := range req.Profile.FileSystem.UnreadablePaths {
			unreadables = append(unreadables, rule.Path)
		}

		// In L3, the sandbox user must have read/write access to the workspace and write roots
		sandboxUser := win.GetSandboxUsername()
		err = win.ApplyWorkspaceACLsForUser(workspacePath, sandboxUser, req.Profile.FileSystem.WritableRoots, unreadables)
		if err != nil {
			return nil, NewError(ErrCodeSandboxInitFailed, err)
		}

		// Pass sandbox user and override profile environment variables
		env["GENESIS_SANDBOX_USER"] = sandboxUser
		env["USERPROFILE"] = `C:\Users\` + sandboxUser
		env["HOME"] = `C:\Users\` + sandboxUser
		env["HOMEPATH"] = `\Users\` + sandboxUser
		env["APPDATA"] = `C:\Users\` + sandboxUser + `\AppData\Roaming`
		env["LOCALAPPDATA"] = `C:\Users\` + sandboxUser + `\AppData\Local`

		windowsLevel = "genesis_sandbox_user"
		enforcement = EnforcementFilesystemNetwork
	} else if requiresFS {
		if !win.IsWindowsSetupReady() {
			return nil, NewError(ErrCodePolicyUnsupported, nil).WithReason(
				"Windows本地平台沙箱未初始化。要执行文件系统隔离，请以管理员或普通用户在命令行运行 'genesis-cli sandbox windows-setup' 来初始化沙箱环境，或者在配置文件中配置远程沙箱服务(mode: remote_sandbox/docker_sandbox)。",
			)
		}

		workspacePath := "."
		if len(req.WorkspaceRoots) > 0 {
			workspacePath = req.WorkspaceRoots[0]
		}

		// Calculate capability SID for the workspace
		sid, err := win.GetWorkspaceCapabilitySID(workspacePath)
		if err != nil {
			return nil, NewError(ErrCodeSandboxInitFailed, err)
		}

		// Apply ACLs to write roots and deny read paths
		var unreadables []string
		for _, rule := range req.Profile.FileSystem.UnreadablePaths {
			unreadables = append(unreadables, rule.Path)
		}
		
		// Setup write roots to allow the capability SID, and deny read paths
		err = win.ApplyWorkspaceACLs(workspacePath, req.Profile.FileSystem.WritableRoots, unreadables)
		if err != nil {
			return nil, NewError(ErrCodeSandboxInitFailed, err)
		}

		// Pass the capability SID to execution stage via internal env var
		env["GENESIS_SANDBOX_CAP_SIDS"] = sid
		windowsLevel = "unelevated_acl"
		enforcement = EnforcementFilesystem
	} else {
		// L1 (process constrained)
		support := win.EvaluateProcessConstrainedSupport(
			win.FileSystemPolicy{RequiresFilesystem: false},
			win.NetworkMode(req.Profile.Network),
			win.ProcessPolicy{KillProcessTree: req.Profile.Process.KillProcessTree, ConstrainToken: req.Profile.Process.ConstrainToken},
		)
		if !support.Supported {
			return nil, NewError(ErrCodePolicyUnsupported, nil).WithReason(strings.Join(support.Reasons, "; "))
		}
		windowsLevel = "token_job"
		enforcement = EnforcementProcessConstrained
	}

	env = applyProxyEnv(env, req.Profile.Network, req.Profile.ProxyEnv)

	plan := &Plan{
		Type:                    TypeWindowsProcessConstrained,
		Enforcement:             enforcement,
		WindowsLevel:            windowsLevel,
		Command:                 CommandSpec{Argv: argv, Env: env, Cwd: req.Command.Cwd},
		FileSystemPolicy:        req.Profile.FileSystem,
		NetworkPolicy:           req.Profile.Network,
		ProcessPolicy:           req.Profile.Process,
		Warnings:                warnings,
		EffectiveSandboxProfile: req.Profile,
	}
	plan.CompleteAuditTags(req.Preference)
	return plan, nil
}

