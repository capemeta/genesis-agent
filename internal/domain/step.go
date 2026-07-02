package domain

import (
	"encoding/json"
	"time"
)

// ActionType 步骤的动作类型，决定执行路径
type ActionType string

const (
	ActionTypeThink       ActionType = "think"        // LLM推理
	ActionTypeToolCall    ActionType = "tool_call"    // 工具调用
	ActionTypeFinalAnswer ActionType = "final_answer" // 最终回答，结束Loop
	ActionTypeRAGSearch   ActionType = "rag_search"   // RAG检索（预留）
)

// StepStatus 单个Step的执行状态
type StepStatus string

const (
	StepStatusRunning   StepStatus = "running"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
)

// Step 最小执行单元，对应Loop中一次LLM推理或工具调用
// 是Trace的基本单位，每个Step对应一个Span
type Step struct {
	ID            string
	RunID         string
	StepIndex     int // 从0开始的迭代轮次
	ActionType    ActionType
	ActionPayload json.RawMessage // 动作请求内容（LLM输出）
	Observation   json.RawMessage // 执行结果（工具返回值/最终回答）
	TokenUsage    TokenUsage
	Status        StepStatus
	StartedAt     time.Time
	FinishedAt    *time.Time
}
