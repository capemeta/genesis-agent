package contract

import (
	"context"
	"time"
)

// Lease 描述分布式后台任务管理需要的最小租约信息。
// 当前 Phase 不提供 DB/Redis 实现；CLI 仍使用进程内控制面。
type Lease struct {
	AgentID   string
	OwnerID   string
	ExpiresAt time.Time
}

// LeaseStore 是 Enterprise/后台管理器后续接入的租约端口。
// 语义必须保持 acquire/renew/release 幂等，避免跨进程重复 Stop 或重复占槽。
type LeaseStore interface {
	Acquire(ctx context.Context, lease Lease) (bool, error)
	Renew(ctx context.Context, lease Lease) (bool, error)
	Release(ctx context.Context, agentID, ownerID string) error
}

// CancellationStore 是跨进程 TaskStop 的最小控制端口。
// RequestStop 只写意图；持有租约的 worker 通过 PollStop 消费并执行真实 cancel。
type CancellationStore interface {
	RequestStop(ctx context.Context, agentID, requesterRunID string) error
	PollStop(ctx context.Context, agentID, ownerID string) (bool, error)
	ClearStop(ctx context.Context, agentID, ownerID string) error
}

// HeartbeatStore 让管理面区分活跃任务、失联任务和可恢复终态记录。
type HeartbeatStore interface {
	Heartbeat(ctx context.Context, agentID, ownerID string, at time.Time) error
	LastHeartbeat(ctx context.Context, agentID string) (time.Time, error)
}

// BackgroundRunner 管理已启动的后台子智能体实例生命周期。
// Task 只负责 spawn；租约、心跳和跨进程取消由注入的 runner 消费。
type BackgroundRunner interface {
	Run(ctx context.Context, agentID string) error
}
