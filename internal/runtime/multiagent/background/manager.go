// Package background 提供子智能体后台任务的产品无关管理骨架。
package background

import (
	"context"
	"fmt"
	"strings"
	"time"

	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
	defaultLeaseTTL          = 30 * time.Second
)

type Deps struct {
	Controller contract.Controller
	Leases     contract.LeaseStore
	Heartbeats contract.HeartbeatStore
	Cancels    contract.CancellationStore
	OwnerID    string
	Interval   time.Duration
	LeaseTTL   time.Duration
	Now        func() time.Time
}

// Manager 负责持有后台任务租约、定期心跳，并消费跨进程取消意图。
type Manager struct {
	controller contract.Controller
	leases     contract.LeaseStore
	heartbeats contract.HeartbeatStore
	cancels    contract.CancellationStore
	ownerID    string
	interval   time.Duration
	leaseTTL   time.Duration
	now        func() time.Time
}

func New(deps Deps) (*Manager, error) {
	if deps.Controller == nil {
		return nil, fmt.Errorf("background Manager 缺少 Controller")
	}
	if deps.Leases == nil {
		return nil, fmt.Errorf("background Manager 缺少 LeaseStore")
	}
	if deps.Heartbeats == nil {
		return nil, fmt.Errorf("background Manager 缺少 HeartbeatStore")
	}
	if deps.Cancels == nil {
		return nil, fmt.Errorf("background Manager 缺少 CancellationStore")
	}
	ownerID := strings.TrimSpace(deps.OwnerID)
	if ownerID == "" {
		return nil, fmt.Errorf("background Manager owner_id 不能为空")
	}
	interval := deps.Interval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	ttl := deps.LeaseTTL
	if ttl <= 0 {
		ttl = defaultLeaseTTL
	}
	if ttl <= interval {
		return nil, fmt.Errorf("background Manager lease ttl 必须大于 heartbeat interval")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{controller: deps.Controller, leases: deps.Leases, heartbeats: deps.Heartbeats, cancels: deps.Cancels, ownerID: ownerID, interval: interval, leaseTTL: ttl, now: now}, nil
}

func (m *Manager) Run(ctx context.Context, agentID string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return fmt.Errorf("agent_id 不能为空")
	}
	if err := m.acquire(ctx, agentID); err != nil {
		return err
	}
	defer func() { _ = m.leases.Release(context.Background(), agentID, m.ownerID) }()

	if done, err := m.tick(ctx, agentID); err != nil || done {
		return err
	}
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			done, err := m.tick(ctx, agentID)
			if err != nil || done {
				return err
			}
		}
	}
}

func (m *Manager) acquire(ctx context.Context, agentID string) error {
	ok, err := m.leases.Acquire(ctx, m.lease(agentID))
	if err != nil {
		return fmt.Errorf("获取子智能体后台租约失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("子智能体 %s 已被其他 worker 管理", agentID)
	}
	return nil
}

func (m *Manager) tick(ctx context.Context, agentID string) (bool, error) {
	now := m.now()
	ok, err := m.leases.Renew(ctx, m.leaseAt(agentID, now))
	if err != nil {
		return false, fmt.Errorf("续租子智能体后台租约失败: %w", err)
	}
	if !ok {
		return false, fmt.Errorf("子智能体 %s 后台租约已失效", agentID)
	}
	if err := m.heartbeats.Heartbeat(ctx, agentID, m.ownerID, now); err != nil {
		return false, fmt.Errorf("写入子智能体心跳失败: %w", err)
	}
	stop, err := m.cancels.PollStop(ctx, agentID, m.ownerID)
	if err != nil {
		return false, fmt.Errorf("读取子智能体取消意图失败: %w", err)
	}
	if stop {
		if err := m.controller.Stop(ctx, agentID); err != nil {
			return false, fmt.Errorf("执行子智能体取消失败: %w", err)
		}
		if err := m.cancels.ClearStop(ctx, agentID, m.ownerID); err != nil {
			return false, fmt.Errorf("清理子智能体取消意图失败: %w", err)
		}
	}
	instance, err := m.controller.Get(ctx, agentID)
	if err != nil {
		return false, fmt.Errorf("读取子智能体状态失败: %w", err)
	}
	return isTerminal(instance.Status), nil
}

func (m *Manager) lease(agentID string) contract.Lease {
	return m.leaseAt(agentID, m.now())
}

func (m *Manager) leaseAt(agentID string, at time.Time) contract.Lease {
	return contract.Lease{AgentID: agentID, OwnerID: m.ownerID, ExpiresAt: at.Add(m.leaseTTL)}
}

func isTerminal(status model.Status) bool {
	return status == model.StatusCompleted || status == model.StatusFailed || status == model.StatusCancelled
}
