package model

import (
	"fmt"
	"path/filepath"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// LogicalPrefix 是执行环境逻辑目录变量。
type LogicalPrefix string

const (
	LogicalOutputDir LogicalPrefix = "$OUTPUT_DIR"
	LogicalInputDir  LogicalPrefix = "$INPUT_DIR"
	LogicalWorkDir   LogicalPrefix = "$WORK_DIR"
	LogicalTmpDir    LogicalPrefix = "$TMPDIR"
	LogicalSkillDir  LogicalPrefix = "$SKILL_DIR"
)

// LogicalRel 表示逻辑目录前缀和其下相对路径。
type LogicalRel struct {
	Prefix LogicalPrefix
	Rest   string
}

// StripLogicalDirPrefix 解析逻辑目录路径（同时支持 $PREFIX/ 与 约定前缀 prefix/）。
func StripLogicalDirPrefix(raw string) (LogicalRel, bool) {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/")
	if normalized == "" {
		return LogicalRel{}, false
	}
	for strings.HasPrefix(normalized, "./") {
		normalized = strings.TrimPrefix(normalized, "./")
	}

	mappings := []struct {
		prefixes []string
		logical  LogicalPrefix
	}{
		{prefixes: []string{string(LogicalOutputDir), "output"}, logical: LogicalOutputDir},
		{prefixes: []string{string(LogicalInputDir), "input"}, logical: LogicalInputDir},
		{prefixes: []string{string(LogicalWorkDir), "work"}, logical: LogicalWorkDir},
		{prefixes: []string{string(LogicalTmpDir), "tmp"}, logical: LogicalTmpDir},
		{prefixes: []string{string(LogicalSkillDir), "skills"}, logical: LogicalSkillDir},
	}

	for _, m := range mappings {
		for _, p := range m.prefixes {
			if normalized == p {
				return LogicalRel{Prefix: m.logical, Rest: "."}, true
			}
			if strings.HasPrefix(normalized, p+"/") {
				return LogicalRel{Prefix: m.logical, Rest: strings.TrimPrefix(normalized, p+"/")}, true
			}
		}
	}
	return LogicalRel{}, false
}

// DirBase 返回逻辑目录的 backend 实际映射。
func DirBase(prefix LogicalPrefix, workspace execmodel.ExecutionWorkspace) string {
	switch prefix {
	case LogicalOutputDir:
		if workspace.OutputDir != "" {
			return workspace.OutputDir
		}
		return filepath.Join(workspace.WorkDir, "output")
	case LogicalInputDir:
		if workspace.InputDir != "" {
			return workspace.InputDir
		}
		return filepath.Join(workspace.WorkDir, "input")
	case LogicalWorkDir:
		return workspace.WorkDir
	case LogicalTmpDir:
		if workspace.TmpDir != "" {
			return workspace.TmpDir
		}
		return filepath.Join(workspace.WorkDir, "tmp")
	case LogicalSkillDir:
		if workspace.SkillDir != "" {
			return workspace.SkillDir
		}
		return filepath.Join(workspace.WorkDir, "skills")
	default:
		return ""
	}
}

// ExpandLogicalPath 将逻辑路径或相对路径展开到当前实际 ExecutionWorkspace。
func ExpandLogicalPath(raw string, workspace execmodel.ExecutionWorkspace) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, fmt.Errorf("路径不能为空")
	}

	// 1. 尝试匹配前缀（$OUTPUT_DIR、input/、output/ 等）
	rel, ok := StripLogicalDirPrefix(raw)
	if ok {
		base := DirBase(rel.Prefix, workspace)
		if strings.TrimSpace(base) == "" {
			return "", false, fmt.Errorf("逻辑目录 %s 未注入", rel.Prefix)
		}
		if rel.Rest == "." || rel.Rest == "" {
			return base, true, nil
		}
		workspacePath := WorkspacePath(strings.ReplaceAll(rel.Rest, `\`, "/"))
		if err := workspacePath.Validate(); err != nil {
			return "", false, err
		}
		return filepath.Join(base, filepath.FromSlash(string(workspacePath))), true, nil
	}

	// 2. 如果是绝对路径，原样返回（不以相对路径处理）
	normalized := strings.ReplaceAll(raw, `\`, "/")
	if filepath.IsAbs(raw) || strings.HasPrefix(normalized, "/") || (len(normalized) > 1 && normalized[1] == ':') {
		return raw, true, nil
	}

	// 3. 其它所有普通相对路径（例如 src/main.go、./src/main.go、README.md），基于 workspace.WorkDir 展开
	if strings.TrimSpace(workspace.WorkDir) == "" {
		return "", false, fmt.Errorf("工作区 WorkDir 未注入")
	}
	normalized = strings.TrimPrefix(normalized, "./")
	for strings.HasPrefix(normalized, "./") {
		normalized = strings.TrimPrefix(normalized, "./")
	}
	workspacePath := WorkspacePath(normalized)
	if err := workspacePath.Validate(); err != nil {
		return "", false, err
	}
	return filepath.Join(workspace.WorkDir, filepath.FromSlash(string(workspacePath))), true, nil
}

