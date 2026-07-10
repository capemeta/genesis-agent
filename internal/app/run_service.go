package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/runtime/progress"
)

// RunOnce 同步执行一次 Agent 推理
func (s *agentServiceImpl) RunOnce(ctx context.Context, req RunRequest) (*RunResult, error) {
	agent := req.Agent
	if agent == nil {
		agent = s.defaultAgent
	}

	startTime := time.Now()
	if req.OnProgress != nil {
		ctx = progress.WithSink(ctx, req.OnProgress)
	}
	if strings.TrimSpace(req.SessionID) != "" {
		ctx = contextutil.WithSessionID(ctx, req.SessionID)
	}
	if req.Sandbox != nil {
		if s.cfg != nil && !s.cfg.Sandbox.AllowSessionOverride {
			return nil, fmt.Errorf("当前配置不允许会话级 sandbox 覆盖")
		}
		ctx = contextutil.WithSandboxProfileOverride(ctx, *req.Sandbox)
	}
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
