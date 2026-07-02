// package runtime - RunContext定义
// RunContext 是Loop执行期间的内存上下文对象，不持久化
// 对应 AGENTS.md §3.8 RunContext
package runtime

import (
	"genesis-agent/internal/domain"
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
}

// NewRunContext 初始化RunContext
func NewRunContext(run *domain.Run, agent *domain.Agent) *RunContext {
	return &RunContext{
		Run:      run,
		Agent:    agent,
		Messages: make([]*domain.Message, 0, 16),
	}
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
