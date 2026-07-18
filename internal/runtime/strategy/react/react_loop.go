// package react - ReAct Loop策略实现
// ReAct (Reason + Act)：每轮循环中LLM先推理(Think)，再决定行动(Act/工具调用)，直到给出最终答案
// 对应 AGENTS.md §4.2 ReAct Loop执行流程
//
// 重要：本文件不依赖任何 eino 包，所有 LLM 操作通过 llm.ChatModel 接口进行，
// eino 细节完全封装在 llm 包的适配层中
package react

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	"genesis-agent/internal/capabilities/llm/contract"
	"genesis-agent/internal/capabilities/memory/contract"
	skillcollision "genesis-agent/internal/capabilities/skill/collision"
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
	"genesis-agent/internal/capabilities/trace/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
	runtimecontext "genesis-agent/internal/runtime/context"
	"genesis-agent/internal/runtime/multiagent/contextsnapshot"
	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/sanitize"
	"genesis-agent/internal/runtime/progress"
	"genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/repeatguard"
	"genesis-agent/internal/runtime/transcript"
)

// SkillNameMatcher 识别「把 Skill 名当成 Tool 名」的误调用（由产品注入，Gateway 不依赖 skill）。
type SkillNameMatcher interface {
	Match(ctx context.Context, name string) (canonical string, ok bool, err error)
}

// ReactLoopEngine ReAct Loop策略的RunEngine实现
type ReactLoopEngine struct {
	llm                       llm.ChatModel
	registry                  tool.Registry
	memory                    memory.ShortTermMemory
	prompt                    prompt.Builder
	logger                    logger.Logger
	tracer                    trace.Tracer
	skillNameMatcher          SkillNameMatcher
	skillMentionSelector      SkillMentionSelector
	skillExplicitLoader       SkillExplicitLoader
	autoRewriteSkillCollision *bool // nil 表示默认开启

	// 新增：上下文窗口规划与 Token 估算
	estimator             runtimecontext.TokenEstimator
	planner               *runtimecontext.ContextBudgetPlanner
	contextWindow         int
	maxTokens             int
	effectiveContextRatio float64
	outputReserveTokens   int
	compactor             runtimecontext.Compactor

	// 新增：长期记忆与用户画像
	ltm              memory.LongTermMemory
	userProfileStore memory.UserProfileStore
}

// EngineOption 可选依赖。
type EngineOption func(*ReactLoopEngine)

// WithLongTermMemory 注入长期记忆存储后端
func WithLongTermMemory(ltm memory.LongTermMemory) EngineOption {
	return func(e *ReactLoopEngine) {
		e.ltm = ltm
	}
}

// WithUserProfileStore 注入用户画像存储后端
func WithUserProfileStore(ups memory.UserProfileStore) EngineOption {
	return func(e *ReactLoopEngine) {
		e.userProfileStore = ups
	}
}

// WithCompactor 注入两级压缩编排器。
func WithCompactor(compactor runtimecontext.Compactor) EngineOption {
	return func(e *ReactLoopEngine) {
		e.compactor = compactor
	}
}

// WithContextBudgetConfig 注入模型上下文预算配置。
func WithContextBudgetConfig(effectiveRatio float64, outputReserveTokens int) EngineOption {
	return func(e *ReactLoopEngine) {
		e.effectiveContextRatio = effectiveRatio
		e.outputReserveTokens = outputReserveTokens
	}
}

// WithSkillNameMatcher 注入 Skill/Tool 名碰撞检测。
func WithSkillNameMatcher(matcher SkillNameMatcher) EngineOption {
	return func(e *ReactLoopEngine) {
		e.skillNameMatcher = matcher
	}
}

