package service

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ErrCommandInlineRisky 表示 python -c / node -e 内联过长或含换行，在 Windows/远程 shell 下极易因引号失败。
const ErrCommandInlineRisky = "COMMAND_INLINE_RISKY"

// 允许的极短单行探测上限（字符，非整条 command，而是 -c/-e 载荷）。
const maxSafeInlinePayloadRunes = 80

var (
	rePythonDashC = regexp.MustCompile(`(?i)(?:^|[\s;&|])(?:python3?|py)\s+(?:-[^\s]*\s+)*-c\s+`)
	reNodeDashE   = regexp.MustCompile(`(?i)(?:^|[\s;&|])node\s+(?:-[^\s]*\s+)*(?:-e|--eval)\s+`)
)

type inlineCommandRisk struct {
	Kind    string // python_c | node_e
	Reason  string
	Payload string
}

// detectRiskyInlineCommand 识别高风险内联：多行、过长、或载荷内再嵌套引号。
// 极短单行探测（如 python -c "import docx"）放行。
func detectRiskyInlineCommand(command string) (inlineCommandRisk, bool) {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return inlineCommandRisk{}, false
	}
	kind, payload, ok := extractInlinePayload(cmd)
	if !ok {
		return inlineCommandRisk{}, false
	}
	if strings.ContainsAny(payload, "\r\n") {
		return inlineCommandRisk{
			Kind:    kind,
			Reason:  "multiline",
			Payload: payload,
		}, true
	}
	if utf8.RuneCountInString(payload) > maxSafeInlinePayloadRunes {
		return inlineCommandRisk{
			Kind:    kind,
			Reason:  "too_long",
			Payload: payload,
		}, true
	}
	// 原始 command 含转义引号时，Windows cmd/PowerShell 嵌套极易炸。
	if strings.Contains(command, `\"`) || strings.Contains(command, `\'`) {
		return inlineCommandRisk{
			Kind:    kind,
			Reason:  "escaped_quotes",
			Payload: payload,
		}, true
	}
	return inlineCommandRisk{}, false
}

func extractInlinePayload(command string) (kind, payload string, ok bool) {
	normalized := command
	if loc := rePythonDashC.FindStringIndex(normalized); loc != nil {
		rest := strings.TrimSpace(normalized[loc[1]:])
		return "python_c", unquoteInlineArg(rest), true
	}
	if loc := reNodeDashE.FindStringIndex(normalized); loc != nil {
		rest := strings.TrimSpace(normalized[loc[1]:])
		return "node_e", unquoteInlineArg(rest), true
	}
	return "", "", false
}

func unquoteInlineArg(rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	quote := rest[0]
	if quote == '"' || quote == '\'' {
		// 取配对引号内内容；找不到结尾则整段视为载荷（通常已含换行）。
		end := strings.IndexByte(rest[1:], quote)
		if end >= 0 {
			return rest[1 : 1+end]
		}
		return rest[1:]
	}
	// 无引号：取到 shell 分隔符前
	for i, r := range rest {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ';' || r == '&' || r == '|' {
			return rest[:i]
		}
	}
	return rest
}

func errCommandInlineRisky(command string, risk inlineCommandRisk) error {
	hint := `请改用：write_file("$WORK_DIR/check.py", ...) → run_skill_command(command="python check.py", inputs=["$WORK_DIR/check.py"])（node 同理）。仅允许极短单行探测（无换行、载荷≤80字符、无嵌套引号）。`
	return fmt.Errorf(
		"%s: %s 内联代码风险=%s（本地宿主与远程 sandbox 的 shell 引号均易失败）。%s got=%q",
		ErrCommandInlineRisky,
		risk.Kind,
		risk.Reason,
		hint,
		command,
	)
}
