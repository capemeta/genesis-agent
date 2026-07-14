package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	scriptcontract "genesis-agent/internal/capabilities/skill/script/contract"
)

const (
	executionBackendRemoteSandbox        = "remote_sandbox"
	executionBackendLocalHost            = "local_host"
	executionBackendLocalPlatformSandbox = "local_platform_sandbox"

	metaExecutionBackend = "execution_backend"
	metaDegraded         = "degraded"
	metaPathMap          = "path_map"
	metaPathMapNote      = "path_map_note"
	metaBackendLegacy    = "backend"

	pathMapNoteText = "脚本内用环境变量；inputs/write_file 仍只用 $WORK_DIR/... ，禁止把 path_map 右侧路径写入 tool JSON"
)

type pathHintState struct {
	backend  string
	degraded bool
	lastUsed time.Time
}

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

func remotePathMap() map[string]string {
	return map[string]string{
		"WORK_DIR":   "/workspace",
		"INPUT_DIR":  "/workspace/input",
		"OUTPUT_DIR": "/workspace/output",
		"TMPDIR":     "/workspace/tmp",
		"SKILL_DIR":  "/workspace",
	}
}

func localPathMap(runID, packageID string) map[string]string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		runID = "unknown-run"
	}
	base := filepath.ToSlash(filepath.Join(".genesis", "runs", runID))
	skill := base + "/work"
	if pkg := strings.TrimSpace(packageID); pkg != "" {
		skill = base + "/work/skills/" + pkg
	}
	return map[string]string{
		"WORK_DIR":   base + "/work",
		"INPUT_DIR":  base + "/input",
		"OUTPUT_DIR": base + "/output",
		"TMPDIR":     base + "/tmp",
		"SKILL_DIR":  skill,
	}
}

func pathMapForBackend(executionBackend, runID, packageID string) map[string]string {
	if executionBackend == executionBackendRemoteSandbox {
		return remotePathMap()
	}
	return localPathMap(runID, packageID)
}

func (s *Service) shouldIncludePathMap(key, backend string, degraded bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pathHints == nil {
		s.pathHints = map[string]pathHintState{}
	}
	now := time.Now()
	prev, ok := s.pathHints[key]
	changed := !ok || prev.backend != backend || prev.degraded != degraded
	s.pathHints[key] = pathHintState{backend: backend, degraded: degraded, lastUsed: now}
	return changed
}

func attachExecutionPathContext(out *scriptcontract.RunResult, executionBackend string, degraded, includePathMap bool, runID, packageID string) {
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

	if !includePathMap {
		return
	}
	raw, err := json.Marshal(pathMapForBackend(executionBackend, runID, packageID))
	if err != nil {
		return
	}
	out.Metadata[metaPathMap] = string(raw)
	out.Metadata[metaPathMapNote] = pathMapNoteText
	if degraded {
		note := "路径地图已因 sandbox optional 降级刷新；请忽略此前远程 /workspace 映射，inputs 仍只用 $WORK_DIR/..."
		out.Warnings = append(out.Warnings, note)
	}
}

func formatPathContextKey(runID, skill string) string {
	return fmt.Sprintf("%s::%s", strings.TrimSpace(runID), strings.ToLower(strings.TrimSpace(skill)))
}
