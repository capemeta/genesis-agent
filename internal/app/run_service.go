package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentappcontract "genesis-agent/internal/capabilities/agentapp/contract"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	fspermission "genesis-agent/internal/capabilities/filesystem/permission"
	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	memorycontract "genesis-agent/internal/capabilities/memory/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
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
	if req.Sandbox != nil && s.cfg != nil && !s.cfg.Sandbox.AllowSessionOverride {
		return nil, fmt.Errorf("当前配置不允许会话级 sandbox 覆盖")
	}
	if s.workspace.Preparer == nil || s.workspace.AgentApps == nil {
		return nil, fmt.Errorf("Run 工作空间控制面未配置")
	}
	scope := workmodel.ResourceScope{TenantID: req.TenantID, ProjectID: req.ProjectID, UserID: req.UserID}
	effectiveApp, err := s.workspace.AgentApps.ResolveEffective(ctx, agentappcontract.ResolveRequest{AppID: req.AppID, Scope: scope})
	if err != nil {
		return nil, fmt.Errorf("解析 Agent App 有效配置失败: %w", err)
	}
	if s.sessionStore != nil && strings.TrimSpace(req.SessionID) != "" {
		session, sessionErr := s.sessionStore.GetSession(ctx, req.SessionID)
		if sessionErr != nil {
			return nil, fmt.Errorf("读取 Run 会话失败: %w", sessionErr)
		}
		if session.TenantID != req.TenantID || session.UserID != req.UserID || session.AppID != effectiveApp.ID {
			return nil, fmt.Errorf("Run 会话作用域与 tenant/user/agent app 不匹配")
		}
	}
	projectRoot, err := bindProjectRootScope(s.workspace.ProjectRoot, scope)
	if err != nil {
		return nil, fmt.Errorf("绑定项目资源作用域失败: %w", err)
	}
	intent := req.WorkspaceIntent
	if s.workspace.IntentResolver == nil {
		return nil, fmt.Errorf("Run 任务意图控制面未配置")
	}
	intent, err = s.workspace.IntentResolver.ResolveIntent(ctx, workcontract.ResolveIntentRequest{Prompt: req.Input, Supplied: intent, HasProject: projectRoot != nil})
	if err != nil {
		return nil, fmt.Errorf("解析 Run 任务意图失败: %w", err)
	}
	inputs := append([]workmodel.ResourceRef(nil), req.Inputs...)
	if intent.BoundedInputs && s.workspace.RequestInputs != nil {
		planned, planErr := s.workspace.RequestInputs.PlanRequestInputs(ctx, workcontract.RequestInputRequest{Prompt: req.Input, Scope: scope})
		if planErr != nil {
			return nil, fmt.Errorf("解析 Run 请求输入失败: %w", planErr)
		}
		inputs = mergeResourceRefs(inputs, planned)
	}
	prepared, err := s.workspace.Preparer.PrepareRun(ctx, workcontract.PrepareRunRequest{
		Scope:     scope,
		SessionID: req.SessionID, ParentRunID: req.ParentRunID, AgentID: agent.ID,
		App: effectiveApp, Intent: intent, ProjectRoot: projectRoot, ProjectDir: s.workspace.ProjectDir,
		ProductModes: s.workspace.ProductModes, PolicyModes: s.workspace.PolicyModes, BackendModes: s.workspace.BackendModes,
		MaximumAccess: s.workspace.MaximumAccess,
		Inputs:        inputs,
	})
	if err != nil {
		return nil, fmt.Errorf("准备 Run 工作空间失败: %w", err)
	}
	for _, resource := range s.workspace.RunResources {
		if resource == nil {
			continue
		}
		defer resource.ReleaseRun(context.WithoutCancel(ctx), prepared)
	}
	if s.workspace.WorkspaceCompletion != nil {
		if err := s.workspace.WorkspaceCompletion.InitializeRun(ctx, prepared); err != nil {
			return nil, fmt.Errorf("初始化 workspace 完成门禁失败: %w", err)
		}
		defer s.workspace.WorkspaceCompletion.ReleaseRun(prepared)
	}
	if s.workspace.ArtifactRuns != nil {
		if err := s.workspace.ArtifactRuns.InitializeRun(ctx, artifactcontract.RunInitializationRequest{
			TenantID: req.TenantID, RunID: prepared.Manifest.RunID, Prompt: req.Input,
			ArtifactRequired: intent.ArtifactRequired || len(req.Deliverables) > 0,
			Deliverables:     append([]artifactcontract.DeclaredDeliverable(nil), req.Deliverables...),
		}); err != nil {
			return nil, fmt.Errorf("初始化 Run 交付契约失败: %w", err)
		}
	}
	ctx = workcontract.WithPreparedRun(ctx, prepared)
	ctx = workcontract.WithControlPlane(ctx, s.workspace.Preparer)
	ctx = artifactcontract.WithCompletionPolicy(ctx, s.workspace.Completion)
	ctx = workcontract.WithCompletionGuard(ctx, s.workspace.WorkspaceCompletion)
	ctx = artifactcontract.WithQAEvidenceRecorder(ctx, s.workspace.QAEvidence)
	if s.cfg != nil {
		ctx = fspermission.WithPermissionMode(ctx, fspermission.NormalizeMode(s.cfg.Policy.PermissionMode))
	}
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
		RunID:       prepared.Manifest.RunID,
		SessionID:   req.SessionID,
		TenantID:    req.TenantID,
		UserInput:   req.Input,
		Attachments: req.Attachments,
		Agent:       agent,
	})
	s.updateSessionAfterRun(ctx, req, run)
	if err != nil {
		status := "failed"
		if run != nil && run.Status == domain.RunStatusCancelled {
			status = "cancelled"
		}
		dispatchRunComplete(context.WithoutCancel(ctx), status, "", true)
		return nil, fmt.Errorf("Agent 推理失败: %w", err)
	}
	dispatchRunComplete(ctx, "completed", run.FinalAnswer, run.Incomplete)

	return &RunResult{
		Run:     run,
		Elapsed: time.Since(startTime),
	}, nil
}

func mergeResourceRefs(groups ...[]workmodel.ResourceRef) []workmodel.ResourceRef {
	seen := make(map[string]struct{})
	var merged []workmodel.ResourceRef
	for _, group := range groups {
		for _, ref := range group {
			key := ref.Authority + "\x00" + ref.Scheme + "\x00" + ref.ID + "\x00" + ref.Version
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, ref)
		}
	}
	return merged
}

// bindProjectRootScope 把产品入口已授权、但装配时尚未知调用者的项目引用绑定到本次 Run。
// 已带非空作用域的引用只能收窄到兼容 Run，禁止借此跨 tenant/project/user 复用。
func bindProjectRootScope(ref *workmodel.ResourceRef, scope workmodel.ResourceScope) (*workmodel.ResourceRef, error) {
	if ref == nil {
		return nil, nil
	}
	if !scopeFieldCompatible(ref.Scope.TenantID, scope.TenantID) ||
		!scopeFieldCompatible(ref.Scope.ProjectID, scope.ProjectID) ||
		!scopeFieldCompatible(ref.Scope.UserID, scope.UserID) {
		return nil, fmt.Errorf("project root scope 与 Run scope 冲突")
	}
	bound := *ref
	bound.Scope = scope
	return &bound, nil
}

func scopeFieldCompatible(resourceValue, runValue string) bool {
	return resourceValue == "" || resourceValue == runValue
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