// NewReactLoopEngine 创建ReAct Loop引擎，所有依赖通过构造函数注入
func NewReactLoopEngine(
	llmClient llm.ChatModel,
	registry tool.Registry,
	store memory.ShortTermMemory,
	promptBuilder prompt.Builder,
	log logger.Logger,
	tracer trace.Tracer,
	estimator runtimecontext.TokenEstimator,
	planner *runtimecontext.ContextBudgetPlanner,
	contextWindow int,
	maxTokens int,
	opts ...EngineOption,
) *ReactLoopEngine {
	e := &ReactLoopEngine{
		llm:                   llmClient,
		registry:              registry,
		memory:                store,
		prompt:                promptBuilder,
		logger:                log,
		tracer:                tracer,
		estimator:             estimator,
		planner:               planner,
		contextWindow:         contextWindow,
		maxTokens:             maxTokens,
		effectiveContextRatio: 0.92,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

func (e *ReactLoopEngine) GetStrategyName() string { return "react_loop" }

// Start 启动并执行一次完整的ReAct Loop
func (e *ReactLoopEngine) Start(ctx context.Context, req domain.StartRunRequest) (*domain.Run, error) {
	if strings.TrimSpace(req.RunID) == "" {
		return nil, fmt.Errorf("启动 Run 缺少控制面生成的 Run ID")
	}
	run := &domain.Run{
		ID:        req.RunID,
		TenantID:  req.TenantID,
		SessionID: req.SessionID,
		Status:    domain.RunStatusRunning,
		StartedAt: time.Now(),
		Steps:     make([]*domain.Step, 0),
	}

	log := e.logger.With("run_id", run.ID, "session_id", req.SessionID)
	log.Info("Run启动", "agent", req.Agent.Name, "model", req.Agent.DefaultModel)
	progress.Emit(ctx, progress.Event{
		Kind:    progress.KindRun,
		Phase:   progress.PhaseStart,
		RunID:   run.ID,
		Name:    req.Agent.Name,
		Summary: "启动 Agent 运行",
		Metadata: map[string]string{
			"model": req.Agent.DefaultModel,
		},
	})

	// 关联键注入：下游 Gateway / Skill / audit / usage 从 context 读取同一 run_id。
	ctx = contextutil.WithRunID(ctx, run.ID)
	if strings.TrimSpace(req.SessionID) != "" {
		ctx = contextutil.WithSessionID(ctx, req.SessionID)
	}
	ctx = contextutil.WithTenantID(ctx, req.TenantID)

	// 启动根Span（覆盖整个Run生命周期）
	runSpan := e.tracer.StartSpan(ctx, "run", run.ID)

	rc := runtime.NewRunContext(run, req.Agent)
	ctx = contextutil.WithApprovalGrantedHook(ctx, func(context.Context) {
		if rc.RepeatGuard == nil {
			return
		}
		rc.RepeatGuard.ClearApprovalDenied()
		rc.RepeatGuard.MarkUserIntervention()
	})
	err := e.loop(ctx, rc, req, log)

	e.tracer.EndSpan(ctx, runSpan, err)

	now := time.Now()
	run.FinishedAt = &now

	if err != nil {
		if errors.Is(err, approvalcontract.ErrRunAborted) || errors.Is(err, context.Canceled) {
			run.Status = domain.RunStatusCancelled
			log.Info("Run已取消", "steps", len(run.Steps), "tokens", run.TotalTokens)
			progress.Emit(ctx, progress.Event{Kind: progress.KindRun, Phase: progress.PhaseComplete, Level: progress.LevelWarn, RunID: run.ID, Summary: "Agent 运行已取消", StopReason: "cancelled"})
			return run, err
		}
		run.Status = domain.RunStatusFailed
		log.Error("Run失败", "error", err, "steps", len(run.Steps), "tokens", run.TotalTokens)
		progress.Emit(ctx, progress.Event{Kind: progress.KindRun, Phase: progress.PhaseError, Level: progress.LevelError, RunID: run.ID, Summary: "Agent 运行失败", Detail: err.Error()})
		return run, err
	}

	log.Info("Run完成", "steps", len(run.Steps), "tokens", run.TotalTokens, "answer_len", len(run.FinalAnswer), "incomplete", run.Incomplete)
	summary := "Agent 运行完成"
	if run.Incomplete {
		summary = "Agent 运行完成（结果可能不完整）"
	}
	progress.Emit(ctx, progress.Event{Kind: progress.KindRun, Phase: progress.PhaseComplete, RunID: run.ID, Summary: summary})
	return run, nil
}

// loop 核心执行循环
func (e *ReactLoopEngine) loop(ctx context.Context, rc *runtime.RunContext, req domain.StartRunRequest, log logger.Logger) (err error) {
	agent := req.Agent
	maxIter := agent.RuntimePolicy.MaxIterations
	if maxIter <= 0 {
		maxIter = 50
	}

	// historyLen 供 defer 截取本 Run 增量；仅在上下文装配成功后启用落盘。
	// 装配成功后的任意出口（成功 / Incomplete / LLM 失败 / 取消 / 超迭代）都落盘，保证「请继续」可恢复。
	historyLen := 0
	assembled := false
	defer func() {
		if !assembled {
			return
		}
		e.persistRunSessionMessages(ctx, req.SessionID, rc, historyLen, err, log)
	}()

	if dispatcher := hookcontract.FromContext(ctx); dispatcher != nil {
		result, hookErr := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventUserPromptSubmit, Payload: map[string]any{"user_prompt": req.UserInput}})
		if hookErr != nil {
			return fmt.Errorf("执行 UserPromptSubmit Hook 失败: %w", hookErr)
		}
		hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
		if result.Blocked {
			return fmt.Errorf("用户输入被 Hook 阻断: %s", result.BlockReason)
		}
	}

	// ── 步骤1: 获取工具定义（tool.Info，与外部框架无关）─────────
	toolInfos := e.getToolInfos(agent)
	activeToolNames := namesOfToolInfos(toolInfos)

	// ── 步骤2: 构建初始消息列表与弹性预算装配 ──────────────────
	// System消息（Persona System Prompt）
	systemPrompt, err := e.prompt.BuildSystem(ctx, prompt.BuildRequest{
		Agent:          agent,
		Run:            rc.Run,
		AvailableTools: activeToolNames,
	})
	if err != nil {
		return err
	}
	systemTokens := e.estimator.Estimate(ctx, systemPrompt, req.Agent.DefaultModel)

	// 工具定义 DTO 的 Schema Token 估算
	toolsTokens := 0
	for _, t := range toolInfos {
		if t != nil {
			toolsTokens += e.estimator.Estimate(ctx, t.Name, req.Agent.DefaultModel)
			toolsTokens += e.estimator.Estimate(ctx, tool.ResolveDescription(ctx, t), req.Agent.DefaultModel)
			if t.Parameters != nil {
				if data, mErr := json.Marshal(t.Parameters); mErr == nil {
					toolsTokens += e.estimator.Estimate(ctx, string(data), req.Agent.DefaultModel)
				}
			}
		}
	}

	// 当前轮输入 Token 估算
	userTokens := e.estimator.Estimate(ctx, req.UserInput, req.Agent.DefaultModel)

	// 弹性段预算规划 (Clamp + Reflow)
	ref := memory.SessionRef{SessionID: req.SessionID, TenantID: req.TenantID}
	if sessionRef, ok := memory.SessionRefFromCtx(ctx); ok {
		ref = sessionRef
	}
	var historySummary *domain.SessionSummary
	actualSummaryTokens := 0
	if e.memory != nil {
		if summary, sErr := e.memory.GetSummary(ctx, ref); sErr == nil && summary != nil {
			historySummary = summary
			actualSummaryTokens = summary.TokensCount
		}
	}

	// ── 跨会话长期记忆与画像前置检索 ──────────────────
	var up *domain.UserProfile
	var ltmEntries []*domain.LongTermEntry
	actualLTMTokens := 0

	if e.ltm != nil {
		sessionRef, ok := memory.SessionRefFromCtx(ctx)
		if !ok {
			sessionRef = ref
			sessionRef.TenantID = req.TenantID
		}

		profileOptIn := true
		if e.userProfileStore != nil && sessionRef.UserID != "" {
			if loadedUp, upErr := e.userProfileStore.Get(ctx, sessionRef.TenantID, sessionRef.UserID); upErr == nil && loadedUp != nil {
				up = loadedUp
				profileOptIn = up.Builtin.MemoryOptIn
			}
		}

		if profileOptIn {
			var scopes []domain.MemoryScope
			if sessionRef.UserID != "" {
				scopes = append(scopes, domain.MemoryScope{Type: domain.MemoryScopeUser, ID: sessionRef.UserID})
			}
			scopes = append(scopes, domain.MemoryScope{Type: domain.MemoryScopeWorkspace, ID: req.SessionID})

			mQuery := domain.MemoryQuery{
				Query:  req.UserInput,
				Scopes: scopes,
				TopK:   5,
				SortBy: domain.MemorySortByComposite,
			}

			if entries, ltmErr := e.ltm.Search(ctx, sessionRef, mQuery); ltmErr == nil {
				ltmEntries = entries
				for _, entry := range entries {
					if entry != nil {
						actualLTMTokens += e.estimator.Estimate(ctx, "- "+entry.Content, req.Agent.DefaultModel)
					}
				}
			} else {
				log.Warn("检索长期记忆失败", "error", ltmErr)
			}
		}
	}

	planOpt := runtimecontext.PlanOptions{
		ContextWindow:         e.contextWindow,
		EffectiveContextRatio: e.effectiveContextRatio,
		MaxTokens:             e.maxTokens,
		OutputReserveTokens:   e.outputReserveTokens,
		Strategy:              req.ContextStrategy,
		StableSystemTokens:    systemTokens,
		ToolsSchemaTokens:     toolsTokens,
		CurrentUserTokens:     userTokens,
		ActualSummaryTokens:   actualSummaryTokens,
		ActualLTMTokens:       actualLTMTokens,
	}

	budget, inputBudget, planErr := e.planner.Plan(ctx, planOpt)
	if planErr != nil {
		log.Error("Planner 预算分配失败，执行安全退化兜底", "error", planErr)
		budget = runtimecontext.DistributableBudget{
			History: 4096, // 退化安全值
		}
		inputBudget = int(float64(e.contextWindow)*e.effectiveContextRatio) - e.outputReserveTokens
	}

	// 拉取适量历史消息；加载失败必须中止，禁止静默空历史导致「请继续」失忆。
	recentOpt := memory.RecentOptions{
		MaxTokens: budget.History,
		Model:     req.Agent.DefaultModel,
	}
	var history []*domain.Message
	if e.memory != nil {
		res, getErr := e.memory.GetRecent(ctx, ref, recentOpt)
		if getErr != nil {
			return fmt.Errorf("加载短期记忆历史失败: %w", getErr)
		}
		history = res.Messages
	}

	// 精密组装 messages (system -> history -> working_obs -> user)
	assembler := runtimecontext.NewDefaultContextAssembler(e.estimator)
	assemblerOpt := runtimecontext.AssemblerOptions{
		Budget:             budget,
		MaxInputTokens:     inputBudget,
		Model:              req.Agent.DefaultModel,
		SystemPrompt:       systemPrompt,
		UserProfile:        up,
		LongTermMemories:   ltmEntries,
		HistorySummary:     historySummary,
		HistoryMessages:    history,
		CurrentUserMessage: domain.NewUserMessage(req.UserInput),
	}

	rc.Messages, err = assembler.Assemble(ctx, assemblerOpt)
	if err != nil {
		return fmt.Errorf("assemble context messages failed: %w", err)
	}
	// historyLen 必须等于装配后实际进入 rc.Messages 的历史条数（可能被预算截断），
	// 不能用 GetRecent 原始条数，否则 SessionMessagesFromRun 起点越界导致整轮不落盘。
	historyLen = countAssembledHistoryLen(rc.Messages)
	assembled = true

	// mention 自动注入（在首轮 LLM 前；走 Skill 网关，含 Approval / 去重 / 收窄）
	e.injectMentionedSkills(ctx, rc, req.UserInput, &activeToolNames, &toolInfos, log)

	// 打印已加载工具
	toolNames := make([]string, 0, len(toolInfos))
	for _, t := range toolInfos {
		toolNames = append(toolNames, t.Name)
	}
	log.Info("准备执行Loop", "max_iterations", maxIter, "tools", strings.Join(toolNames, ","))

	// ── 步骤3: 主循环 ─────────────────────────────────────────
	stopBlocks := 0
	deliveryBlocks := 0
	for rc.Iteration = 0; rc.Iteration < maxIter; rc.Iteration++ {
		if additions := hookcontract.DrainAdditionalContext(ctx); len(additions) > 0 {
			rc.Messages = append(rc.Messages, domain.NewSystemMessage(strings.Join(additions, "\n")))
		}
		iterLog := log.With("iteration", rc.Iteration)
		if rc.RepeatGuard != nil {
			rc.RepeatGuard.BeginIteration()
		}

		stepID := fmt.Sprintf("%s-step-%d", rc.Run.ID, rc.Iteration)
		stepSpan := e.tracer.StartSpan(ctx, "step", stepID)

		step := &domain.Step{
			ID:         stepID,
			RunID:      rc.Run.ID,
			StepIndex:  rc.Iteration,
			ActionType: domain.ActionTypeThink,
			Status:     domain.StepStatusRunning,
			StartedAt:  time.Now(),
		}

		// ── LLM推理（通过我们的接口，不感知 eino）───────────────
		iterLog.Info("调用LLM推理...")

		thinkingBlockIdx := rc.NextBlockIndex()
		stepIdx := rc.Iteration
		displayFalse := false
		progress.Emit(ctx, progress.Event{
			Kind:       progress.KindLLM,
			Phase:      progress.PhaseStart,
			RunID:      rc.Run.ID,
			StepID:     stepID,
			Name:       req.Agent.DefaultModel,
			Summary:    fmt.Sprintf("第 %d 轮调用 LLM", rc.Iteration+1),
			BlockIndex: &thinkingBlockIdx,
			BlockType:  "thinking",
			StepIndex:  &stepIdx,
			Display:    &displayFalse,
		})
		var contentBlockIdx *int
		displayTrue := true

		onDelta := func(delta string, isThought bool) {
			if isThought {
				// CoT / reasoning：按用户选择在对话中实时展示。
				progress.Emit(ctx, progress.Event{
					Kind:       progress.KindLLM,
					Phase:      progress.PhaseProgress,
					RunID:      rc.Run.ID,
					StepID:     stepID,
					BlockIndex: &thinkingBlockIdx,
					BlockType:  "thinking",
					StepIndex:  &stepIdx,
					Display:    &displayTrue,
					DeltaType:  "text_delta",
					Detail:     delta,
					Summary:    "思考中",
				})
				return
			}
			// 工具调用前正文作为 assistant_draft 实时展示，最终回答另发 final_answer。
			if contentBlockIdx == nil {
				idx := rc.NextBlockIndex()
				contentBlockIdx = &idx
				progress.Emit(ctx, progress.Event{
					Kind:        progress.KindLLM,
					Phase:       progress.PhaseStart,
					RunID:       rc.Run.ID,
					StepID:      stepID,
					Summary:     "思考中",
					BlockIndex:  contentBlockIdx,
					BlockType:   "assistant_draft",
					StepIndex:   &stepIdx,
					Display:     &displayTrue,
					ContentType: "text/markdown",
				})
			}
			progress.Emit(ctx, progress.Event{
				Kind:       progress.KindLLM,
				Phase:      progress.PhaseProgress,
				RunID:      rc.Run.ID,
				StepID:     stepID,
				BlockIndex: contentBlockIdx,
				BlockType:  "assistant_draft",
				StepIndex:  &stepIdx,
				Display:    &displayTrue,
				DeltaType:  "text_delta",
				Detail:     delta,
				Summary:    "思考中",
			})
		}

		// 两级压缩编排接线
		if e.compactor != nil {
			// 1. L1 Micro-Compact 大工具结果就地外置卸载
			microRes, microErr := e.compactor.MaybeMicroCompact(ctx, rc)
			if microErr != nil {
				iterLog.Warn("L1 Micro Compact 失败", "err", microErr)
			} else if microRes.Triggered {
				iterLog.Info("L1 Micro Compact 成功触发", "saved_tokens", microRes.TokensSaved)
			}

			// 在压缩前，先记下本 Run 产生的增量消息数 N
			deltaCount := len(rc.Messages) - historyLen

			autoRes, compactErr := e.compactor.MaybeAutoCompact(ctx, rc, ref)
			if compactErr != nil {
				iterLog.Warn("L2 Auto Compact 失败", "err", compactErr)
			} else if autoRes.Triggered {
				iterLog.Info("L2 Auto Compact 成功触发", "saved_tokens", autoRes.TokensSaved, "summary_id", autoRes.SummaryID)
				// 自适应调整 historyLen，防止增量历史截断指针在重构后越界
				historyLen = len(rc.Messages) - deltaCount
				if historyLen < 0 {
					historyLen = 0
				}
			}
		}

		// ForModel：完整链送给 LLM（含 skill_injection / tool）；UI 另走 ForUI，禁止双写存储
		// 使用命名返回值 err，避免 := 遮蔽导致 defer 落盘读到错误的失败原因。
		var llmResp *domain.Message
		llmResp, err = e.llm.StreamGenerate(ctx, transcript.ProjectForModel(rc.Messages), toolInfos, onDelta)
		if err != nil {
			e.tracer.EndSpan(ctx, stepSpan, err)
			progress.Emit(ctx, progress.Event{
				Kind:       progress.KindLLM,
				Phase:      progress.PhaseError,
				Level:      progress.LevelError,
				RunID:      rc.Run.ID,
				StepID:     stepID,
				Summary:    "LLM 调用失败",
				Detail:     err.Error(),
				BlockIndex: &thinkingBlockIdx,
				BlockType:  "thinking",
				StepIndex:  &stepIdx,
				Display:    &displayFalse,
				StopReason: "error",
			})
			return fmt.Errorf("第%d轮LLM调用失败: %w", rc.Iteration, err)
		}
		iterLog.Debug("LLM响应", "tool_calls", len(llmResp.ToolCalls), "content_len", len(llmResp.Content))
		usage := domain.TokenUsage{CompletionTokens: int64(e.estimator.Estimate(ctx, llmResp.Content, req.Agent.DefaultModel))}
		if payload, marshalErr := json.Marshal(llmResp.ToolCalls); marshalErr == nil {
			usage.CompletionTokens += int64(e.estimator.Estimate(ctx, string(payload), req.Agent.DefaultModel))
		}
		usage.TotalTokens = usage.CompletionTokens
		if budget := contract.TreeBudgetFromContext(ctx); budget != nil {
			if err := budget.Consume(usage.TotalTokens, 0); err != nil {
				return err
			}
		}
		rc.AddTokens(usage)
		if limit := req.Agent.RuntimePolicy.MaxTokens; limit > 0 && rc.TokenUsed > limit {
			return fmt.Errorf("budget exceeded: tokens (used=%d, max=%d)", rc.TokenUsed, limit)
		}

		progress.Emit(ctx, progress.Event{
			Kind:       progress.KindLLM,
			Phase:      progress.PhaseComplete,
			RunID:      rc.Run.ID,
			StepID:     stepID,
			Name:       req.Agent.DefaultModel,
			Summary:    fmt.Sprintf("LLM 返回 %d 个工具调用", len(llmResp.ToolCalls)),
			BlockIndex: &thinkingBlockIdx,
			BlockType:  "thinking",
			StepIndex:  &stepIdx,
			Display:    &displayFalse,
			StopReason: "complete",
		})

		// ── 分支判断 ─────────────────────────────────────────
		if contentBlockIdx != nil {
			progress.Emit(ctx, progress.Event{
				Kind:       progress.KindLLM,
				Phase:      progress.PhaseComplete,
				RunID:      rc.Run.ID,
				StepID:     stepID,
				BlockIndex: contentBlockIdx,
				BlockType:  "assistant_draft",
				StepIndex:  &stepIdx,
				Display:    &displayTrue,
				StopReason: "complete",
			})
		}

		if len(llmResp.ToolCalls) > 0 {
			if limit := req.Agent.RuntimePolicy.MaxToolCalls; limit > 0 && rc.ToolCalls+len(llmResp.ToolCalls) > limit {
				return fmt.Errorf("budget exceeded: tool_calls (used=%d, requested=%d, max=%d)", rc.ToolCalls, len(llmResp.ToolCalls), limit)
			}
			if budget := contract.TreeBudgetFromContext(ctx); budget != nil {
				if err := budget.Consume(0, len(llmResp.ToolCalls)); err != nil {
					return err
				}
			}
			rc.ToolCalls += len(llmResp.ToolCalls)

			// ── 路径A: 执行工具调用 ──────────────────────────
			step.ActionType = domain.ActionTypeToolCall
			rc.Messages = append(rc.Messages, llmResp)

			if llmResp.Content != "" {
				iterLog.Info("LLM思考内容", "thought", summarizeToolOutputForLog(llmResp.Content, 0))
			}

			toolResults, toolErr := e.executeToolCalls(ctx, rc, llmResp.ToolCalls, iterLog)
			if toolErr != nil {
				e.tracer.EndSpan(ctx, stepSpan, toolErr)
				return toolErr
			}
			for _, toolResult := range toolResults {
				if e.applySkillToolResult(rc, toolResult, &activeToolNames, &toolInfos, iterLog) {
					continue
				}
				rc.Messages = append(rc.Messages, domain.NewToolResultMessage(toolResult.ID, toolResult.Content))
			}

			payload, _ := json.Marshal(llmResp.ToolCalls)
			step.ActionPayload = payload

			if stop, stopErr := e.applyRepeatGuardProgress(ctx, rc, iterLog, false); stop {
				now := time.Now()
				step.Status = domain.StepStatusCompleted
				step.FinishedAt = &now
				rc.AddStep(step)
				e.tracer.EndSpan(ctx, stepSpan, stopErr)
				return stopErr
			}

		} else if llmResp.Content != "" {
			// ── 路径B: 最终回答 ──────────────────────────────
			step.ActionType = domain.ActionTypeFinalAnswer
			pending, reminder, completionErr := artifactCompletionPending(ctx)
			if completionErr != nil {
				return fmt.Errorf("评估 Artifact 完成门禁: %w", completionErr)
			}
			if pending {
				deliveryBlocks++
				if deliveryBlocks > 1 {
					return artifactcontract.NewError(artifactcontract.ErrCodeArtifactDeliveryRequired, fmt.Errorf("required Deliverable 尚未满足持久化完成门禁；%s", reminder))
				}
				rc.Messages = append(rc.Messages, llmResp, domain.NewSystemMessage(reminder))
				now := time.Now()
				step.Status = domain.StepStatusCompleted
				step.FinishedAt = &now
				rc.AddStep(step)
				e.tracer.EndSpan(ctx, stepSpan, nil)
				continue
			}
			if rc.RepeatGuard != nil {
				_ = rc.RepeatGuard.EndIteration(rc.Iteration, true)
			}

			iterLog.Info("获得最终回答", "content_preview", truncate(llmResp.Content, 100))

			if dispatcher := hookcontract.FromContext(ctx); dispatcher != nil {
				result, hookErr := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventStop, Payload: map[string]any{"final_answer": llmResp.Content, "iterations": rc.Iteration + 1, "stop_active": stopBlocks > 0}})
				if hookErr != nil {
					return fmt.Errorf("执行 Stop Hook 失败: %w", hookErr)
				}
				hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
				if result.Blocked {
					stopBlocks++
					if stopBlocks > 5 {
						return fmt.Errorf("Stop Hook 连续阻断超过 5 次: %s", result.BlockReason)
					}
					rc.Messages = append(rc.Messages, domain.NewSystemMessage("Stop Hook 要求继续处理："+result.BlockReason))
					now := time.Now()
					step.Status = domain.StepStatusCompleted
					step.FinishedAt = &now
					rc.AddStep(step)
					e.tracer.EndSpan(ctx, stepSpan, nil)
					continue
				}
			}
			rc.Run.FinalAnswer = llmResp.Content
			rc.Run.Status = domain.RunStatusCompleted
			stopReason := "complete"
			if rc.Run.Incomplete {
				stopReason = "partial_complete"
			}

			// 流式阶段未向对话区展示正文；此处一次性写入 final_answer。
			answerBlockIdx := rc.NextBlockIndex()
			displayTrue := true
			progress.Emit(ctx, progress.Event{
				Kind:        progress.KindRun,
				Phase:       progress.PhaseStart,
				RunID:       rc.Run.ID,
				StepID:      stepID,
				Summary:     "生成最终回答",
				BlockIndex:  &answerBlockIdx,
				BlockType:   "final_answer",
				StepIndex:   &stepIdx,
				Display:     &displayTrue,
				ContentType: "text/markdown",
			})
			progress.Emit(ctx, progress.Event{
				Kind:       progress.KindRun,
				Phase:      progress.PhaseProgress,
				RunID:      rc.Run.ID,
				StepID:     stepID,
				BlockIndex: &answerBlockIdx,
				BlockType:  "final_answer",
				StepIndex:  &stepIdx,
				DeltaType:  "text_delta",
				Detail:     llmResp.Content,
			})
			meta := map[string]string{}
			if rc.Run.Incomplete {
				meta["incomplete"] = "true"
				meta["incomplete_reason"] = "skill_qa_pending"
			}
			progress.Emit(ctx, progress.Event{
				Kind:       progress.KindRun,
				Phase:      progress.PhaseComplete,
				RunID:      rc.Run.ID,
				StepID:     stepID,
				BlockIndex: &answerBlockIdx,
				BlockType:  "final_answer",
				StepIndex:  &stepIdx,
				Display:    &displayTrue,
				StopReason: stopReason,
				Metadata:   meta,
			})

			llmResp.EnsureKind()
			rc.Messages = append(rc.Messages, llmResp)

			now := time.Now()
			step.Status = domain.StepStatusCompleted
			step.FinishedAt = &now
			observation, _ := json.Marshal(map[string]string{"answer": llmResp.Content})
			step.Observation = observation
			rc.AddStep(step)

			e.tracer.EndSpan(ctx, stepSpan, nil)
			return nil

		} else {
			// LLM返回为空（异常情况）
			e.tracer.EndSpan(ctx, stepSpan, fmt.Errorf("empty response"))
			return fmt.Errorf("第%d轮LLM返回空响应（无工具调用也无文本内容）", rc.Iteration)
		}

		now := time.Now()
		step.Status = domain.StepStatusCompleted
		step.FinishedAt = &now
		rc.AddStep(step)
		e.tracer.EndSpan(ctx, stepSpan, nil)
	}

	return fmt.Errorf("超过最大迭代次数 %d，Loop未能得出最终答案", maxIter)
}

