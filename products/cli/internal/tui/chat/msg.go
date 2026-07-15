// Package chat 实现基于 Bubble Tea 的 TUI 对话界面
// 遵循 Elm 架构：Model 持有状态，Update 处理消息，View 渲染视图
package chat

import (
	"time"

	"genesis-agent/internal/app"
	"genesis-agent/internal/runtime/progress"
)

// runCompleteMsg Agent 推理成功完成时，后台 goroutine 发回 Update 的消息
type runCompleteMsg struct {
	result *app.RunResult // 完整的执行结果（含 Run 和耗时）
}

// runErrorMsg Agent 推理出错时发回 Update 的消息
type runErrorMsg struct {
	err error // 错误详情
}

// progressMsg Agent 运行进度事件。
type progressMsg struct {
	event progress.Event
}

// flushProgressMsg 请求将已合并的进度状态刷新到 viewport。
type flushProgressMsg struct{}

// clearToastMsg 定时清除 toast 消息（由 clearToastAfter Cmd 发出）
type clearToastMsg struct{}

// uiMessage TUI 渲染用的对话消息，是 domain.Message 的视图层映射
type uiMessage struct {
	role            string        // 发送方："user" | "assistant" | "system"
	content         string        // 消息正文
	isProgress      bool          // 是否是运行过程日志（如果是，则动态根据展开/折叠渲染）
	progressLog     []string      // 过程日志的条目列表
	activityTokens  int64         // 运行过程摘要的 token 消耗
	activityElapsed time.Duration // 运行过程摘要的耗时
	activityOutcome string        // 运行过程结果：进行中 / 完成 / 失败
	steps           int           // 推理步骤数（仅 assistant 有效，0 表示未知）
	tokens          int64         // Token 消耗（仅 assistant 有效）
	elapsed         time.Duration // 推理耗时（仅 assistant 有效）
}
