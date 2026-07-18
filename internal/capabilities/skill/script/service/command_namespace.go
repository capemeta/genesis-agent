package service

import (
	"fmt"
	"strings"
)

// ErrCommandLogicalPrefix 表示 command 误含控制面逻辑前缀；命令只消费 staging 后的相对名称。
const ErrCommandLogicalPrefix = "COMMAND_LOGICAL_PREFIX_FORBIDDEN"

var commandLogicalPrefixes = []string{"$WORK_DIR", "$INPUT_DIR", "$OUTPUT_DIR", "$TMPDIR", "$TMP_DIR", "$SKILL_DIR"}

func findLogicalPrefixInCommand(command string) string {
	for _, prefix := range commandLogicalPrefixes {
		if strings.Contains(strings.TrimSpace(command), prefix) {
			return prefix
		}
	}
	return ""
}

func errCommandLogicalPrefix(command, prefix string) error {
	return fmt.Errorf("%s: command 禁止包含逻辑目录字面量 %s；请通过 InputRef staging 后使用映射的相对名称。got=%q", ErrCommandLogicalPrefix, prefix, command)
}
