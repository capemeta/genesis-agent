// Package seatbelt 提供 macOS Seatbelt/sandbox-exec plan builder。
package seatbelt

const Path = "/usr/bin/sandbox-exec"

// CommandSpec 是结构化命令。
type CommandSpec struct {
	Argv []string
	Env  map[string]string
	Cwd  string
}

// FileSystemPolicy 是 Seatbelt builder 需要的文件策略。
type FileSystemPolicy struct {
	ReadableRoots          []string
	WritableRoots          []string
	ReadOnlyRoots          []string
	UnreadablePaths        []string
	ProtectedMetadataPaths []string
	AllowFullDiskRead      bool
	AllowFullDiskWrite     bool
}

// NetworkPolicy 是 Seatbelt builder 需要的网络策略。
type NetworkPolicy string

const (
	NetworkFullAccess NetworkPolicy = "full_access"
	NetworkDisabled   NetworkPolicy = "disabled"
	NetworkProxyOnly  NetworkPolicy = "proxy_only"
	NetworkLoopback   NetworkPolicy = "loopback_only"
)

// BuildOptions 控制 Seatbelt argv 构造。
type BuildOptions struct {
	Command          CommandSpec
	FileSystem       FileSystemPolicy
	Network          NetworkPolicy
	ProxyPorts       []int
	AllowUnixSockets []string
}

// Plan 是 Seatbelt 构造结果。
type Plan struct {
	Program string
	Args    []string
	Profile string
}

// Build 构造 sandbox-exec argv。它只包裹结构化 argv，不解释命令内容。
func Build(opts BuildOptions) (*Plan, error) {
	if len(opts.Command.Argv) == 0 || opts.Command.Argv[0] == "" {
		return nil, ErrInvalidCommand
	}
	profile := BuildProfile(opts.FileSystem, opts.Network, opts.ProxyPorts, opts.AllowUnixSockets)
	args := []string{"-p", profile, "--"}
	args = append(args, opts.Command.Argv...)
	return &Plan{Program: Path, Args: args, Profile: profile}, nil
}
