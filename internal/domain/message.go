// Package domain - 对话消息类型定义
// Message 是整个 Agent 系统中信息流转的核心单元，不依赖任何外部框架
// 语义上与 OpenAI、Anthropic、eino 等主流框架的消息结构保持一致
package domain

import "strings"

// RoleType 消息发送方的角色类型（LLM 协议角色）
type RoleType string

const (
	RoleSystem    RoleType = "system"    // 系统提示词
	RoleUser      RoleType = "user"      // 用户侧（含真人输入与 runtime 注入）
	RoleAssistant RoleType = "assistant" // LLM回复（可能含工具调用）
	RoleTool      RoleType = "tool"      // 工具执行结果
)

// MessageKind 消息语义类型（UI 投影 / 压缩用户轮 / LTM 抽取的主开关）。
// 契约见 docs/会话管理与记忆管理设计方案.md §6.2；不采用必填 Origin。
type MessageKind string

const (
	MessageKindUserTurn            MessageKind = "user_turn"             // 真人输入
	MessageKindAssistant           MessageKind = "assistant"             // 模型回复（可含 tool_calls）
	MessageKindToolResult          MessageKind = "tool_result"           // role=tool
	MessageKindSkillInjection      MessageKind = "skill_injection"       // runtime 注入的 SKILL 全文（role=user）
	MessageKindConversationSummary MessageKind = "conversation_summary"  // compact 回填摘要（role=user）
	MessageKindReminder            MessageKind = "reminder"              // 提示词分层 L4 turn reminder 等
	MessageKindSystem              MessageKind = "system"                // 稳定 system / 宿主诊断
)

// 常见 Source（可选审计字段，不参与 UI/压缩主策略）
const (
	MessageSourceUser          = "user"
	MessageSourceSkillGateway  = "skill_gateway"
	MessageSourceSkillMention  = "skill_mention"
	MessageSourceCompactor     = "compactor"
	MessageSourcePromptCompose = "prompt_composer"
	MessageSourceRepeatGuard   = "repeat_guard"
)

// FunctionCall 工具调用的函数信息
type FunctionCall struct {
	Name      string // 工具名称
	Arguments string // JSON格式的参数字符串
}

// ToolCall LLM发出的单次工具调用请求
type ToolCall struct {
	ID       string       // 工具调用的唯一ID，用于关联后续的工具结果
	Type     string       // 固定为 "function"
	Function FunctionCall // 具体调用的函数信息
}

// Message 对话消息
// 不持有任何外部框架类型，可被任意层直接使用
type Message struct {
	// Role 消息发送方角色（协议层）
	Role RoleType `json:"role"`
	// Content 文本内容（assistant发起工具调用时可能为空）
	Content string `json:"content,omitempty"`
	// ToolCalls LLM请求调用的工具列表（仅 assistant 角色时有值）
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID 本消息对应的工具调用ID（仅 tool 角色时有值）
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ReasoningContent 推理/思考内容（仅 assistant 角色在支持CoT时有值）
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// Kind 语义类型（必填；旧数据为空时用 EnsureKind / NormalizedKind 按 Role 回退）
	Kind MessageKind `json:"kind,omitempty"`
	// Source 可选细粒度生产者（审计/调试）
	Source string `json:"source,omitempty"`
}

// NewSystemMessage 创建 system 角色消息（Kind=system）
func NewSystemMessage(content string) *Message {
	return &Message{Role: RoleSystem, Content: content, Kind: MessageKindSystem}
}

// NewUserMessage 创建真人用户输入（Kind=user_turn）
func NewUserMessage(content string) *Message {
	return &Message{Role: RoleUser, Content: content, Kind: MessageKindUserTurn, Source: MessageSourceUser}
}

// NewSkillInjectionMessage 创建 Skill 全文注入（role=user, Kind=skill_injection）
func NewSkillInjectionMessage(content string) *Message {
	return &Message{Role: RoleUser, Content: content, Kind: MessageKindSkillInjection}
}

// NewReminderMessage 创建 runtime reminder（role=user, Kind=reminder）
func NewReminderMessage(content string) *Message {
	return &Message{Role: RoleUser, Content: content, Kind: MessageKindReminder}
}

// NewConversationSummaryMessage 创建压缩摘要（role=user, Kind=conversation_summary）
func NewConversationSummaryMessage(content string) *Message {
	return &Message{
		Role:    RoleUser,
		Content: content,
		Kind:    MessageKindConversationSummary,
		Source:  MessageSourceCompactor,
	}
}

// NewAssistantMessage 创建助手回复消息（Kind=assistant）
func NewAssistantMessage(content string) *Message {
	return &Message{Role: RoleAssistant, Content: content, Kind: MessageKindAssistant}
}

