package app

import (
	"context"
	"fmt"
	"time"

	"genesis-agent/internal/domain"
)

// RunOnce 同步执行一次 Agent 推理
func (s *agentServiceImpl) RunOnce(ctx context.Context, req RunRequest) (*RunResult, error) {
	agent := req.Agent
	if agent == nil {
		agent = s.defaultAgent
	}

	startTime := time.Now()
	run, err := s.runEngine.Start(ctx, domain.StartRunRequest{
		SessionID: req.SessionID,
		TenantID:  req.TenantID,
		UserInput: req.Input,
		Agent:     agent,
	})
	if err != nil {
		return nil, fmt.Errorf("Agent 推理失败: %w", err)
	}

	return &RunResult{
		Run:     run,
		Elapsed: time.Since(startTime),
	}, nil
}
