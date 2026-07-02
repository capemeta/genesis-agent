// Package chat 实现基于 Bubble Tea 的 TUI 对话界面
// 遵循 Elm 架构：Model 持有状态，Update 处理消息，View 渲染视图
package chat

import (
	"time"

	"genesis-agent/internal/app"
)

// runCompleteMsg Agent 推理成功完成时，后台 goroutine 发回 Update 的消息
type runCompleteMsg struct {
	result *app.RunResult // 完整的执行结果（含 Run 和耗时）
}

// runErrorMsg Agent 推理出错时发回 Update 的消息
type runErrorMsg struct {
	err error // 错误详情
}

// uiMessage TUI 渲染用的对话消息，是 domain.Message 的视图层映射
type uiMessage struct {
	role    string        // 发送方："user" | "assistant" | "system"
	content string        // 消息正文
	steps   int           // 推理步骤数（仅 assistant 有效，0 表示未知）
	tokens  int64         // Token 消耗（仅 assistant 有效）
	elapsed time.Duration // 推理耗时（仅 assistant 有效）
}
