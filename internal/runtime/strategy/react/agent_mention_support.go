package react

import (
	"context"
	"strings"

	subagentprompt "genesis-agent/internal/capabilities/subagent/prompt"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/prompt"
)

// SubAgentTypeLookup 供 @agent mention 预校验；与 Task Catalog 同源注入。
type SubAgentTypeLookup interface {
	Has(name string) bool
}

// WithSubAgentTypeLookup 注入子智能体类型查询（mention 预校验）。
func WithSubAgentTypeLookup(lookup SubAgentTypeLookup) EngineOption {
	return func(e *ReactLoopEngine) {
		e.subAgentTypeLookup = lookup
	}
}

// promptAudience 按委派深度和子智能体类型选择 BuildSystem 受众。
// - depth=0：主 Agent（AudienceRoot）
// - depth>0 且 SubagentType 以 "skill-fork:" 开头：Skill 执行子 Run（AudienceSkillFork）
// - depth>0 其他：普通 Task 子 Agent（AudienceSubAgent）
func promptAudience(ctx context.Context) prompt.Audience {
	if multicontract.DelegationDepth(ctx) <= 0 {
		return prompt.AudienceRoot
	}
	if strings.HasPrefix(contextutil.GetSubagentType(ctx), subagentprompt.SkillForkSubagentTypePrefix) {
		return prompt.AudienceSkillFork
	}
	return prompt.AudienceSubAgent
}

// injectAgentMentions 在根 Run 首轮 LLM 前注入 @agent / @run-agent 提醒（L4）。
// Catalog 命中 → 强制 Task；未命中 → 禁止盲目 Task；lookup 未注入时保持强制提醒（兼容测试）。
// 不直接 Spawn；子 Run 不注入。
func (e *ReactLoopEngine) injectAgentMentions(ctx context.Context, rc *runtime.RunContext, userInput string) {
	if rc == nil || strings.TrimSpace(userInput) == "" {
		return
	}
	if multicontract.DelegationDepth(ctx) > 0 {
		return
	}
	for _, agentType := range subagentprompt.ParseAgentMentions(userInput) {
		mention := "run-agent-" + agentType
		var reminder string
		if e.subAgentTypeLookup != nil && !e.subAgentTypeLookup.Has(agentType) {
			reminder = subagentprompt.UnknownAgentMentionReminder(agentType, mention)
		} else {
			reminder = subagentprompt.AgentMentionReminder(agentType, mention)
		}
		if reminder == "" {
			continue
		}
		rc.Messages = append(rc.Messages, domain.NewReminderMessage(reminder))
	}
}