// NewToolResultMessage 创建工具执行结果消息（role=tool, Kind=tool_result）
func NewToolResultMessage(toolCallID, result string) *Message {
	return &Message{
		Role:       RoleTool,
		Content:    result,
		ToolCallID: toolCallID,
		Kind:       MessageKindToolResult,
	}
}

// WithSource 设置审计 Source，返回自身便于链式调用。
func (m *Message) WithSource(source string) *Message {
	if m == nil {
		return nil
	}
	m.Source = source
	return m
}

// EnsureKind 若 Kind 为空则按 Role 回退填充（读旧数据 / LLM 回写时调用）。
func (m *Message) EnsureKind() {
	if m == nil {
		return
	}
	if m.Kind == "" {
		m.Kind = KindFromRole(m.Role)
	}
}

// KindFromRole 旧数据缺 Kind 时的保守回退（真人话宁可多展示）。
func KindFromRole(role RoleType) MessageKind {
	switch role {
	case RoleUser:
		return MessageKindUserTurn
	case RoleAssistant:
		return MessageKindAssistant
	case RoleTool:
		return MessageKindToolResult
	case RoleSystem:
		return MessageKindSystem
	default:
		return MessageKindSystem
	}
}

// NormalizedKind 返回有效 Kind（空则按 Role 回退，不写回）。
func (m *Message) NormalizedKind() MessageKind {
	if m == nil {
		return ""
	}
	if m.Kind != "" {
		return m.Kind
	}
	return KindFromRole(m.Role)
}

// IsChatVisible 按 Kind 判断是否可能进入默认聊天气泡（粗筛；不含正文细节）。
func IsChatVisible(kind MessageKind) bool {
	switch kind {
	case MessageKindUserTurn, MessageKindAssistant, MessageKindConversationSummary:
		return true
	default:
		return false
	}
}

// IsChatVisibleMessage 默认聊天气泡是否展示该条消息（Kind + 可见正文）。
// 纯 tool_calls、无 Content 的中间 assistant 不进默认气泡（轨迹留给 ForModel / 进度视图）。
func IsChatVisibleMessage(m *Message) bool {
	if m == nil {
		return false
	}
	kind := m.NormalizedKind()
	if !IsChatVisible(kind) {
		return false
	}
	if kind == MessageKindAssistant && strings.TrimSpace(m.Content) == "" {
		return false
	}
	return true
}

// IsRealUserTurn 是否计入压缩「真实用户轮」边界。
func IsRealUserTurn(kind MessageKind) bool {
	return kind == MessageKindUserTurn
}

// IncludeInLongTermExtract 是否允许进入 LTM 抽取输入。
func IncludeInLongTermExtract(kind MessageKind) bool {
	return kind == MessageKindUserTurn
}

// ForUI 投影为默认会话 UI 可见消息（同源存储，禁止另写一套）。
// 产品侧微调展示请用 internal/runtime/transcript.ProjectForUI + UIPolicy。
func ForUI(msgs []*Message) []*Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]*Message, 0, len(msgs))
	for _, m := range msgs {
		if IsChatVisibleMessage(m) {
			out = append(out, m)
		}
	}
	return out
}

// ForModel 投影为送给 LLM 的上下文（当前为全链；预算裁剪由 ContextAssembler 后续负责）。
func ForModel(msgs []*Message) []*Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]*Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		m.EnsureKind()
		out = append(out, m)
	}
	return out
}

// SessionMessagesFromRun 截取本 Run 应写入短期记忆的消息。
//
// all 布局约定（与 react_loop 一致）：
//   - [0]：本 Run 重建的基线 system（每次 BuildSystem，不落短期记忆）
//   - [1 : 1+historyLen]：已持久化历史（本轮不再重复 Append）
//   - 其余：本 Run 新增（user_turn / skill_injection / assistant / tool_result / 诊断 system 等）
//
// historyLen 为 GetHistory 返回条数；若无前置基线 system，则从 historyLen 起截取。
func SessionMessagesFromRun(all []*Message, historyLen int) []*Message {
	if len(all) == 0 {
		return nil
	}
	if historyLen < 0 {
		historyLen = 0
	}
	start := historyLen
	if all[0] != nil && all[0].Role == RoleSystem && all[0].NormalizedKind() == MessageKindSystem {
		start = 1 + historyLen
	}
	if start >= len(all) {
		return nil
	}
	out := make([]*Message, 0, len(all)-start)
	for _, m := range all[start:] {
		if m == nil {
			continue
		}
		m.EnsureKind()
		out = append(out, m)
	}
	return out
}
