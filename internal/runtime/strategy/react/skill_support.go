package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
)

// SkillMention 是用户输入中解析出的 skill 引用。
type SkillMention struct {
	Skill    string
	Resource string
}

// SkillMentionSelector 在 turn 开始时解析 $skill / skill:// mention。
type SkillMentionSelector interface {
	SelectMentions(ctx context.Context, text string) ([]SkillMention, error)
}

// SkillExplicitLoader 加载用户显式选择的 Skill。
type SkillExplicitLoader interface {
	LoadExplicitSkill(ctx context.Context, req skillcontract.ExplicitLoadRequest) (string, error)
}

// WithSkillMentionSelector 注入 mention 自动选择。
func WithSkillMentionSelector(selector SkillMentionSelector) EngineOption {
	return func(e *ReactLoopEngine) {
		e.skillMentionSelector = selector
	}
}

// WithSkillExplicitLoader 注入用户显式 Skill 加载器。
func WithSkillExplicitLoader(loader SkillExplicitLoader) EngineOption {
	return func(e *ReactLoopEngine) {
		e.skillExplicitLoader = loader
	}
}

// WithAutoRewriteSkillCollision 控制误把 skill 名当 tool 时是否同轮改写执行；默认 true。
func WithAutoRewriteSkillCollision(enabled bool) EngineOption {
	return func(e *ReactLoopEngine) {
		e.autoRewriteSkillCollision = &enabled
	}
}

func (e *ReactLoopEngine) shouldAutoRewriteSkillCollision() bool {
	if e.autoRewriteSkillCollision == nil {
		return true
	}
	return *e.autoRewriteSkillCollision
}

func skillInjectionKey(injection skillInjectionOutput) string {
	if strings.TrimSpace(injection.Resource) != "" {
		return strings.TrimSpace(injection.Resource)
	}
	return strings.TrimSpace(injection.QualifiedName)
}

func renderAlreadyLoadedAck(injection skillInjectionOutput) string {
	payload := map[string]any{
		"type":           "already_loaded",
		"qualified_name": injection.QualifiedName,
		"resource":       injection.Resource,
		"message":        "Skill already injected in this run; skipped duplicate body.",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"type":"already_loaded"}`
	}
	return string(data)
}

// applySkillToolResult 处理 Skill 网关结果：去重、短确认、单份 injection、工具收窄。
func (e *ReactLoopEngine) applySkillToolResult(rc *runtime.RunContext, toolResult toolExecutionResult, activeToolNames *[]string, toolInfos *[]*tool.Info, iterLog logger.Logger) bool {
	injection, ok := parseSkillInjection(toolResult)
	if !ok {
		return false
	}
	key := skillInjectionKey(injection)
	if rc.HasInjectedSkill(key) {
		rc.Messages = append(rc.Messages, domain.NewToolResultMessage(toolResult.ID, renderAlreadyLoadedAck(injection)))
		return true
	}
	narrowed, narrowOK := narrowToolNames(*activeToolNames, injection.AllowedTools)
	rc.Messages = append(rc.Messages, domain.NewToolResultMessage(toolResult.ID, renderSkillToolAck(injection, narrowOK)))
	// 对齐 Kode newMessages / Codex <skill>：SKILL 全文以 user + Kind=skill_injection 注入。
	rc.Messages = append(rc.Messages, domain.NewSkillInjectionMessage(renderSkillInjection(injection)).WithSource(domain.MessageSourceSkillGateway))
	rc.MarkInjectedSkill(key)
	registerSkillInjectionFollow(rc, injection.Content)
	if narrowOK {
		*activeToolNames = narrowed
		*toolInfos = e.filterToolInfos(context.Background(), *activeToolNames)
	} else {
		iterLog.Warn("Skill allowed_tools 与当前可见工具求交为空，保持原工具集", "skill", injection.QualifiedName, "allowed_tools", injection.AllowedTools)
	}
	return true
}

// injectMentionedSkills 在首轮 LLM 前按 mention 自动加载 skill。
// 只追加 user 侧 <skill_injection>（对齐 Kode/Codex），不伪造 tool_call/tool_result。
func (e *ReactLoopEngine) injectMentionedSkills(ctx context.Context, rc *runtime.RunContext, userInput string, activeToolNames *[]string, toolInfos *[]*tool.Info, log logger.Logger) {
	if e.skillMentionSelector == nil || strings.TrimSpace(userInput) == "" {
		return
	}
	mentions, err := e.skillMentionSelector.SelectMentions(ctx, userInput)
	if err != nil {
		log.Warn("解析 skill mention 失败，跳过自动注入", "error", err)
		return
	}
	if e.skillExplicitLoader == nil {
		log.Warn("未配置显式 Skill 加载器，跳过 mention 自动注入")
		return
	}
	for _, mention := range mentions {
		result, execErr := e.skillExplicitLoader.LoadExplicitSkill(ctx, skillcontract.ExplicitLoadRequest{
			Skill:    mention.Skill,
			Resource: mention.Resource,
		})
		if execErr != nil {
			log.Warn("mention 自动加载 Skill 失败", "skill", mention.Skill, "error", execErr)
			rc.Messages = append(rc.Messages, domain.NewSystemMessage(fmt.Sprintf(
				"<skill_mention_error>\n自动加载 mention skill 失败: %s\nerror: %s\n</skill_mention_error>",
				firstNonEmpty(mention.Skill, mention.Resource), execErr.Error(),
			)).WithSource(domain.MessageSourceSkillMention))
			continue
		}
		tr := toolExecutionResult{ID: "mention", Name: "Skill", Content: result}
		injection, ok := parseSkillInjection(tr)
		if !ok {
			log.Warn("mention Skill 返回无法解析", "skill", mention.Skill)
			continue
		}
		key := skillInjectionKey(injection)
		if rc.HasInjectedSkill(key) {
			continue
		}
		narrowed, narrowOK := narrowToolNames(*activeToolNames, injection.AllowedTools)
		rc.Messages = append(rc.Messages, domain.NewSkillInjectionMessage(renderSkillInjection(injection)).WithSource(domain.MessageSourceSkillMention))
		rc.MarkInjectedSkill(key)
		registerSkillInjectionFollow(rc, injection.Content)
		if narrowOK {
			*activeToolNames = narrowed
			*toolInfos = e.filterToolInfos(ctx, *activeToolNames)
		} else {
			log.Warn("mention Skill allowed_tools 求交为空，保持原工具集", "skill", injection.QualifiedName)
		}
		log.Info("已按 mention 自动注入 Skill", "skill", injection.QualifiedName)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
