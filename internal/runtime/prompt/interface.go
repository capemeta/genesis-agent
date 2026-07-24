// Package prompt 定义运行时提示词构建接口与默认实现。
package prompt

import (
	"context"

	"genesis-agent/internal/domain"
)

// Audience 区分主 Agent 与子智能体的提示词装配裁剪。
type Audience string

const (
	// AudienceRoot 是根会话主 Agent（默认）。
	AudienceRoot Audience = "root"
	// AudienceSubAgent 是普通 Task 委派子 Run：跳过主侧委派纪律与冗长腔调，但可保留
	// memory/RAG 类 Injector；仍持有 Skill 工具（可在任务中调度技能）。
	AudienceSubAgent Audience = "subagent"
	// AudienceSkillFork 是由 Skill 网关派生的执行子 Run：跳过所有全局 Injectors；
	// 不持有 Skill 工具；System 由 ComposeChildSystem 完全接管。
	AudienceSkillFork Audience = "skill_fork"
)

// BuildRequest 描述一次系统提示词构建请求。
type BuildRequest struct {
	Agent          *domain.Agent
	Run            *domain.Run
	UserID         string
	TurnID         string
	Context        map[string]string
	AvailableTools []string
	// VisionMode 是本 Run 的 EffectiveVisionMode（direct_inject / expert_route / degraded_text）。
	// 空值时不注入形态特定文案；有 view_image 工具时仍注入通用看图纪律。
	VisionMode string
	// Audience 为空时按 AudienceRoot 处理。
	Audience Audience
	// DelegationPosture 控制 Task 可用时的委派纪律文案（proactive / explicit_request_only）。
	// 空值由 Builder 默认姿态回落。
	DelegationPosture string
	// CollaborationMode 协作模式：plan_mode 时注入 plan_mode_rules 并跳过 task_management。
	CollaborationMode string
	// PlanDocumentPath 实施方案工作区相对路径（规划模式提示词锚点）；空则用占位模式串。
	PlanDocumentPath string
}

// Fragment 是动态上下文片段。
type Fragment struct {
	Name     string
	Contents string
}

// ContextInjector 在运行时注入动态上下文，例如 Skills、记忆摘要等。
type ContextInjector interface {
	Inject(ctx context.Context, req BuildRequest) (Fragment, error)
}

// AudienceAware 是可选接口：Injector 通过实现此接口声明自己适用于哪些受众。
// 未实现此接口视为适用于所有受众（向后兼容）。
// 返回 nil 或空切片同样视为适用于所有受众。
type AudienceAware interface {
	Audiences() []Audience
}

// ContextInjectorFunc 让普通函数可作为注入器（适用所有受众）。
type ContextInjectorFunc func(ctx context.Context, req BuildRequest) (Fragment, error)

func (f ContextInjectorFunc) Inject(ctx context.Context, req BuildRequest) (Fragment, error) {
	return f(ctx, req)
}

// audienceFilteredInjector 包装任意 ContextInjector，限定其适用受众。
type audienceFilteredInjector struct {
	inner     ContextInjector
	audiences []Audience
}

func (a *audienceFilteredInjector) Inject(ctx context.Context, req BuildRequest) (Fragment, error) {
	return a.inner.Inject(ctx, req)
}

func (a *audienceFilteredInjector) Audiences() []Audience {
	return a.audiences
}

// WithAudiences 为任意 ContextInjector 声明适用受众，返回实现了 AudienceAware 的注入器。
// 用于 skillstack 等需要限定受众的匿名函数注入器。
func WithAudiences(injector ContextInjector, audiences ...Audience) ContextInjector {
	return &audienceFilteredInjector{inner: injector, audiences: audiences}
}

// Builder 构建运行时提示词。
type Builder interface {
	BuildSystem(ctx context.Context, req BuildRequest) (string, error)
}