func artifactCompletionPending(ctx context.Context) (bool, string, error) {
	policy, ok := artifactcontract.CompletionPolicyFromContext(ctx)
	if !ok {
		return false, "", nil
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return false, "", fmt.Errorf("completion policy 已配置但缺少 prepared run")
	}
	decision, err := policy.EvaluateCompletion(ctx, prepared.Manifest.Scope.TenantID, prepared.Manifest.RunID)
	if err != nil {
		return false, "", err
	}
	if decision.Complete {
		return false, "", nil
	}
	data, _ := json.Marshal(decision)
	return true, "Harness 根据持久化 Deliverable/Publication/Delivery/QA 事实判定尚未完成。若 run_skill_command 返回多个候选，只能调用 select_deliverable_candidate 选择 candidate_id；不得提交路径或自行复制文件。decision=" + string(data), nil
}

type toolExecutionResult struct {
	ID      string
	Name    string
	Content string
}

func (e *ReactLoopEngine) executeToolCalls(ctx context.Context, rc *runtime.RunContext, calls []domain.ToolCall, log logger.Logger) ([]toolExecutionResult, error) {
	// 先做 CollisionGuard 改写，再判定 Skill 独占轮，确保 office-ppt 等同轮误调用也能走 Skill 路径。
	calls = e.rewriteSkillCollisions(ctx, calls, log)

	if skillIndex := indexOfLoadSkill(calls); skillIndex >= 0 && len(calls) > 1 {
		out := make([]toolExecutionResult, len(calls))
		for i, call := range calls {
			out[i] = toolExecutionResult{ID: call.ID, Name: call.Function.Name, Content: "跳过：Skill 加载必须独占本轮；注入完成后请在下一轮再调用其他工具。"}
		}
		result, err := e.executeOneToolCall(ctx, rc, calls[skillIndex], log)
		out[skillIndex] = result
		return out, err
	}

	executionCtx, cancelExecutions := context.WithCancel(ctx)
	defer cancelExecutions()
	tasks := make([]scheduler.Task, 0, len(calls))
	for _, tc := range calls {
		tc := tc
		traits := tool.TraitsOf(nil)
		if registered := e.registry.Get(strings.TrimSpace(tc.Function.Name)); registered != nil {
			traits = tool.TraitsOf(registered.GetInfo())
		}
		tasks = append(tasks, scheduler.Task{ID: tc.ID, Name: tc.Function.Name, Params: tc.Function.Arguments, Traits: traits, Run: func(taskCtx context.Context) (string, error) {
			result, err := e.runToolCall(taskCtx, rc, tc, log)
			if errors.Is(err, approvalcontract.ErrRunAborted) || errors.Is(err, context.Canceled) {
				cancelExecutions()
			}
			return result, err
		}})
	}
	results := scheduler.NewQueue().Run(executionCtx, tasks)
	argsByID := map[string]string{}
	for _, tc := range calls {
		argsByID[tc.ID] = tc.Function.Arguments
	}
	out := make([]toolExecutionResult, 0, len(results))
	for _, result := range results {
		if errors.Is(result.Err, approvalcontract.ErrRunAborted) || errors.Is(result.Err, context.Canceled) {
			return nil, result.Err
		}
		content := result.Output
		if result.Err != nil {
			content = toolFailureContent(result.Output, result.Err)
		}
		if err := recordSkillQAEvidence(ctx, rc, result.Name, argsByID[result.ID], content); err != nil {
			return nil, err
		}
		content = annotateSkillFollowHints(rc, result.Name, argsByID[result.ID], content)
		out = append(out, toolExecutionResult{ID: result.ID, Name: result.Name, Content: content})
	}
	return out, nil
}

