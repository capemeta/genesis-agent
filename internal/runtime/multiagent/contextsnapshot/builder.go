// Package contextsnapshot 构造最小授权的子智能体输入快照。
package contextsnapshot

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/multiagent/sanitize"
)

const boundaryMessage = "以上内容仅为只读背景；父线程工具、权限、审批与未列出的资源在子线程不可用。仅完成下方委派任务。"

// Mode 是父上下文传递模式。
type Mode string

const (
	ModeIsolated        Mode = "isolated"
	ModeLastNTurns      Mode = "last_n_turns"
	ModeFilteredHistory Mode = "filtered_history"
	ModeSkillIsolated   Mode = "skill_isolated"
)

// DelegationEnvelope 是不可信任务文本与可信运行约束之间的固定边界。
type DelegationEnvelope struct {
	TaskID         string
	ParentRunID    string
	ToolCallID     string
	PromptOrigin   string
	Objective      string
	ExpectedOutput string
	Capabilities   []string
	MaxTurns       int
	MaxTokens      int64
	MaxToolCalls   int
	ReturnContract string
}

// Input 是纯 Builder 的输入；Messages 必须已经由 Source flush/materialize 后读取。
type Input struct {
	Mode            Mode
	Messages        []*domain.Message
	LastNTurns      int
	MaxRunes        int
	Delegation      DelegationEnvelope
	RuntimeContract string
}

// Output 是供 Controller/Task 组装子 Run 的安全输入。
type Output struct {
	SystemContract string
	UserInput      string
	Snapshot       []*domain.Message
	Truncated      bool
	Omitted        []string
}

// Builder 不持有 I/O；父 transcript 的读取和 flush 由调用侧 Source 完成。
type Builder struct{ Sanitizer sanitize.Text }

// Build 从白名单消息构造子 Agent 输入。
func (b Builder) Build(input Input) (Output, error) {
	if err := validateEnvelope(input.Delegation); err != nil {
		return Output{}, err
	}
	objective, err := b.sanitizer().Sanitize(input.Delegation.Objective)
	if err != nil {
		return Output{}, fmt.Errorf("委派任务脱敏失败: %w", err)
	}
	input.Delegation.Objective = objective
	mandatoryInput := renderUserInput(nil, input.Delegation)
	if input.MaxRunes > 0 && runeLen(mandatoryInput)+runeLen(input.RuntimeContract) > input.MaxRunes {
		return Output{}, fmt.Errorf("subagent input budget exceeded: mandatory contract")
	}

	out := Output{SystemContract: strings.TrimSpace(input.RuntimeContract)}
	if input.Mode != ModeIsolated && input.Mode != ModeSkillIsolated {
		selected := selectMessages(input.Messages, input.Mode, input.LastNTurns)
		for _, message := range selected {
			if copied, ok := filterMessage(message); ok {
				cleaned, err := b.sanitizer().Sanitize(copied.Content)
				if err != nil {
					return Output{}, fmt.Errorf("父上下文脱敏失败: %w", err)
				}
				copied.Content = cleaned
				out.Snapshot = append(out.Snapshot, copied)
			} else {
				out.Omitted = append(out.Omitted, "parent_message")
			}
		}
	}
	snapshotBudget := input.MaxRunes - runeLen(mandatoryInput) - runeLen(input.RuntimeContract)
	out.Snapshot, out.Truncated = trimSnapshot(out.Snapshot, snapshotBudget)
	out.UserInput = renderUserInput(out.Snapshot, input.Delegation)
	return out, nil
}

func validateEnvelope(envelope DelegationEnvelope) error {
	objective := strings.TrimSpace(envelope.Objective)
	if objective == "" {
		return fmt.Errorf("subagent delegation objective不能为空")
	}
	if !utf8.ValidString(objective) || strings.ContainsRune(objective, '\x00') {
		return fmt.Errorf("subagent delegation objective编码非法")
	}
	return nil
}

func selectMessages(messages []*domain.Message, mode Mode, lastNTurns int) []*domain.Message {
	if mode == ModeFilteredHistory {
		return messages
	}
	if mode != ModeLastNTurns || lastNTurns <= 0 {
		return nil
	}
	start := len(messages)
	seenTurns := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && messages[i].NormalizedKind() == domain.MessageKindUserTurn {
			seenTurns++
			if seenTurns > lastNTurns {
				start = i + 1
				break
			}
		}
		start = i
	}
	return messages[start:]
}

func filterMessage(message *domain.Message) (*domain.Message, bool) {
	if message == nil || strings.TrimSpace(message.Content) == "" {
		return nil, false
	}
	kind := message.NormalizedKind()
	if message.Role != domain.RoleUser && message.Role != domain.RoleAssistant {
		return nil, false
	}
	if kind != domain.MessageKindUserTurn && kind != domain.MessageKindAssistant && kind != domain.MessageKindConversationSummary {
		return nil, false
	}
	if (kind == domain.MessageKindUserTurn || kind == domain.MessageKindConversationSummary) && message.Role != domain.RoleUser {
		return nil, false
	}
	if kind == domain.MessageKindAssistant && message.Role != domain.RoleAssistant {
		return nil, false
	}
	if kind == domain.MessageKindAssistant && (len(message.ToolCalls) > 0 || strings.TrimSpace(message.ReasoningContent) != "") {
		return nil, false
	}
	copy := *message
	copy.ToolCalls = nil
	copy.ToolCallID = ""
	copy.ReasoningContent = ""
	return &copy, true
}

func trimSnapshot(messages []*domain.Message, budget int) ([]*domain.Message, bool) {
	if budget <= 0 || len(messages) == 0 {
		return nil, len(messages) > 0
	}
	used := 0
	start := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		length := runeLen(messages[i].Content)
		if used+length > budget {
			break
		}
		used += length
		start = i
	}
	return messages[start:], start > 0
}

func renderUserInput(snapshot []*domain.Message, envelope DelegationEnvelope) string {
	var builder strings.Builder
	for _, message := range snapshot {
		fmt.Fprintf(&builder, "[背景 %s]\n%s\n\n", message.Role, strings.TrimSpace(message.Content))
	}
	if len(snapshot) > 0 {
		builder.WriteString(boundaryMessage)
		builder.WriteString("\n\n")
	}
	builder.WriteString("[委派任务]\n")
	builder.WriteString(strings.TrimSpace(envelope.Objective))
	if output := strings.TrimSpace(envelope.ExpectedOutput); output != "" {
		builder.WriteString("\n\n[期望输出]\n")
		builder.WriteString(output)
	}
	builder.WriteString("\n\n[回传约束]\n")
	contract := strings.TrimSpace(envelope.ReturnContract)
	if contract == "" {
		contract = "仅返回结论、已验证证据和已登记产物；不要回放完整过程或敏感原文。"
	}
	builder.WriteString(contract)
	return builder.String()
}

func runeLen(value string) int { return utf8.RuneCountInString(value) }

func (b Builder) sanitizer() sanitize.Text {
	if b.Sanitizer != nil {
		return b.Sanitizer
	}
	return sanitize.Default{}
}
