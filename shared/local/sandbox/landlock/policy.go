// Package landlock 提供 Linux Landlock fallback 能力判断。
package landlock

// NetworkMode 描述网络策略。
type NetworkMode string

const (
	NetworkFullAccess NetworkMode = "full_access"
	NetworkDisabled   NetworkMode = "disabled"
	NetworkProxyOnly  NetworkMode = "proxy_only"
	NetworkLoopback   NetworkMode = "loopback_only"
)

// FileSystemPolicy 描述 Landlock 可判断的文件策略。
type FileSystemPolicy struct {
	WritableRoots          []string
	UnreadablePaths        []string
	ProtectedMetadataPaths []string
	AllowFullDiskRead      bool
	AllowFullDiskWrite     bool
}

// SupportResult 描述策略是否可由 Landlock 表达。
type SupportResult struct {
	Supported bool
	Reasons   []string
}

// EvaluateSupport 判断 Landlock 是否能表达当前策略。
func EvaluateSupport(fs FileSystemPolicy, network NetworkMode) SupportResult {
	var reasons []string
	if network == NetworkDisabled || network == NetworkProxyOnly || network == NetworkLoopback {
		reasons = append(reasons, "Landlock不能表达网络隔离策略")
	}
	if len(fs.UnreadablePaths) > 0 {
		reasons = append(reasons, "Landlock fallback不支持完整deny-read策略")
	}
	if len(fs.ProtectedMetadataPaths) > 0 {
		reasons = append(reasons, "Landlock fallback不能提供protected metadata强保证")
	}
	if !fs.AllowFullDiskRead {
		reasons = append(reasons, "Landlock fallback当前仅支持读全盘、写受限的策略")
	}
	return SupportResult{Supported: len(reasons) == 0, Reasons: reasons}
}