// toolFailureContent 对齐 Codex/Kode：失败时仍把工具 stdout/JSON 交给模型，禁止只回 error 摘要。
func toolFailureContent(output string, err error) string {
	summary := "工具执行失败"
	if err != nil {
		summary = fmt.Sprintf("工具执行失败: %s", err.Error())
		if inferFailureKindFromError(err.Error()) == "tool_arguments_truncated" {
			summary += "\n参数 JSON 在传输完成前被截断。不要原样重试大型结构化调用；请缩小单次改动、拆分模块，或改用支持 freeform/增量输入的编辑工具。"
		}
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return summary
	}
	return output + "\n" + summary
}

// rewriteSkillCollisions 将误把 skill 名当 tool 的调用改写为 Skill 网关（默认开启）。
func (e *ReactLoopEngine) rewriteSkillCollisions(ctx context.Context, calls []domain.ToolCall, log logger.Logger) []domain.ToolCall {
	if e.skillNameMatcher == nil || !e.shouldAutoRewriteSkillCollision() {
		return calls
	}
	out := make([]domain.ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		name := strings.TrimSpace(out[i].Function.Name)
		if name == "" || name == "Skill" {
			continue
		}
		if e.registry.Get(name) != nil || isToolRegistered(e.registry, name) {
			continue
		}
		canonical, ok, err := e.skillNameMatcher.Match(ctx, out[i].Function.Name)
		if err != nil || !ok {
			continue
		}
		log.Warn("Skill名被当作Tool调用，已同轮改写为Skill网关", "requested", out[i].Function.Name, "canonical", canonical, "call_id", out[i].ID)
		out[i].Function.Name = "Skill"
		out[i].Function.Arguments = skillcollision.RewriteArgs(canonical, out[i].Function.Arguments)
	}
	return out
}

