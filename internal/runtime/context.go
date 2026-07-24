package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	skillmodel "genesis-agent/internal/capabilities/skill/model"
	subagentprompt "genesis-agent/internal/capabilities/subagent/prompt"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/repeatguard"
)

// RunContext Loop执行期间的内存状态
// 在一次Run的整个生命周期内存在，Run结束后释放
// 所有字段均使用 domain 类型，不依赖外部框架
type RunContext struct {
	// --- 基础标识 ---
	Run   *domain.Run   // 当前Run
	Agent *domain.Agent // Agent配置（只读）

	// --- Loop状态 ---
	Iteration int               // 当前迭代轮次（从0开始）
	Messages  []*domain.Message // 当前对话消息列表（含历史+本轮交互）

	// --- 统计 ---
	TokenUsed int64 // 本次Run累计Token消耗
	ToolCalls int   // 本次Run累计工具调用数

	// --- Stream / Block状态 ---
	blockSeq int64 // 递增分配的BlockIndex，用于流式多端输出，线程安全

	// --- Skill 注入去重（本 Run 内）---
	injectedMu     sync.Mutex
	injectedSkills map[string]struct{} // key = opaque resource 或 authority:package
	activeSkill    *skillmodel.InvocationBinding

	// --- Skill 必做步骤软门禁跟踪（本 Run 内）---
	SkillFollow *SkillFollowState

	// --- 视觉能力形态（Run 级工具门控；Sanitizer 仍按请求级 TargetModel）---
	VisionMode string

	// --- Repeat Guard（本 Run 内；Resume 时须随可恢复状态一并恢复）---
	RepeatGuard *repeatguard.Guard
}

// ActivateInvocation 原子激活当前 Run 唯一的 Skill Invocation。
// 第一版明确采用单 active invocation：一旦激活，权限只能保持或收紧，禁止在同一
// Run 内隐式切换到另一个 binding，避免工具调用、包快照和交付所有者产生歧义。
func (rc *RunContext) ActivateInvocation(binding skillmodel.InvocationBinding) error {
	if rc == nil {
		return fmt.Errorf("SKILL_INVOCATION_ACTIVATION_FAILED: run context为空")
	}
	if err := skillmodel.ValidateBindingIdentity(binding); err != nil {
		return fmt.Errorf("SKILL_INVOCATION_ACTIVATION_FAILED: %w", err)
	}
	rc.injectedMu.Lock()
	defer rc.injectedMu.Unlock()
	if rc.activeSkill != nil {
		if rc.activeSkill.ID == binding.ID {
			return nil
		}
		return fmt.Errorf("SKILL_INVOCATION_ALREADY_ACTIVE: 当前Run已激活%q，禁止隐式切换到%q", rc.activeSkill.Handle, binding.Handle)
	}
	cloned := binding.Clone()
	rc.activeSkill = &cloned
	return nil
}

// ActiveInvocation 返回当前 Run 的不可变 InvocationBinding 副本。
func (rc *RunContext) ActiveInvocation() (skillmodel.InvocationBinding, bool) {
	if rc == nil {
		return skillmodel.InvocationBinding{}, false
	}
	rc.injectedMu.Lock()
	defer rc.injectedMu.Unlock()
	if rc.activeSkill == nil {
		return skillmodel.InvocationBinding{}, false
	}
	return rc.activeSkill.Clone(), true
}

// InvocationAllowsTool 是执行期硬门禁；工具可见性过滤与策略门控仅作用于 skill-fork 子智能体执行上下文。
// 主 Agent (AudienceRoot / main 模式) 不受此限，始终保留完整的工具链能力。
func (rc *RunContext) InvocationAllowsTool(ctx context.Context, name string) bool {
	binding, ok := rc.ActiveInvocation()
	if !ok {
		return true
	}
	// tool_policy.allow 仅对 skill-fork 派生的子 Run 生效；主 Agent 不被锁定。
	subagentType := ""
	if ctx != nil {
		subagentType = contextutil.GetSubagentType(ctx)
	}
	if !strings.HasPrefix(subagentType, subagentprompt.SkillForkSubagentTypePrefix) {
		return true
	}
	name = strings.TrimSpace(name)
	for _, allowed := range binding.ToolPolicy.Allowed {
		if strings.TrimSpace(allowed) == name {
			return true
		}
	}
	return false
}




// NewRunContext 初始化RunContext
func NewRunContext(run *domain.Run, agent *domain.Agent) *RunContext {
	rc := &RunContext{
		Run:            run,
		Agent:          agent,
		Messages:       make([]*domain.Message, 0, 16),
		blockSeq:       -1, // 第一个分配的将是 0
		injectedSkills: make(map[string]struct{}),
		SkillFollow:    NewSkillFollowState(),
	}
	policy := domain.RuntimePolicy{}
	if agent != nil {
		policy = agent.RuntimePolicy
	}
	rc.RepeatGuard = repeatguard.New(repeatguard.ConfigFromPolicy(policy))
	return rc
}

// NextBlockIndex 分配并返回下一个 BlockIndex（从0开始）
func (rc *RunContext) NextBlockIndex() int {
	return int(atomic.AddInt64(&rc.blockSeq, 1))
}

// HasInjectedSkill 判断本 Run 是否已注入过该 skill resource。
func (rc *RunContext) HasInjectedSkill(key string) bool {
	if rc == nil || key == "" {
		return false
	}
	rc.injectedMu.Lock()
	defer rc.injectedMu.Unlock()
	_, ok := rc.injectedSkills[key]
	return ok
}

// MarkInjectedSkill 记录已注入的 skill；若已存在返回 false。
func (rc *RunContext) MarkInjectedSkill(key string) bool {
	if rc == nil || key == "" {
		return false
	}
	rc.injectedMu.Lock()
	defer rc.injectedMu.Unlock()
	if _, ok := rc.injectedSkills[key]; ok {
		return false
	}
	rc.injectedSkills[key] = struct{}{}
	return true
}

// AddStep 向Run中追加一个Step记录
func (rc *RunContext) AddStep(step *domain.Step) {
	rc.Run.Steps = append(rc.Run.Steps, step)
}

// AddTokens 累加Token用量
func (rc *RunContext) AddTokens(usage domain.TokenUsage) {
	rc.TokenUsed += usage.TotalTokens
	rc.Run.TotalTokens += usage.TotalTokens
}
