package service

import (
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

const (
	executionBackendRemoteSandbox        = "remote_sandbox"
	executionBackendLocalHost            = "local_host"
	executionBackendLocalPlatformSandbox = "local_platform_sandbox"

	metaExecutionBackend = "execution_backend"
	metaDegraded         = "degraded"
	metaBackendLegacy    = "backend"
)

func detectDegradedFromWarnings(warnings []string) bool {
	for _, w := range warnings {
		if strings.Contains(w, "已降级到本地") {
			return true
		}
	}
	return false
}

func resolveExecutionBackend(sandbox execmodel.SandboxProfile, intendedRemote, degraded bool) string {
	if intendedRemote && !degraded {
		return executionBackendRemoteSandbox
	}
	provider := strings.TrimSpace(sandbox.Provider)
	if strings.EqualFold(provider, "local-platform") || strings.EqualFold(provider, "local_platform") {
		return executionBackendLocalPlatformSandbox
	}
	return executionBackendLocalHost
}

func legacyBackendAlias(executionBackend string) string {
	switch executionBackend {
	case executionBackendRemoteSandbox:
		return "remote_session"
	default:
		return "local"
	}
}

// attachExecutionPathContext 只向模型暴露 execution_backend/degraded，不再返回物理 path_map。
func attachExecutionPathContext(out *scriptcontract.RunResult, executionBackend string, degraded bool) {
	if out == nil {
		return
	}
	if out.Metadata == nil {
		out.Metadata = map[string]string{}
	}
	out.Metadata[metaExecutionBackend] = executionBackend
	if degraded {
		out.Metadata[metaDegraded] = "true"
	} else {
		out.Metadata[metaDegraded] = "false"
	}
	// 兼容旧字段；以生效 backend 为准（降级后不再谎报 remote_session）。
	out.Metadata[metaBackendLegacy] = legacyBackendAlias(executionBackend)
}