func (e *ReactLoopEngine) executeOneToolCall(ctx context.Context, rc *runtime.RunContext, tc domain.ToolCall, log logger.Logger) (toolExecutionResult, error) {
	content, err := e.runToolCall(ctx, rc, tc, log)
	if errors.Is(err, approvalcontract.ErrRunAborted) || errors.Is(err, context.Canceled) {
		return toolExecutionResult{ID: tc.ID, Name: tc.Function.Name}, err
	}
	if err != nil {
		content = toolFailureContent(content, err)
	}
	if evidenceErr := recordSkillQAEvidence(ctx, rc, tc.Function.Name, tc.Function.Arguments, content); evidenceErr != nil {
		return toolExecutionResult{ID: tc.ID, Name: tc.Function.Name}, evidenceErr
	}
	content = annotateSkillFollowHints(rc, tc.Function.Name, tc.Function.Arguments, content)
	return toolExecutionResult{ID: tc.ID, Name: tc.Function.Name, Content: content}, nil
}

func recordSkillQAEvidence(ctx context.Context, rc *runtime.RunContext, toolName, args, content string) error {
	if strings.TrimSpace(toolName) != "run_skill_command" || rc == nil || rc.SkillFollow == nil || !toolResultOK(content) {
		return nil
	}
	command := extractCommandArg(args)
	if command == "" || !rc.SkillFollow.IsQACommand(command) {
		return nil
	}
	recorder, ok := artifactcontract.QAEvidenceRecorderFromContext(ctx)
	if !ok {
		return nil
	}
	prepared, ok := workcontract.PreparedRunFromContext(ctx)
	if !ok {
		return fmt.Errorf("QA evidence recorder 已配置但缺少 prepared run")
	}
	digest := sha256.Sum256([]byte(command))
	return recorder.RecordPassed(ctx, artifactcontract.QAPassRequest{TenantID: prepared.Manifest.Scope.TenantID, RunID: prepared.Manifest.RunID, Validator: "skill-command:sha256:" + fmt.Sprintf("%x", digest[:])})
}

