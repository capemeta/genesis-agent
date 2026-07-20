package sandbox

import (
	"fmt"
	"path/filepath"
)

// Type 描述本机平台沙箱实现类型。
type Type string

const (
	TypeNone                      Type = "none"
	TypeMacOSSeatbelt             Type = "macos_seatbelt"
	TypeLinuxBubblewrap           Type = "linux_bubblewrap"
	TypeLinuxLandlock             Type = "linux_landlock"
	TypeWindowsProcessConstrained Type = "windows_process_constrained"
	TypeWindowsAppContainer       Type = "windows_appcontainer"
	TypeExternal                  Type = "external"
)

// 兼容旧骨架中的名字。
const (
	TypeWindowsToken     Type = TypeWindowsProcessConstrained
	TypeWindowsJobObject Type = TypeWindowsProcessConstrained
)

// Preference 描述产品侧对本机平台沙箱的偏好。
type Preference string

const (
	PreferenceDisabled Preference = "disabled"
	PreferenceAuto     Preference = "auto"
	PreferenceRequired Preference = "required"
)

// 兼容旧骨架中的名字。
const (
	PreferenceForbid  Preference = PreferenceDisabled
	PreferenceRequire Preference = PreferenceRequired
)

// EnforcementLevel 描述当前 plan 实际能提供的隔离等级。
type EnforcementLevel string

const (
	EnforcementNone               EnforcementLevel = "none"
	EnforcementProcessConstrained EnforcementLevel = "process_constrained"
	EnforcementFilesystem         EnforcementLevel = "filesystem"
	EnforcementFilesystemNetwork  EnforcementLevel = "filesystem_network"
)

// NetworkMode 描述网络沙箱策略。
type NetworkMode string

const (
	NetworkFullAccess NetworkMode = "full_access"
	NetworkDisabled   NetworkMode = "disabled"
	NetworkProxyOnly  NetworkMode = "proxy_only"
	NetworkLoopback   NetworkMode = "loopback_only"
)

// PathRule 描述一个平台无关路径规则。
type PathRule struct {
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"`
}

// FileSystemPolicy 描述平台无关文件系统沙箱策略。
type FileSystemPolicy struct {
	ReadableRoots           []string   `json:"readable_roots,omitempty"`
	WritableRoots           []string   `json:"writable_roots,omitempty"`
	ReadOnlyRoots           []string   `json:"read_only_roots,omitempty"`
	UnreadablePaths         []PathRule `json:"unreadable_paths,omitempty"`
	ProtectedMetadataNames  []string   `json:"protected_metadata_names,omitempty"`
	ProtectedMetadataPaths  []string   `json:"protected_metadata_paths,omitempty"`
	IncludePlatformDefaults bool       `json:"include_platform_defaults,omitempty"`
	AllowFullDiskRead       bool       `json:"allow_full_disk_read,omitempty"`
	AllowFullDiskWrite      bool       `json:"allow_full_disk_write,omitempty"`
}

// IsZero 返回文件系统策略是否未配置。
func (p FileSystemPolicy) IsZero() bool {
	return len(p.ReadableRoots) == 0 && len(p.WritableRoots) == 0 && len(p.ReadOnlyRoots) == 0 && len(p.UnreadablePaths) == 0 && len(p.ProtectedMetadataNames) == 0 && len(p.ProtectedMetadataPaths) == 0 && !p.IncludePlatformDefaults && !p.AllowFullDiskRead && !p.AllowFullDiskWrite
}

// RequiresFilesystemSandbox 返回策略是否需要真实文件系统隔离。
func (p FileSystemPolicy) RequiresFilesystemSandbox() bool {
	if len(p.ReadableRoots) > 0 || len(p.WritableRoots) > 0 || len(p.ReadOnlyRoots) > 0 || len(p.UnreadablePaths) > 0 || len(p.ProtectedMetadataNames) > 0 || len(p.ProtectedMetadataPaths) > 0 || p.IncludePlatformDefaults {
		return true
	}
	return !p.AllowFullDiskRead || !p.AllowFullDiskWrite
}

// ProcessPolicy 描述进程级沙箱策略。
type ProcessPolicy struct {
	KillProcessTree bool `json:"kill_process_tree,omitempty"`
	ConstrainToken  bool `json:"constrain_token,omitempty"`
	PTYAllowed      bool `json:"pty_allowed,omitempty"`
}

// RequiresProcessConfinement 返回策略是否需要进程级约束。
func (p ProcessPolicy) RequiresProcessConfinement() bool {
	return p.KillProcessTree || p.ConstrainToken
}

// Profile 是本地平台沙箱策略的聚合。
type Profile struct {
	FileSystem       FileSystemPolicy  `json:"file_system"`
	Network          NetworkMode       `json:"network"`
	Process          ProcessPolicy     `json:"process"`
	ProxyPorts       []int             `json:"proxy_ports,omitempty"`
	AllowUnixSockets []string          `json:"allow_unix_sockets,omitempty"`
	ProxyEnv         map[string]string `json:"proxy_env,omitempty"`
}

