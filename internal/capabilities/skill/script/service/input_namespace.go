package service

import (
	"fmt"
	"path"
	"strings"
)

// ErrInputPathNamespaceMismatch 表示控制面 inputs 误用了执行面绝对根（如 /workspace）。
const ErrInputPathNamespaceMismatch = "INPUT_PATH_NAMESPACE_MISMATCH"

// ErrCommandLogicalPrefix 表示 command 误含控制面逻辑前缀（如 $WORK_DIR）；本地宿主与远程沙箱均禁止。
const ErrCommandLogicalPrefix = "COMMAND_LOGICAL_PREFIX_FORBIDDEN"

var commandLogicalPrefixes = []string{
	"$WORK_DIR",
	"$INPUT_DIR",
	"$OUTPUT_DIR",
	"$TMPDIR",
	"$TMP_DIR",
	"$SKILL_DIR",
}

// isExecutionPlaneAbsoluteInput 判断是否为远程/容器执行面绝对路径，不可作为控制面 stage 源。
func isExecutionPlaneAbsoluteInput(raw string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if normalized == "" {
		return false
	}
	// 逻辑前缀属于控制面，放行。
	if strings.HasPrefix(normalized, "$") {
		return false
	}
	cleaned := path.Clean(normalized)
	if cleaned == "/workspace" || strings.HasPrefix(cleaned, "/workspace/") {
		return true
	}
	return false
}

func errInputPathNamespaceMismatch(raw string) error {
	return fmt.Errorf(
		"%s: inputs 属于执行面路径，不能用于控制面 stage。请改用 $WORK_DIR/相对名或工作区相对路径；若文件已在 Skill 工作目录则省略 inputs。got=%q",
		ErrInputPathNamespaceMismatch,
		raw,
	)
}

// findLogicalPrefixInCommand 若 command 含控制面逻辑目录字面量则返回该前缀（shell/远程均不展开 $WORK_DIR）。
func findLogicalPrefixInCommand(command string) string {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return ""
	}
	for _, prefix := range commandLogicalPrefixes {
		if strings.Contains(cmd, prefix) {
			return prefix
		}
	}
	return ""
}

func errCommandLogicalPrefix(command, prefix string) error {
	return fmt.Errorf(
		"%s: command 禁止包含逻辑目录字面量 %s（本地宿主与远程 sandbox 均不展开）。请用 inputs=[\"%s/相对名\"] stage 后，command 写相对文件名（例如 python create_pdfs.py）。got=%q",
		ErrCommandLogicalPrefix,
		prefix,
		prefix,
		command,
	)
}
