package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"genesis-agent/internal/runtime/multiagent/contract"
)

type memoryResultDeliveryStore struct {
	mu   sync.Mutex
	seen map[contract.ResultDeliveryKey]struct{}
}

func NewMemoryResultDeliveryStore() contract.ResultDeliveryStore {
	return &memoryResultDeliveryStore{seen: make(map[contract.ResultDeliveryKey]struct{})}
}

func (s *memoryResultDeliveryStore) MarkDelivered(ctx context.Context, key contract.ResultDeliveryKey) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validateDeliveryKey(key); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[key]; ok {
		return true, nil
	}
	s.seen[key] = struct{}{}
	return false, nil
}

func validateDeliveryKey(key contract.ResultDeliveryKey) error {
	values := map[string]string{
		"tenant_id":     key.TenantID,
		"session_id":    key.SessionID,
		"parent_run_id": key.ParentRunID,
		"agent_id":      key.AgentID,
		"result_id":     key.ResultID,
	}
	for name, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("TaskOutput 结果交付键缺少 %s", name)
		}
		if len(value) > 512 {
			return fmt.Errorf("TaskOutput 结果交付键 %s 过长", name)
		}
	}
	return nil
}
