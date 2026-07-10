// Package domain - 对话消息类型定义
// Message 是整个 Agent 系统中信息流转的核心单元，不依赖任何外部框架
// 语义上与 OpenAI、Anthropic、eino 等主流框架的消息结构保持一致
package domain

// RoleType 消息发送方的角色类型
type RoleType string

const (
	RoleSystem    RoleType = "system"    // 系统提示词
	RoleUser      RoleType = "user"      // 用户输入
	RoleAssistant RoleType = "assistant" // LLM回复（可能含工具调用）
	RoleTool      RoleType = "tool"      // 工具执行结果
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
	// Role 消息发送方角色
	Role RoleType
	// Content 文本内容（assistant发起工具调用时可能为空）
	Content string
	// ToolCalls LLM请求调用的工具列表（仅 assistant 角色时有值）
	ToolCalls []ToolCall
	// ToolCallID 本消息对应的工具调用ID（仅 tool 角色时有值）
	ToolCallID string
	// ReasoningContent 推理/思考内容（仅 assistant 角色在支持CoT时有值）
	ReasoningContent string
}

// NewSystemMessage 创建 system 角色消息
func NewSystemMessage(content string) *Message {
	return &Message{Role: RoleSystem, Content: content}
}

// NewUserMessage 创建 user 角色消息
func NewUserMessage(content string) *Message {
	return &Message{Role: RoleUser, Content: content}
}

// NewToolResultMessage 创建工具执行结果消息（role=tool）
func NewToolResultMessage(toolCallID, result string) *Message {
	return &Message{
		Role:       RoleTool,
		Content:    result,
		ToolCallID: toolCallID,
	}
}
