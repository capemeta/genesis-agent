// package windowssandbox 提供 Windows 本地进程约束沙箱能力判断。
package windowssandbox

// NetworkMode 描述网络策略。
type NetworkMode string

const (
	NetworkFullAccess NetworkMode = "full_access"
	NetworkDisabled   NetworkMode = "disabled"
	NetworkProxyOnly  NetworkMode = "proxy_only"
	NetworkLoopback   NetworkMode = "loopback_only"
)

// FileSystemPolicy 描述 Windows process-constrained 能力可判断的文件策略。
type FileSystemPolicy struct {
	RequiresFilesystem bool
}

// ProcessPolicy 描述进程约束。
type ProcessPolicy struct {
	KillProcessTree bool
	ConstrainToken  bool
}

// SupportResult 描述策略是否可支持。
type SupportResult struct {
	Supported bool
	Reasons   []string
}

// EvaluateProcessConstrainedSupport 判断 Restricted Token + Job Object 等轻量能力是否足够。
func EvaluateProcessConstrainedSupport(fs FileSystemPolicy, network NetworkMode, process ProcessPolicy) SupportResult {
	var reasons []string
	if fs.RequiresFilesystem {
		reasons = append(reasons, "Windows process_constrained不能声明为filesystem sandbox")
	}
	if network == NetworkDisabled || network == NetworkProxyOnly || network == NetworkLoopback {
		reasons = append(reasons, "Windows process_constrained不能表达网络隔离，需要WFP/AppContainer/外置执行环境")
	}
	return SupportResult{Supported: len(reasons) == 0, Reasons: reasons}
}

// BuildProcessConstrainedPlan 第一版不改写 argv，只声明进程约束语义。
func BuildProcessConstrainedPlan(command []string) ([]string, error) {
	if len(command) == 0 || command[0] == "" {
		return nil, ErrInvalidCommand
	}
	out := append([]string{}, command...)
	return out, nil
}

// EvaluateAppContainerSupport 返回 AppContainer 当前是否可作为本地强沙箱使用。
func EvaluateAppContainerSupport() SupportResult {
	return SupportResult{Supported: false, Reasons: []string{
		"Windows AppContainer需要profile创建、capability SID、文件ACL初始化和网络能力声明；当前本地实现未完成，必须fail-closed",
	}}
}