func (e *ReactLoopEngine) runToolCall(ctx context.Context, rc *runtime.RunContext, tc domain.ToolCall, log logger.Logger) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	runID := rc.Run.ID
	stepIdx := rc.Iteration
	displayTrue := true
	toolName := strings.TrimSpace(tc.Function.Name)
	if toolName == "run_skill_command" && rc != nil && rc.SkillFollow != nil {
		command := extractCommandArg(tc.Function.Arguments)
		if rc.SkillFollow.ShouldBlockQA(command) {
			log.Warn("Harness 已阻止重复 QA 环境探测", "command", truncate(command, 160))
			return `{"ok":false,"failure_kind":"qa_unavailable","suggested_action":"publish_and_finish_incomplete","error":"QA 环境失败预算已耗尽；禁止搜索宿主或其他插件路径重试"}`, nil
		}
	}
	if toolName == "Task" {
		// 在执行 Task 前固定当前父 Run 状态；Builder 会丢弃当前 Task 工具调用及全部工具轨迹。
		ctx = contextsnapshot.WithParentSnapshot(ctx, rc.Messages, tc.ID)
	}

	toolLog := log.With("tool", toolName, "call_id", tc.ID)
	toolLog.Info("执行工具调用", "args", summarizeToolArgumentsForLog(tc.Function.Arguments))

	// CollisionGuard（不改写路径）：在 Profile 拒绝文案之前识别 skill 名误调用。
	// 默认 auto_rewrite 已在 executeToolCalls 前置完成；此处仅处理关闭改写时的结构化纠错。
	if e.registry.Get(toolName) == nil && !isToolRegistered(e.registry, toolName) && e.skillNameMatcher != nil {
		if canonical, ok, matchErr := e.skillNameMatcher.Match(ctx, tc.Function.Name); matchErr == nil && ok {
			payload := skillcollision.FormatResult(tc.Function.Name, canonical)
			toolLog.Warn("Skill名被当作Tool调用", "requested", tc.Function.Name, "canonical", canonical)
			return payload, nil
		}
	}

	// 1. Tool Input Block
	inputBlockIdx := rc.NextBlockIndex()
	progress.Emit(ctx, progress.Event{
		Kind:       progress.KindTool,
		Phase:      progress.PhaseStart,
		RunID:      runID,
		CallID:     tc.ID,
		Name:       toolName,
		Summary:    "调用工具: " + toolName,
		Detail:     truncate(tc.Function.Arguments, 240),
		Component:  "tool",
		BlockIndex: &inputBlockIdx,
		BlockType:  "tool_input",
		StepIndex:  &stepIdx,
		Display:    &displayTrue,
	})

	// 非流式下 input 立即完成；该 complete 仅闭合 tool_input block，不面向用户展示
	//（否则会与上方带参数的「调用工具」摘要重复）。
	displayFalse := false
	progress.Emit(ctx, progress.Event{
		Kind:       progress.KindTool,
		Phase:      progress.PhaseComplete,
		RunID:      runID,
		CallID:     tc.ID,
		Name:       toolName,
		Summary:    "调用工具: " + toolName,
		Component:  "tool",
		BlockIndex: &inputBlockIdx,
		BlockType:  "tool_input",
		StepIndex:  &stepIdx,
		Display:    &displayFalse,
		StopReason: "complete",
	})

	// 2. Tool Result Block
	resultBlockIdx := rc.NextBlockIndex()
	progress.Emit(ctx, progress.Event{
		Kind:       progress.KindTool,
		Phase:      progress.PhaseStart,
		RunID:      runID,
		CallID:     tc.ID,
		Name:       toolName,
		Summary:    "执行工具: " + toolName,
		Component:  "tool",
		BlockIndex: &resultBlockIdx,
		BlockType:  "tool_result",
		StepIndex:  &stepIdx,
		Display:    &displayTrue,
	})

	toolSpanID := fmt.Sprintf("%s-tool-%s", runID, tc.ID)
	toolSpan := e.tracer.StartSpan(ctx, "tool:"+toolName, toolSpanID)

	// Repeat Guard L1：同调用身份连续失败达阈值后硬拦截（不 Execute、不入账）。
	if rc.RepeatGuard != nil {
		check := rc.RepeatGuard.Check(toolName, tc.Function.Arguments, nil)
		if check.Blocked {
			toolLog.Warn("Repeat Guard 拦截重复失败",
				"failure_kind", check.FailureKind,
				"call_key_prefix", check.Identity.KeyPrefix,
			)
			e.tracer.EndSpan(ctx, toolSpan, nil)
			progress.Emit(ctx, progress.Event{
				Kind:       progress.KindTool,
				Phase:      progress.PhaseError,
				Level:      progress.LevelWarn,
				RunID:      runID,
				CallID:     tc.ID,
				Name:       toolName,
				Summary:    "已拦截重复失败: " + toolName,
				Detail:     truncate(check.Content, 240),
				Component:  "tool",
				BlockIndex: &resultBlockIdx,
				BlockType:  "tool_result",
				StepIndex:  &stepIdx,
				Display:    &displayTrue,
				StopReason: check.FailureKind,
				Metadata: map[string]string{
					"failure_kind":    check.FailureKind,
					"call_key_prefix": check.Identity.KeyPrefix,
				},
			})
			return check.Content, nil
		}
	}

	result, toolErr := e.registry.Execute(ctx, toolName, tc.Function.Arguments)
	var outcome repeatguard.Outcome
	if rc.RepeatGuard != nil {
		outcome = rc.RepeatGuard.Record(toolName, tc.Function.Arguments, result, toolErr, nil)
	} else {
		outcome = repeatguard.ParseOutcome(toolName, result, toolErr)
	}
	if toolErr != nil || !outcome.Success {
		kind, stdout, stderr := extractToolFailureLogFields(result, toolErr)
		if kind == "" || kind == "tool_error" {
			if outcome.FailureKind != "" {
				kind = outcome.FailureKind
			}
		}
		errMsg := ""
		if toolErr != nil {
			errMsg = toolErr.Error()
		} else if outcome.ErrorExcerpt != "" {
			errMsg = outcome.ErrorExcerpt
		} else {
			errMsg = "ok=false"
		}
		toolLog.Warn("工具执行失败",
			"error", errMsg,
			"failure_kind", kind,
			"stdout", summarizeToolOutputForLog(stdout, 500),
			"stderr", summarizeToolOutputForLog(stderr, 500),
		)
		e.tracer.EndSpan(ctx, toolSpan, toolErr)
		detail := errMsg
		if trimmed := strings.TrimSpace(result); trimmed != "" {
			detail = trimmed
		}
		progress.Emit(ctx, progress.Event{
			Kind:       progress.KindTool,
			Phase:      progress.PhaseError,
			Level:      progress.LevelError,
			RunID:      runID,
			CallID:     tc.ID,
			Name:       tc.Function.Name,
			Summary:    "工具执行失败: " + tc.Function.Name,
			Detail:     truncate(detail, 240),
			Component:  "tool",
			BlockIndex: &resultBlockIdx,
			BlockType:  "tool_result",
			StepIndex:  &stepIdx,
			Display:    &displayTrue,
			StopReason: "error",
			Metadata:   toolTimingMetadata(toolName, result),
		})
		// 保留 result JSON（如 ok=false + failure_kind），对齐 Codex RespondToModel(content)。
		return result, toolErr
	}
	// 日志仅保存 result 的脱敏摘要；完整结果仍返回模型，避免影响任务执行。
	toolLog.Info("工具执行成功", "result", summarizeToolResultForLog(result))
	e.tracer.EndSpan(ctx, toolSpan, nil)
	progress.Emit(ctx, progress.Event{
		Kind:       progress.KindTool,
		Phase:      progress.PhaseComplete,
		RunID:      runID,
		CallID:     tc.ID,
		Name:       tc.Function.Name,
		Summary:    "工具执行完成: " + tc.Function.Name,
		Detail:     truncate(result, 240),
		Component:  "tool",
		BlockIndex: &resultBlockIdx,
		BlockType:  "tool_result",
		StepIndex:  &stepIdx,
		Display:    &displayTrue,
		StopReason: "complete",
		Metadata:   toolTimingMetadata(toolName, result),
	})
	return result, nil
}