// RequiresPlatformSandbox 返回策略是否需要本地平台沙箱。
func (p Profile) RequiresPlatformSandbox() bool {
	return p.FileSystem.RequiresFilesystemSandbox() || p.Network == NetworkDisabled || p.Network == NetworkProxyOnly || p.Network == NetworkLoopback || p.Process.RequiresProcessConfinement()
}

// CommandSpec 保留结构化 argv/env/cwd。
type CommandSpec struct {
	Argv []string          `json:"argv"`
	Env  map[string]string `json:"env,omitempty"`
	Cwd  string            `json:"cwd,omitempty"`
}

// Clone 复制命令规格。
func (c CommandSpec) Clone() CommandSpec {
	out := CommandSpec{Cwd: c.Cwd}
	out.Argv = append([]string{}, c.Argv...)
	if len(c.Env) > 0 {
		out.Env = make(map[string]string, len(c.Env))
		for k, v := range c.Env {
			out.Env[k] = v
		}
	}
	return out
}

// BuildRequest 是构造 sandbox plan 的输入。
type BuildRequest struct {
	Preference       Preference  `json:"preference"`
	Command          CommandSpec `json:"command"`
	Profile          Profile     `json:"profile"`
	SandboxPolicyCwd string      `json:"sandbox_policy_cwd,omitempty"`
	WorkspaceRoots   []string    `json:"workspace_roots,omitempty"`
	Writables        []string    `json:"writable_roots,omitempty"`
}

// Validate 校验请求。
func (r BuildRequest) Validate() error {
	if len(r.Command.Argv) == 0 || r.Command.Argv[0] == "" {
		return fmt.Errorf("sandbox command argv不能为空")
	}
	if r.Command.Cwd != "" && !filepath.IsAbs(r.Command.Cwd) {
		return fmt.Errorf("command cwd必须是绝对路径: %s", r.Command.Cwd)
	}
	return nil
}

func (r BuildRequest) withDefaults() BuildRequest {
	if r.Preference == "" {
		r.Preference = PreferenceAuto
	}
	if r.Profile.Network == "" {
		r.Profile.Network = NetworkFullAccess
	}
	if r.Profile.FileSystem.IsZero() {
		r.Profile.FileSystem.AllowFullDiskRead = true
		r.Profile.FileSystem.AllowFullDiskWrite = true
	}
	return r
}

// Capability 描述当前主机可用的沙箱能力。
type Capability struct {
	Type        Type             `json:"type"`
	Available   bool             `json:"available"`
	Enforcement EnforcementLevel `json:"enforcement,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	HelperPath  string           `json:"helper_path,omitempty"`
}

// Plan 是平台沙箱构造结果。
type Plan struct {
	Type                    Type              `json:"type"`
	Enforcement             EnforcementLevel  `json:"enforcement"`
	Command                 CommandSpec       `json:"command"`
	HelperPath              string            `json:"helper_path,omitempty"`
	FileSystemPolicy        FileSystemPolicy  `json:"filesystem_policy_effective"`
	NetworkPolicy           NetworkMode       `json:"network_policy_effective"`
	ProcessPolicy           ProcessPolicy     `json:"process_policy_effective"`
	Warnings                []string          `json:"warnings,omitempty"`
	UnsupportedReasons      []string          `json:"unsupported_reasons,omitempty"`
	AuditTags               map[string]string `json:"audit_tags,omitempty"`
	Degraded                bool              `json:"degraded,omitempty"`
	EffectiveSandboxProfile Profile           `json:"effective_sandbox_profile"`
	WindowsLevel            string            `json:"windows_level,omitempty"`
}

// CompleteAuditTags 补齐审计字段。
func (p *Plan) CompleteAuditTags(preference Preference) {
	if p.AuditTags == nil {
		p.AuditTags = map[string]string{}
	}
	p.AuditTags["sandbox.kind"] = string(p.Type)
	p.AuditTags["sandbox.enforcement_level"] = string(p.Enforcement)
	p.AuditTags["sandbox.preference"] = string(preference)
	p.AuditTags["sandbox.effective_network_policy"] = string(p.NetworkPolicy)
	if p.WindowsLevel != "" {
		p.AuditTags["sandbox.windows_level"] = p.WindowsLevel
	} else if p.Type == TypeNone {
		p.AuditTags["sandbox.windows_level"] = "disabled"
	}
	if p.HelperPath != "" {
		p.AuditTags["sandbox.helper_path"] = p.HelperPath
	}
	if p.Degraded {
		p.AuditTags["sandbox.degraded"] = "true"
	}
	if len(p.Warnings) > 0 {
		p.AuditTags["sandbox.warnings"] = joinStrings(p.Warnings, "; ")
	}
}
