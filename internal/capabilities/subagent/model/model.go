// Package model 定义可被 Task 调用的子智能体定义。
package model

// Definition 是内置或外部 Source 归一后的子智能体定义。
type Definition struct {
	Name          string
	Description   string
	WhenToUse     string
	SystemPrompt  string
	ReadOnly      bool
	Tools         []string
	MaxTurns      int
	MaxDepth      int
	MaxTokens     int64
	MaxToolCalls  int
	ForkContext   *bool
	ExecutionMode ExecutionMode
	TimeoutSec    int
}

// ExecutionMode 是 Definition 对 Task 默认等待方式的约束。
type ExecutionMode string

const (
	ExecutionModeSync  ExecutionMode = "sync"
	ExecutionModeAsync ExecutionMode = "async"
)

// Summary 是供动态 Task 描述渲染的安全投影。
type Summary struct {
	Name        string
	Description string
	WhenToUse   string
}
