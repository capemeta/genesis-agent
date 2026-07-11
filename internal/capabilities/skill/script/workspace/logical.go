package workspace

import (
	"fmt"
	"path/filepath"
	"strings"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// LogicalPrefix 是 Genesis 逻辑目录前缀（与脚本环境变量同名）。
type LogicalPrefix string

const (
	LogicalOutputDir LogicalPrefix = "$OUTPUT_DIR"
	LogicalInputDir  LogicalPrefix = "$INPUT_DIR"
	LogicalWorkDir   LogicalPrefix = "$WORK_DIR"
	LogicalTmpDir    LogicalPrefix = "$TMPDIR"
	LogicalSkillDir  LogicalPrefix = "$SKILL_DIR"
)

// LogicalRel 表示逻辑目录前缀 + 相对路径。
type LogicalRel struct {
	Prefix LogicalPrefix
	Rest   string
}

// StripLogicalDirPrefix 解析 $WORK_DIR/foo 这类逻辑路径。
func StripLogicalDirPrefix(raw string) (LogicalRel, bool) {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	for _, prefix := range []LogicalPrefix{
		LogicalOutputDir, LogicalInputDir, LogicalWorkDir, LogicalTmpDir, LogicalSkillDir,
	} {
		p := string(prefix)
		if normalized == p {
			return LogicalRel{Prefix: prefix, Rest: "."}, true
		}
		if strings.HasPrefix(normalized, p+"/") {
			return LogicalRel{Prefix: prefix, Rest: strings.TrimPrefix(normalized, p+"/")}, true
		}
	}
	return LogicalRel{}, false
}

// DirBase 返回逻辑前缀对应的物理目录。
func DirBase(prefix LogicalPrefix, ws execmodel.ExecutionWorkspace) string {
	switch prefix {
	case LogicalOutputDir:
		return ws.OutputDir
	case LogicalInputDir:
		return ws.InputDir
	case LogicalWorkDir:
		return ws.WorkDir
	case LogicalTmpDir:
		return ws.TmpDir
	case LogicalSkillDir:
		return ws.SkillDir
	default:
		return ""
	}
}

// ExpandLogicalPath 将逻辑路径展开为绝对路径；非逻辑路径返回 ok=false。
func ExpandLogicalPath(raw string, ws execmodel.ExecutionWorkspace) (string, bool, error) {
	rel, ok := StripLogicalDirPrefix(raw)
	if !ok {
		return "", false, nil
	}
	base := DirBase(rel.Prefix, ws)
	if strings.TrimSpace(base) == "" {
		return "", false, fmt.Errorf("逻辑目录 %s 未注入", rel.Prefix)
	}
	if rel.Rest == "." || rel.Rest == "" {
		return base, true, nil
	}
	return filepath.Join(base, filepath.FromSlash(rel.Rest)), true, nil
}
