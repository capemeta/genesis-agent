package service

import (
	"fmt"
	"regexp"
	"strings"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

var (
	versionProbePattern = regexp.MustCompile(`(?i)^\s*(node(?:\.exe)?|python(?:3)?(?:\.exe)?)\s+(--version|-v)\s*$`)
	nodeEvalPattern     = regexp.MustCompile(`(?is)^\s*node(?:\.exe)?\s+(?:-e|--eval)\s+(.+)$`)
	nodeRequirePattern  = regexp.MustCompile(`(?i)require\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	pythonEvalPattern   = regexp.MustCompile(`(?is)^\s*python(?:3)?(?:\.exe)?\s+-c\s+(.+)$`)
	pythonImportPattern = regexp.MustCompile(`(?i)(?:^|[;\s])(?:import\s+|from\s+)([a-zA-Z0-9_.-]+)`)
)

// redundantRuntimeProbe 仅识别无业务副作用的版本/import/require 探测。
// 正式业务命令仍由 preflight 按 Skill 声明的 runtime/profile 校验，避免为特定 Skill 写分支。
func redundantRuntimeProbe(command string, deps skillmodel.RuntimeDeps) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}
	if match := versionProbePattern.FindStringSubmatch(command); len(match) > 0 {
		head := strings.ToLower(match[1])
		if (strings.HasPrefix(head, "node") && len(deps.Node) > 0) || (strings.HasPrefix(head, "python") && len(deps.Python) > 0) {
			return fmt.Sprintf("runtime probe unnecessary: %s 已由 Skill runtime/profile 声明并在正式命令前自动 preflight", match[1]), true
		}
	}
	if match := nodeEvalPattern.FindStringSubmatch(command); len(match) > 0 {
		payload := strings.ToLower(match[1])
		if isSideEffectFreeProbePayload(payload) {
			for _, required := range nodeRequirePattern.FindAllStringSubmatch(payload, -1) {
				if len(required) > 1 && runtimePackageDeclared(required[1], deps.Node, true) {
					return fmt.Sprintf("runtime probe unnecessary: Node 依赖 %s 已在 Skill runtime 中声明并由 preflight 校验", required[1]), true
				}
			}
		}
	}
	if match := pythonEvalPattern.FindStringSubmatch(command); len(match) > 0 {
		payload := strings.Trim(strings.ToLower(strings.TrimSpace(match[1])), `"'`)
		if isSideEffectFreeProbePayload(payload) {
			for _, imported := range pythonImportPattern.FindAllStringSubmatch(payload, -1) {
				if len(imported) > 1 && runtimePackageDeclared(imported[1], deps.Python, false) {
					return fmt.Sprintf("runtime probe unnecessary: Python 依赖 %s 已在 Skill runtime 中声明并由 preflight 校验", imported[1]), true
				}
			}
		}
	}
	return "", false
}

func isSideEffectFreeProbePayload(payload string) bool {
	for _, forbidden := range []string{"writefile", "appendfile", "unlink", "remove", "rename", "exec(", "spawn(", "fetch(", "http.", "https.", "process.exit"} {
		if strings.Contains(payload, forbidden) {
			return false
		}
	}
	return true
}

func runtimePackageDeclared(requested string, packages []skillmodel.RuntimePackage, node bool) bool {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if strings.HasPrefix(requested, "@") {
		parts := strings.Split(requested, "/")
		if len(parts) >= 2 {
			requested = strings.Join(parts[:2], "/")
		}
	} else {
		requested = strings.Split(requested, "/")[0]
	}
	for _, pkg := range packages {
		candidates := []string{pkg.Name}
		if node {
			candidates = append(candidates, pkg.Require)
		} else {
			candidates = append(candidates, pkg.Import)
		}
		for _, candidate := range candidates {
			candidate = strings.ToLower(strings.TrimSpace(candidate))
			if candidate == requested || strings.Split(candidate, ".")[0] == strings.Split(requested, ".")[0] {
				return true
			}
		}
	}
	return false
}
