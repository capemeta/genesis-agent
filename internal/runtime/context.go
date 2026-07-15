package runtime

import (
	"sync"
	"sync/atomic"

	"genesis-agent/internal/domain"
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

	// --- Skill 必做步骤软门禁跟踪（本 Run 内）---
	SkillFollow *SkillFollowState

	// --- Repeat Guard（本 Run 内；Resume 时须随可恢复状态一并恢复）---
	RepeatGuard *repeatguard.Guard
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
