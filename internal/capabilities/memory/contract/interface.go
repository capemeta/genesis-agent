// Package memory 定义记忆系统接口契约。
// 对应 AGENTS.md §5.4 记忆系统及《会话管理与记忆管理设计方案.md》核心契约设计。
package memory

import (
	"context"
	"errors"

	"genesis-agent/internal/domain"
)

// ErrSessionNotFound 表示指定会话不存在。
var ErrSessionNotFound = errors.New("session not found")

// SessionRef 所有请求必携带隔离维度（租户/用户/会话 + 多智能体维度）。
// 使用方式：调用方可通过 context.WithValue(ctx, SessionRefKey, ref) 在业务入口注入，
// 内核 helper SessionRefFromCtx(ctx) 从 ctx 取出，避免多层透传；接口仍显式传参以保证可测试性。
type SessionRef struct {
	TenantID        string
	UserID          string
	SessionID       string
	AppID           string // 多智能体：Agent App（可空）
	AgentInstanceID string // 多智能体：Agent 实例（可空）
}

// ctxKey 类型用于避免 context key 碰撞
type ctxKey struct{}

// SessionRefKey 是注入 context 的 key
var SessionRefKey = ctxKey{}

// SessionRefFromCtx 从 Context 提取 SessionRef
func SessionRefFromCtx(ctx context.Context) (SessionRef, bool) {
	ref, ok := ctx.Value(SessionRefKey).(SessionRef)
	return ref, ok
}

// ContextWithSessionRef 将 SessionRef 注入 Context
func ContextWithSessionRef(ctx context.Context, ref SessionRef) context.Context {
	return context.WithValue(ctx, SessionRefKey, ref)
}

// ==========================================
// 1. 短期记忆接口 (ShortTermMemory)
// ==========================================

// RecentOptions 获取最近历史选项
type RecentOptions struct {
	MaxTurns  int    // 最大用户轮数（Kind=user_turn）
	MaxTokens int    // Token 预算限制
	Model     string // 模型名称（用于 Token 估算器）
}

// RecentResult 获取最近历史返回结果
type RecentResult struct {
	Messages  []*domain.Message
	Truncated bool // 是否发生了截断
}

// SummarizeOptions 触发压缩的配置
type SummarizeOptions struct {
	KeepRecentTurns int    // 压缩时最近保留轮数
	Model           string // 压缩模型
	// Messages 是本次需要摘要的确定性消息快照。为空时由存储后端读取会话历史。
	// 运行时传入该字段，确保触发判断、摘要输入和内存替换处于同一消息边界。
	Messages []*domain.Message
}

// SummaryResult 压缩摘要返回结果
type SummaryResult struct {
	Summary     *domain.SessionSummary
	TokensSaved int
}

// ShortTermMemory 短期记忆：管理 Session 级完整对话历史与滚动摘要。
type ShortTermMemory interface {
	Append(ctx context.Context, ref SessionRef, msgs []*domain.Message) error
	// GetRecent 从新到旧返回消息，受条数与 token 预算双约束；返回是否发生截断。
	GetRecent(ctx context.Context, ref SessionRef, opt RecentOptions) (RecentResult, error)
	// Summarize 将 keepRecent 之外的旧历史压缩为摘要并落盘（返回新的 summary 与 leaf 指针）。
	Summarize(ctx context.Context, ref SessionRef, opt SummarizeOptions) (SummaryResult, error)
	GetSummary(ctx context.Context, ref SessionRef) (*domain.SessionSummary, error)
	Clear(ctx context.Context, ref SessionRef) error
}

// SessionHistoryStore 是支持会话审计回放与分支的可选扩展。
// Fork 的 leafID 为空时复制完整可恢复历史；非空时复制至该消息（含该消息）为止。
type SessionHistoryStore interface {
	Replay(ctx context.Context, ref SessionRef, leafID string) ([]*domain.Message, error)
	Fork(ctx context.Context, source, target SessionRef, leafID string) error
}

// ==========================================
// 2. 长期记忆接口 (LongTermMemory)
// ==========================================

// LongTermMemory 长期记忆服务接口，用于保存、检索与合并长期记忆。
type LongTermMemory interface {
	// Save 保存或更新长期记忆条目，写入时须校验 CustomData 是否符合 Schema。
	Save(ctx context.Context, ref SessionRef, entries []*domain.LongTermEntry) error
	// Search 检索相关的长期记忆，支持向量检索与传统基于标签/文本的检索退化链。
	Search(ctx context.Context, ref SessionRef, query domain.MemoryQuery) ([]*domain.LongTermEntry, error)
	// Delete 删除指定记忆条目。
	Delete(ctx context.Context, ref SessionRef, ids []string) error
}

// ==========================================
// 3. 用户画像接口 (UserProfileStore)
// ==========================================

// UserProfileStore 用户画像存储接口，处理强类型内置字段与自定义画像字段。
type UserProfileStore interface {
	// Get 获取用户的画像数据。
	Get(ctx context.Context, tenantID, userID string) (*domain.UserProfile, error)
	// Save 保存用户的画像数据，Builtin 与 CustomFields 独立持久化。
	Save(ctx context.Context, tenantID, userID string, profile *domain.UserProfile) error
}

// ==========================================
// 4. 长期记忆抽取接口 (MemoryExtractor)
// ==========================================

// ExtractInput 长期记忆抽取的输入参数
type ExtractInput struct {
	SessionRef SessionRef
	Messages   []*domain.Message // 待抽取的原始消息列表（应该已过滤掉非用户轮或临时注入）
	Strategy   string            // 抽取策略/场景配置名称
}

// MemoryExtractor 业务可自定义的长期记忆/画像提取器。
type MemoryExtractor interface {
	// Extract 从给定的一段对话消息中抽取候选长期记忆条目与画像更新项。
	Extract(ctx context.Context, in ExtractInput) ([]*domain.LongTermEntry, error)
}

// ==========================================
// 5. 会话管理仓储接口 (SessionStore)
// ==========================================

// SessionStore 会话管理仓库接口，负责 Session 本身的 CRUD 及原子状态机转换。
type SessionStore interface {
	// CreateSession 创建新的会话。
	CreateSession(ctx context.Context, session *domain.Session) error
	// GetSession 获取会话信息。
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	// ListSessions 按会话归属范围查询，结果按最近更新时间倒序返回。
	ListSessions(ctx context.Context, query SessionQuery) ([]*domain.Session, error)
	// FindLatestSession 返回范围内最近更新的一个会话。
	FindLatestSession(ctx context.Context, query SessionQuery) (*domain.Session, error)
	// UpdateSession 更新会话（修改标题、更新累计 Tokens、更新 summary_leaf_id 等）。
	UpdateSession(ctx context.Context, session *domain.Session) error
	// UpdateStatus 带有 CAS（Compare-And-Swap）的状态更新，确保状态机转换并发安全。
	UpdateStatus(ctx context.Context, sessionID string, expected, target domain.SessionState) (bool, error)
	// DeleteSession 软删除会话。
	DeleteSession(ctx context.Context, sessionID string) error
}

// SessionQuery 描述会话查询范围。非空字段必须精确匹配，避免跨租户、跨用户恢复会话。
type SessionQuery struct {
	TenantID        string
	UserID          string
	AgentID         string
	AppID           string
	Limit           int
	IncludeArchived bool
	IncludeDeleted  bool
}
