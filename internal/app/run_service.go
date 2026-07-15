package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	memorycontract "genesis-agent/internal/capabilities/memory/contract"
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
	if s.hooks != nil {
		ctx = hookcontract.WithDispatcher(ctx, s.hooks)
	}
	ctx = hookcontract.WithScopeContext(ctx, hookmodel.ScopeContext{TenantID: req.TenantID, ProjectID: req.ProjectID, UserID: req.UserID, RoleIDs: append([]string(nil), req.RoleIDs...), AgentID: agent.ID})

	// 显式注入 SessionRef 保证画像与长期记忆能够定位 UserID 作用域
	ctx = memorycontract.ContextWithSessionRef(ctx, memorycontract.SessionRef{
		SessionID: req.SessionID,
		TenantID:  req.TenantID,
		UserID:    req.UserID,
	})

	if strings.TrimSpace(req.SessionID) != "" {
		ctx = contextutil.WithSessionID(ctx, req.SessionID)
	}
	if strings.TrimSpace(req.TenantID) != "" {
		ctx = contextutil.WithTenantID(ctx, req.TenantID)
	}
	if strings.TrimSpace(req.UserID) != "" {
		ctx = contextutil.WithUserID(ctx, req.UserID)
	}
	if req.Sandbox != nil {
		if s.cfg != nil && !s.cfg.Sandbox.AllowSessionOverride {
			return nil, fmt.Errorf("当前配置不允许会话级 sandbox 覆盖")
		}
		ctx = contextutil.WithSandboxProfileOverride(ctx, *req.Sandbox)
	}
	if s.sessionStore != nil && strings.TrimSpace(req.SessionID) != "" {
		// 会话空闲后再次运行时恢复 active，压缩器依赖该状态进行 CAS 互斥。
		if _, statusErr := s.sessionStore.UpdateStatus(ctx, req.SessionID, domain.SessionStateIdle, domain.SessionStateActive); statusErr != nil {
			return nil, fmt.Errorf("activate session: %w", statusErr)
		}
	}
	if dispatcher := hookcontract.FromContext(ctx); dispatcher != nil {
		result, dispatchErr := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventRunStart, Payload: map[string]any{"user_prompt": req.Input}})
		if dispatchErr != nil {
			return nil, fmt.Errorf("执行 RunStart Hook 失败: %w", dispatchErr)
		}
		hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
	}
	run, err := s.runEngine.Start(ctx, domain.StartRunRequest{
		SessionID: req.SessionID,
		TenantID:  req.TenantID,
		UserInput: req.Input,
		Agent:     agent,
	})
	s.updateSessionAfterRun(ctx, req, run)
	if err != nil {
		dispatchRunComplete(ctx, "failed", "", true)
		return nil, fmt.Errorf("Agent 推理失败: %w", err)
	}
	dispatchRunComplete(ctx, "completed", run.FinalAnswer, run.Incomplete)

	return &RunResult{
		Run:     run,
		Elapsed: time.Since(startTime),
	}, nil
}

func (s *agentServiceImpl) updateSessionAfterRun(ctx context.Context, req RunRequest, run *domain.Run) {
	if s.sessionStore == nil || run == nil || strings.TrimSpace(req.SessionID) == "" {
		return
	}
	session, err := s.sessionStore.GetSession(ctx, req.SessionID)
	if err != nil {
		return
	}
	session.Status = domain.SessionStateIdle
	session.TotalTokens += run.TotalTokens
	if session.Title == "" {
		session.Title = sessionTitle(req.Input)
	}
	if s.memStore != nil {
		if summary, summaryErr := s.memStore.GetSummary(ctx, memorycontract.SessionRef{SessionID: req.SessionID, TenantID: req.TenantID, UserID: req.UserID}); summaryErr == nil && summary != nil {
			session.SummaryLeafID = summary.LeafID
		}
	}
	_ = s.sessionStore.UpdateSession(ctx, session)
}

func sessionTitle(input string) string {
	const maxRunes = 48
	runes := []rune(strings.TrimSpace(input))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes])
}

func dispatchRunComplete(ctx context.Context, status, finalAnswer string, incomplete bool) {
	dispatcher := hookcontract.FromContext(ctx)
	if dispatcher == nil {
		return
	}
	result, err := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventRunComplete, Payload: map[string]any{"status": status, "final_answer": finalAnswer, "incomplete": incomplete}})
	if err == nil {
		hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
	}
}
