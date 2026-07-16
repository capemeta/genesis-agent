package background

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"genesis-agent/internal/runtime/multiagent/contract"
)

type MemoryControlStore struct {
	mu         sync.Mutex
	leases     map[string]contract.Lease
	heartbeats map[string]time.Time
	stops      map[string]struct{}
	now        func() time.Time
}

func NewMemoryControlStore() *MemoryControlStore {
	return &MemoryControlStore{
		leases:     make(map[string]contract.Lease),
		heartbeats: make(map[string]time.Time),
		stops:      make(map[string]struct{}),
		now:        time.Now,
	}
}

func (s *MemoryControlStore) Acquire(ctx context.Context, lease contract.Lease) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validateLease(lease); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[lease.AgentID]
	if ok && current.OwnerID != lease.OwnerID && current.ExpiresAt.After(s.now()) {
		return false, nil
	}
	s.leases[lease.AgentID] = lease
	return true, nil
}

func (s *MemoryControlStore) Renew(ctx context.Context, lease contract.Lease) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validateLease(lease); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[lease.AgentID]
	if !ok || current.OwnerID != lease.OwnerID {
		return false, nil
	}
	s.leases[lease.AgentID] = lease
	return true, nil
}

func (s *MemoryControlStore) Release(ctx context.Context, agentID, ownerID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	agentID = strings.TrimSpace(agentID)
	ownerID = strings.TrimSpace(ownerID)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[agentID]
	if ok && current.OwnerID == ownerID {
		delete(s.leases, agentID)
	}
	return nil
}

func (s *MemoryControlStore) RequestStop(ctx context.Context, agentID, requesterRunID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return fmt.Errorf("agent_id 不能为空")
	}
	if strings.TrimSpace(requesterRunID) == "" {
		return fmt.Errorf("requester_run_id 不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops[agentID] = struct{}{}
	return nil
}

func (s *MemoryControlStore) PollStop(ctx context.Context, agentID, ownerID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	agentID = strings.TrimSpace(agentID)
	ownerID = strings.TrimSpace(ownerID)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[agentID]
	if !ok || current.OwnerID != ownerID {
		return false, nil
	}
	_, ok = s.stops[agentID]
	return ok, nil
}

func (s *MemoryControlStore) ClearStop(ctx context.Context, agentID, ownerID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	agentID = strings.TrimSpace(agentID)
	ownerID = strings.TrimSpace(ownerID)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[agentID]
	if ok && current.OwnerID == ownerID {
		delete(s.stops, agentID)
	}
	return nil
}

func (s *MemoryControlStore) Heartbeat(ctx context.Context, agentID, ownerID string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	agentID = strings.TrimSpace(agentID)
	ownerID = strings.TrimSpace(ownerID)
	if agentID == "" || ownerID == "" {
		return fmt.Errorf("心跳缺少 agent_id 或 owner_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[agentID]
	if !ok || current.OwnerID != ownerID {
		return fmt.Errorf("无权写入子智能体心跳")
	}
	s.heartbeats[agentID] = at
	return nil
}

func (s *MemoryControlStore) LastHeartbeat(ctx context.Context, agentID string) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	agentID = strings.TrimSpace(agentID)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heartbeats[agentID], nil
}

func validateLease(lease contract.Lease) error {
	if strings.TrimSpace(lease.AgentID) == "" {
		return fmt.Errorf("lease agent_id 不能为空")
	}
	if strings.TrimSpace(lease.OwnerID) == "" {
		return fmt.Errorf("lease owner_id 不能为空")
	}
	if lease.ExpiresAt.IsZero() {
		return fmt.Errorf("lease expires_at 不能为空")
	}
	return nil
}