func toolTimingMetadata(toolName, result string) map[string]string {
	if toolName != "run_skill_command" {
		return nil
	}
	var payload map[string]any
	if !decodeFirstJSONObject(result, &payload) {
		return nil
	}
	keys := []string{"duration_ms", "approval_duration_ms", "staging_duration_ms", "execution_duration_ms"}
	metadata := make(map[string]string, len(keys))
	for _, key := range keys {
		value, ok := payload[key].(float64)
		if !ok || value < 0 {
			continue
		}
		metadata[key] = strconv.FormatInt(int64(value), 10)
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

// applyRepeatGuardProgress 评估 L2 进展门禁；若 partial_complete 则结束 Run（err=nil）。
func (e *ReactLoopEngine) applyRepeatGuardProgress(ctx context.Context, rc *runtime.RunContext, log logger.Logger, finalAnswer bool) (stop bool, err error) {
	if rc == nil || rc.RepeatGuard == nil {
		return false, nil
	}
	dec := rc.RepeatGuard.EndIteration(rc.Iteration, finalAnswer)
	if dec.InjectNoProgress {
		log.Warn("Repeat Guard 无进展",
			"failure_kind", "no_progress",
			"stagnant_iterations", dec.StagnantIterations,
		)
		progress.Emit(ctx, progress.Event{
			Kind:    progress.KindRun,
			Phase:   progress.PhaseProgress,
			Level:   progress.LevelWarn,
			RunID:   rc.Run.ID,
			Summary: "运行无进展，请更换策略",
			Detail:  truncate(dec.NoProgressJSON, 240),
			Metadata: map[string]string{
				"failure_kind":        "no_progress",
				"stagnant_iterations": fmt.Sprintf("%d", dec.StagnantIterations),
			},
		})
		rc.Messages = append(rc.Messages, domain.NewSystemMessage(
			"<repeat_guard>\n"+dec.NoProgressJSON+"\n</repeat_guard>",
		).WithSource(domain.MessageSourceRepeatGuard))
		return false, nil
	}
	if dec.PartialComplete {
		log.Warn("Repeat Guard partial_complete",
			"stagnant_iterations", dec.StagnantIterations,
		)
		progress.Emit(ctx, progress.Event{
			Kind:    progress.KindRun,
			Phase:   progress.PhaseComplete,
			Level:   progress.LevelWarn,
			RunID:   rc.Run.ID,
			Summary: "无进展，已 partial_complete",
			Detail:  dec.PartialCompleteMsg,
			Metadata: map[string]string{
				"incomplete":          "true",
				"failure_kind":        "no_progress",
				"stagnant_iterations": fmt.Sprintf("%d", dec.StagnantIterations),
			},
		})
		if strings.TrimSpace(rc.Run.FinalAnswer) == "" {
			rc.Run.FinalAnswer = dec.PartialCompleteMsg
		}
		rc.Run.Incomplete = true
		rc.Run.Status = domain.RunStatusCompleted
		// 将 partial 结论写入消息链，便于短期记忆恢复工具轨迹 + 结论
		if !lastMessageIsAssistantWithContent(rc.Messages, rc.Run.FinalAnswer) {
			rc.Messages = append(rc.Messages, domain.NewAssistantMessage(rc.Run.FinalAnswer))
		}
		return true, nil
	}
	return false, nil
}

func lastMessageIsAssistantWithContent(msgs []*domain.Message, content string) bool {
	if len(msgs) == 0 || strings.TrimSpace(content) == "" {
		return false
	}
	last := msgs[len(msgs)-1]
	return last != nil && last.Role == domain.RoleAssistant && last.Content == content
}

// countAssembledHistoryLen 计算 Assemble 之后、本轮 user_turn 之前的历史消息条数。
// 布局： [system?] + [history…] + [user_turn] + [reminder?]
func countAssembledHistoryLen(msgs []*domain.Message) int {
	if len(msgs) == 0 {
		return 0
	}
	start := 0
	if msgs[0] != nil && msgs[0].Role == domain.RoleSystem && msgs[0].NormalizedKind() == domain.MessageKindSystem {
		start = 1
	}
	end := len(msgs)
	if end > start && msgs[end-1] != nil && msgs[end-1].NormalizedKind() == domain.MessageKindReminder {
		end--
	}
	if end > start && msgs[end-1] != nil && msgs[end-1].NormalizedKind() == domain.MessageKindUserTurn {
		end--
	}
	if end < start {
		return 0
	}
	return end - start
}

// persistRunSessionMessages 将本 Run 新增的完整消息链追加到短期记忆。
// runErr 非空且非 Incomplete 时，追加一条 reminder 中断标记，供下一轮续跑感知。
func (e *ReactLoopEngine) persistRunSessionMessages(ctx context.Context, sessionID string, rc *runtime.RunContext, historyLen int, runErr error, log logger.Logger) {
	if e.memory == nil || rc == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	if runErr != nil && (rc.Run == nil || !rc.Run.Incomplete) {
		e.appendRunInterruptMarker(rc, runErr)
	}
	toSave := domain.SessionMessagesFromRun(rc.Messages, historyLen)
	if len(toSave) == 0 {
		return
	}
	ref := memory.SessionRef{SessionID: sessionID}
	if sessionRef, ok := memory.SessionRefFromCtx(ctx); ok {
		ref = sessionRef
	}
	if err := e.memory.Append(ctx, ref, toSave); err != nil {
		log.Warn("保存对话历史失败", "error", err, "message_count", len(toSave))
		return
	}
	log.Info("已保存本 Run 完整消息链", "message_count", len(toSave), "history_len", historyLen, "interrupted", runErr != nil && (rc.Run == nil || !rc.Run.Incomplete))
}

// appendRunInterruptMarker 在消息链末尾写入运行中断 reminder（ForModel 可见，ForUI 默认隐藏）。
func (e *ReactLoopEngine) appendRunInterruptMarker(rc *runtime.RunContext, runErr error) {
	if rc == nil || runErr == nil {
		return
	}
	detail := strings.TrimSpace(runErr.Error())
	if detail == "" {
		detail = "unknown error"
	}
	if len([]rune(detail)) > 500 {
		detail = string([]rune(detail)[:500]) + "…"
	}
	marker := domain.NewReminderMessage("上一次运行中断：" + detail)
	marker.Source = domain.MessageSourceRunEngine
	if len(rc.Messages) > 0 {
		last := rc.Messages[len(rc.Messages)-1]
		if last != nil && last.NormalizedKind() == domain.MessageKindReminder &&
			last.Source == domain.MessageSourceRunEngine &&
			strings.HasPrefix(last.Content, "上一次运行中断：") {
			return
		}
	}
	rc.Messages = append(rc.Messages, marker)
}

func indexOfLoadSkill(calls []domain.ToolCall) int {
	for i, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "Skill" {
			return i
		}
	}
	return -1
}

func isToolRegistered(registry tool.Registry, name string) bool {
	if checker, ok := registry.(interface{ IsRegistered(string) bool }); ok {
		return checker.IsRegistered(name)
	}
	return false
}

// ==================== 辅助函数 ====================

// getToolInfos 根据Agent配置获取可用工具列表
func (e *ReactLoopEngine) getToolInfos(agent *domain.Agent) []*tool.Info {
	ctx := context.Background()
	if len(agent.Tools) == 0 {
		if withCtx, ok := e.registry.(interface {
			ListInfosContext(context.Context) []*tool.Info
		}); ok {
			return withCtx.ListInfosContext(ctx)
		}
		return e.registry.ListInfos()
	}
	names := make([]string, 0, len(agent.Tools))
	for _, ref := range agent.Tools {
		names = append(names, ref.Name)
	}
	return e.filterToolInfos(ctx, names)
}

func (e *ReactLoopEngine) filterToolInfos(ctx context.Context, names []string) []*tool.Info {
	if withCtx, ok := e.registry.(interface {
		FilterInfosContext(context.Context, []string) []*tool.Info
	}); ok {
		return withCtx.FilterInfosContext(ctx, names)
	}
	return filterToolInfosByName(e.registry.ListInfos(), names)
}

// extractToolFailureLogFields 从工具 JSON 结果提取 failure_kind / stdout / stderr，便于 agent.log 排障。
// 若 JSON 无 failure_kind，则从 error 文案推导常见 kind。
func extractToolFailureLogFields(result string, toolErr error) (kind, stdout, stderr string) {
	trimmed := strings.TrimSpace(result)
	if trimmed != "" {
		var payload map[string]any
		if json.Unmarshal([]byte(trimmed), &payload) == nil {
			kind = stringField(payload, "failure_kind")
			stdout = stringField(payload, "stdout")
			stderr = stringField(payload, "stderr")
			if stdout == "" {
				if msg := stringField(payload, "message"); msg != "" {
					stdout = msg
				}
			}
		}
		if stdout == "" && !strings.HasPrefix(trimmed, "{") {
			stdout = trimmed
		}
	}
	if kind == "" && toolErr != nil {
		kind = inferFailureKindFromError(toolErr.Error())
	}
	if kind == "" {
		kind = "tool_error"
	}
	return kind, stdout, stderr
}

// summarizeToolResultForLog 构造仅用于日志的 ToolResult 副本。
// stdout/stderr 可能包含用户文档、表格或命令输出，不能把完整内容写入 agent.log。
// 保留字节数和 SHA-256 以便关联排障；原始 result 不会被修改，仍完整交给模型。
func summarizeToolResultForLog(result string) string {
	var payload any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return summarizeToolOutputForLog(result, 0)
	}
	summarizeToolPayloadFields(payload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "[工具结果日志序列化失败]"
	}
	return string(encoded)
}

// summarizeToolArgumentsForLog 避免把待写文件、补丁或其他正文载荷写入 agent.log。
// 非法/截断 JSON 无法安全区分元数据与正文，因此整段只保留长度和哈希。
func summarizeToolArgumentsForLog(arguments string) string {
	var payload any
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return summarizeToolOutputForLog(arguments, 0)
	}
	summarizeToolPayloadFields(payload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "[工具参数日志序列化失败]"
	}
	return string(encoded)
}

func summarizeToolPayloadFields(value any) {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			switch strings.ToLower(key) {
			case "content", "patch", "stdout", "stderr", "payload", "data":
				if text, ok := item.(string); ok {
					current[key] = summarizeToolOutputForLog(text, 0)
				}
			default:
				summarizeToolPayloadFields(item)
			}
		}
	case []any:
		for _, item := range current {
			summarizeToolPayloadFields(item)
		}
	}
}

