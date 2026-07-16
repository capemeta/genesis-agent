package contextsnapshot

import (
	"context"
	"fmt"

	memory "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
)

// Snapshot 是 Source materialize 后的只读父线程快照。
type Snapshot struct {
	Messages   []*domain.Message
	ToolCallID string
}

// TranscriptSnapshotSource 是唯一允许向 Builder 提供父线程消息的端口。
// 具体实现负责在读取前固定一致的父线程状态；Builder 本身保持纯函数。
type TranscriptSnapshotSource interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}

type snapshotKey struct{}

// WithParentSnapshot 将当前父 Run 的不可变消息副本附加到单次 Task 调用上下文。
// 仅 ReAct 执行器应调用此函数；它不持久化，也不向普通工具公开 transcript 查询能力。
func WithParentSnapshot(ctx context.Context, messages []*domain.Message, toolCallID string) context.Context {
	return context.WithValue(ctx, snapshotKey{}, Snapshot{Messages: cloneMessages(messages), ToolCallID: toolCallID})
}

// WithoutParentSnapshot 在 Spawn 边界清除仅供父 Task 使用的快照。
// 子 Run 会继续继承取消、关联键、Hook 等正常 Context 值，但绝不继承父 transcript。
func WithoutParentSnapshot(ctx context.Context) context.Context {
	return context.WithValue(ctx, snapshotKey{}, nil)
}

// ContextSource 从 ReAct 为当前 Task 调用固定的快照读取数据。
// Phase 2 可替换为先 flush/materialize 的持久化 transcript Source，而不改变 Task/Builder。
type ContextSource struct{}

// Snapshot 返回独立副本，防止调用方修改保存在 Context 中的快照。
func (ContextSource) Snapshot(ctx context.Context) (Snapshot, error) {
	snapshot, ok := ctx.Value(snapshotKey{}).(Snapshot)
	if !ok {
		return Snapshot{}, fmt.Errorf("父 transcript 快照不可用")
	}
	snapshot.Messages = cloneMessages(snapshot.Messages)
	return snapshot, nil
}

// PersistentSource 将已持久化会话历史与当前父 Run 活跃快照合并。
// 活跃快照保存本轮尚未落盘消息，并携带当前 Task 调用标识；持久化历史则来自 ShortTermMemory。
type PersistentSource struct {
	store  memory.ShortTermMemory
	active TranscriptSnapshotSource
}

// NewPersistentSource 创建完整会话历史 Source。store 为空时保守退化为活跃快照。
func NewPersistentSource(store memory.ShortTermMemory) TranscriptSnapshotSource {
	return PersistentSource{store: store, active: ContextSource{}}
}

// Snapshot 先固定活跃快照，再读取同一 session 的已持久化历史并按时间合并。
func (s PersistentSource) Snapshot(ctx context.Context) (Snapshot, error) {
	active, err := s.active.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if s.store == nil {
		return active, nil
	}
	ref := sessionRef(ctx)
	if ref.SessionID == "" {
		return Snapshot{}, fmt.Errorf("父 transcript 快照缺少 session_id")
	}
	persisted, err := replayHistory(ctx, s.store, ref)
	if err != nil {
		return Snapshot{}, fmt.Errorf("读取持久化父 transcript 失败: %w", err)
	}
	active.Messages = mergeMessages(persisted, active.Messages)
	return active, nil
}

func sessionRef(ctx context.Context) memory.SessionRef {
	tenantID, _ := contextutil.GetTenantID(ctx)
	userID, _ := contextutil.GetUserID(ctx)
	sessionID, _ := contextutil.GetSessionID(ctx)
	fallback := memory.SessionRef{TenantID: tenantID, UserID: userID, SessionID: sessionID}
	ref, ok := memory.SessionRefFromCtx(ctx)
	if !ok {
		return fallback
	}
	if ref.TenantID == "" {
		ref.TenantID = fallback.TenantID
	}
	if ref.UserID == "" {
		ref.UserID = fallback.UserID
	}
	if ref.SessionID == "" {
		ref.SessionID = fallback.SessionID
	}
	return ref
}

func replayHistory(ctx context.Context, store memory.ShortTermMemory, ref memory.SessionRef) ([]*domain.Message, error) {
	if history, ok := store.(memory.SessionHistoryStore); ok {
		return history.Replay(ctx, ref, "")
	}
	recent, err := store.GetRecent(ctx, ref, memory.RecentOptions{})
	if err != nil {
		return nil, err
	}
	return recent.Messages, nil
}

// mergeMessages 通过持久化尾部与活跃前缀的最长重叠去除重复历史，保留活跃快照中的本轮新增消息。
func mergeMessages(persisted, active []*domain.Message) []*domain.Message {
	active = skipLeadingSystem(active)
	if len(persisted) == 0 {
		return cloneMessages(active)
	}
	if len(active) == 0 {
		return cloneMessages(persisted)
	}
	overlap := longestOverlap(persisted, active)
	merged := cloneMessages(persisted)
	merged = append(merged, cloneMessages(active[overlap:])...)
	return merged
}

func skipLeadingSystem(messages []*domain.Message) []*domain.Message {
	start := 0
	for start < len(messages) && messages[start] != nil && messages[start].Role == domain.RoleSystem {
		start++
	}
	return messages[start:]
}

func longestOverlap(left, right []*domain.Message) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for size := limit; size > 0; size-- {
		matched := true
		for i := 0; i < size; i++ {
			if !sameMessage(left[len(left)-size+i], right[i]) {
				matched = false
				break
			}
		}
		if matched {
			return size
		}
	}
	return 0
}

func sameMessage(left, right *domain.Message) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.UUID != "" && right.UUID != "" {
		return left.UUID == right.UUID
	}
	if left.Role != right.Role || left.NormalizedKind() != right.NormalizedKind() || left.Content != right.Content || left.ToolCallID != right.ToolCallID || len(left.ToolCalls) != len(right.ToolCalls) {
		return false
	}
	for i := range left.ToolCalls {
		leftCall, rightCall := left.ToolCalls[i], right.ToolCalls[i]
		if leftCall.ID != rightCall.ID || leftCall.Type != rightCall.Type || leftCall.Function.Name != rightCall.Function.Name || leftCall.Function.Arguments != rightCall.Function.Arguments {
			return false
		}
	}
	return true
}

func cloneMessages(messages []*domain.Message) []*domain.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]*domain.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		copy := *message
		if len(message.ToolCalls) > 0 {
			copy.ToolCalls = append([]domain.ToolCall(nil), message.ToolCalls...)
		}
		cloned = append(cloned, &copy)
	}
	return cloned
}
