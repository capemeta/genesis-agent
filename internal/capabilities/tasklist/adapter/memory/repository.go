// Package memory 提供 TaskListRepository 的进程内实现。
package memory

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"genesis-agent/internal/capabilities/tasklist/contract"
	"genesis-agent/internal/capabilities/tasklist/model"
)

var _ contract.Repository = (*Repository)(nil)

type Repository struct {
	mu      sync.RWMutex
	plans   map[string]*model.TaskList
	history map[string][]model.RevisionLog
}

func New() *Repository {
	return &Repository{plans: map[string]*model.TaskList{}, history: map[string][]model.RevisionLog{}}
}

func (r *Repository) GetTaskList(ctx context.Context, sessionID string) (*model.TaskList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if safeID(sessionID) == "" {
		return nil, fmt.Errorf("invalid session id: %q", sessionID)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	plan := r.plans[sessionID]
	if plan == nil {
		return nil, nil
	}
	copy := *plan
	copy.Steps = append([]model.Step(nil), plan.Steps...)
	return &copy, nil
}

func (r *Repository) SaveTaskList(ctx context.Context, plan *model.TaskList, revision *model.RevisionLog) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan == nil || safeID(plan.SessionID) == "" {
		return fmt.Errorf("invalid session id: %q", valueSessionID(plan))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.plans[plan.SessionID]; existing != nil && plan.Version <= existing.Version {
		return fmt.Errorf("concurrency conflict: current version is %d, proposed version %d must be greater", existing.Version, plan.Version)
	}
	copy := *plan
	copy.Steps = append([]model.Step(nil), plan.Steps...)
	r.plans[plan.SessionID] = &copy
	if revision != nil {
		r.history[plan.SessionID] = append(r.history[plan.SessionID], *revision)
	}
	return nil
}

func (r *Repository) GetHistory(ctx context.Context, sessionID string) ([]model.RevisionLog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if safeID(sessionID) == "" {
		return nil, fmt.Errorf("invalid session id: %q", sessionID)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]model.RevisionLog(nil), r.history[sessionID]...), nil
}

var sessionIDRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func safeID(raw string) string { return sessionIDRegex.ReplaceAllString(raw, "") }

func valueSessionID(plan *model.TaskList) string {
	if plan == nil {
		return ""
	}
	return plan.SessionID
}