// summarizeToolOutputForLog 对 stdout/stderr 做凭据脱敏；previewLimit 为 0 时不保留正文。
func summarizeToolOutputForLog(value string, previewLimit int) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	cleaned, err := (sanitize.Default{}).Sanitize(value)
	if err != nil {
		return fmt.Sprintf("[日志输出已省略: bytes=%d sha256=%x reason=invalid_text]", len(value), sum)
	}
	if previewLimit > 0 {
		preview := truncate(cleaned, previewLimit)
		return fmt.Sprintf("%s [日志已截断: bytes=%d sha256=%x]", preview, len(value), sum)
	}
	return fmt.Sprintf("[日志输出已省略: bytes=%d sha256=%x]", len(value), sum)
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func inferFailureKindFromError(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "unexpected eof"), strings.Contains(lower, "unexpected end of json input"), strings.Contains(lower, "unterminated string"):
		return "tool_arguments_truncated"
	case strings.Contains(lower, "execution_path_contract_violation"), strings.Contains(lower, "path_contract"):
		return "path_contract_violation"
	case strings.Contains(lower, "dependency_missing"):
		return "dependency_missing"
	case strings.Contains(lower, "approval"):
		return "approval_denied"
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "deadline exceeded"):
		return "timeout"
	case strings.Contains(lower, "sandbox"):
		return "sandbox_violation"
	case strings.Contains(lower, "冒充"), strings.Contains(lower, "artifact"):
		return "artifact_invalid"
	default:
		return ""
	}
}

// truncate 截断字符串，用于日志展示
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

type skillInjectionOutput struct {
	Type          string   `json:"type"`
	QualifiedName string   `json:"qualified_name"`
	Resource      string   `json:"resource"`
	Content       string   `json:"content"`
	AllowedTools  []string `json:"allowed_tools"`
	Truncated     bool     `json:"truncated"`
}

func parseSkillInjection(result toolExecutionResult) (skillInjectionOutput, bool) {
	name := strings.TrimSpace(result.Name)
	if name != "Skill" || result.Content == "" {
		return skillInjectionOutput{}, false
	}
	var out skillInjectionOutput
	if err := json.Unmarshal([]byte(result.Content), &out); err != nil {
		return skillInjectionOutput{}, false
	}
	return out, out.Type == "skill_injection" && out.Content != ""
}

func renderSkillToolAck(injection skillInjectionOutput, narrowOK bool) string {
	msg := "Skill loaded. Follow <skill_injection> instructions and use primitive tools."
	if !narrowOK {
		msg = "Skill loaded, but allowed_tools intersected with the current visible tool set is empty; tool visibility was not narrowed. Fix the skill allowed-tools declaration."
	}
	payload := map[string]any{
		"type":           "skill_loaded",
		"qualified_name": injection.QualifiedName,
		"truncated":      injection.Truncated,
		"allowed_tools":  injection.AllowedTools,
		"message":        msg,
	}
	if !narrowOK {
		payload["narrow_failed"] = true
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"type":"skill_loaded"}`
	}
	return string(data)
}

func renderSkillInjection(injection skillInjectionOutput) string {
	var sb strings.Builder
	sb.WriteString("<skill_injection>\n")
	sb.WriteString("Skill: ")
	sb.WriteString(injection.QualifiedName)
	sb.WriteString("\n\n")
	sb.WriteString(injection.Content)
	sb.WriteString(renderSkillScriptBridge(injection))
	if injection.Truncated {
		sb.WriteString("\n\n[skill上下文已截断：开始写脚本或跑命令前，必须先用 read_skill_resource/search_skill_resources 读齐文中链接的 .md 参考，并完成 QA Required 步骤]")
	}
	sb.WriteString("\n</skill_injection>")
	return sb.String()
}

func renderSkillScriptBridge(injection skillInjectionOutput) string {
	name := strings.TrimSpace(injection.QualifiedName)
	if name == "" {
		return ""
	}
	return fmt.Sprintf(`

<skill_runtime_bridge>
Skill Markdown 可移植，不必含 Genesis 工具名；适配由本 bridge 完成，不要改写第三方 SKILL.md / references。
执行：文档中的 shell/脚本命令（python/python3/node/python -m 等）一律 run_skill_command(skill=%q, command=原文)。解释器跟文档（.js/require→node；python/-m→python），勿把 Node 包装成 python -m。运行时会 materialize 完整 Skill 包到工作目录后再执行。
写与 stage：刚 write_file("$WORK_DIR/...") 的下一跳 run_skill_command **必须**带 inputs=["$WORK_DIR/..."]（漏传会 input_binding_missing）；command 只用 stage 后相对名。用户指定的宿主路径可直接进 inputs（勿 run_command 搬运）。禁止把 /workspace 等执行面路径写入 inputs/write_file.path；禁止把 $WORK_DIR/$INPUT_DIR/$OUTPUT_DIR/$TMPDIR/$SKILL_DIR 写进 command（不会展开）。仅跑包内 scripts/ 或文件已在技能 cwd 时可省略 inputs。大脚本可拆分多次 write_file，或 append=true + expected_hash；工具参数截断后勿原样重试。
禁止多行/长串 python -c、node -e/--eval（本地与远程 shell 引号均易失败）；仅极短单行探测。依赖：勿用 run_skill_command 跑 npm/pip install（含 SKILL 里的安装示例行）；靠 dependencies.runtime / profile，缺包看 dependency_missing 再用 install_skill_dependencies，勿先写探测脚本、勿全局装包绕过声明。
产物与交付：produced[] 只有 Harness 的 candidate_id/name；唯一 required 候选自动发布，多候选只用 select_deliverable_candidate。禁止提交路径/locator，禁止 write_file 伪造办公二进制。最终以 Publication/Delivery 为准，勿把 runs 或 $OUTPUT_DIR 当作用户已交付。Skill 命令产出按 SKILL 写相对 cwd；元数据 execution_backend 标明执行面——remote_sandbox/remote_session 时宿主 glob/list_dir/walk_dir/read_file 看不到技能 cwd 文件；run_skill_command 刚列出的文件须继续用 run_skill_command 查看/QA，禁止改用宿主 read_file。
SKILL 要求先 Read 的链接与 QA 命令须执行；用 grep/rg 检测「不应出现」的文本时，exit_code=1 且空 stderr 视为未命中/通过，勿当脚本崩溃反复重试。因 dependency_missing/sandbox_unavailable/unsupported_environment 失败时如实报告缺口，勿换绝对路径或搜用户目录硬扛。
</skill_runtime_bridge>`, name)
}
func namesOfToolInfos(infos []*tool.Info) []string {
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		if info != nil && info.Name != "" {
			out = append(out, strings.TrimSpace(info.Name))
		}
	}
	return out
}

// narrowToolNames 按 skill.allowed_tools 收窄当前可见工具集。
// allowed 为空：不收窄。非空：与 current 求交，并始终并入 Skill；若 current 已含资源元工具则一并保留。
// 求交结果为空时 ok=false，调用方不得静默回退为全量工具集。
func narrowToolNames(current []string, allowed []string) (names []string, ok bool) {
	if len(allowed) == 0 {
		return current, true
	}
	currentSet := map[string]struct{}{}
	for _, name := range current {
		name = strings.TrimSpace(name)
		if name != "" {
			currentSet[name] = struct{}{}
		}
	}
	allowedSet := map[string]struct{}{}
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name != "" {
			allowedSet[name] = struct{}{}
		}
	}
	// 网关与资源元工具：收窄后仍应可用（仅当当前 turn 本来就可见）。
	// 网关 / 资源 / 脚本 / 依赖安装：收窄后仍应可用（仅当当前 turn 本来就可见）。
	// install_skill_dependencies 是缺包闭环必需；对齐设计 §7。
	for _, meta := range []string{"Skill", "list_skill_resources", "read_skill_resource", "search_skill_resources", "run_skill_command", "install_skill_dependencies"} {
		if _, inCurrent := currentSet[meta]; inCurrent {
			allowedSet[meta] = struct{}{}
		}
	}
	out := make([]string, 0, len(current))
	seen := map[string]struct{}{}
	for _, name := range current {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, allow := allowedSet[name]; !allow {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func filterToolInfosByName(infos []*tool.Info, names []string) []*tool.Info {
	allowed := map[string]struct{}{}
	for _, name := range names {
		allowed[strings.TrimSpace(name)] = struct{}{}
	}
	out := make([]*tool.Info, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		if _, ok := allowed[strings.TrimSpace(info.Name)]; ok {
			out = append(out, info)
		}
	}
	return out
}
