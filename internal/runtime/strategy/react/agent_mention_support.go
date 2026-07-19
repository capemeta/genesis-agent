package react

import (
	"context"
	"strings"

	subagentprompt "genesis-agent/internal/capabilities/subagent/prompt"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/prompt"
)

// promptAudience 按委派深度选择 BuildSystem 受众。
func promptAudience(ctx context.Context) prompt.Audience {
	if multicontract.DelegationDepth(ctx) > 0 {
		return prompt.AudienceSubAgent
	}
	return prompt.AudienceRoot
}

// injectAgentMentions 在根 Run 首轮 LLM 前注入 @agent / @run-agent 强制 Task 提醒（L4）。
// 不直接 Spawn：仍由模型经 Task 网关委派，与 Skill mention 同构。
// 子 Run 不注入，避免把委派信封中的 mention 文本误当成用户点名。
func (e *ReactLoopEngine) injectAgentMentions(ctx context.Context, rc *runtime.RunContext, userInput string) {
	if rc == nil || strings.TrimSpace(userInput) == "" {
		return
	}
	if multicontract.DelegationDepth(ctx) > 0 {
		return
	}
	for _, agentType := range subagentprompt.ParseAgentMentions(userInput) {
		reminder := subagentprompt.AgentMentionReminder(agentType, "run-agent-"+agentType)
		if reminder == "" {
			continue
		}
		rc.Messages = append(rc.Messages, domain.NewReminderMessage(reminder))
	}
}
