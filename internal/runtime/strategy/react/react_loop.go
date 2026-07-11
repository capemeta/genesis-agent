// package react - ReAct Loop策略实现
// ReAct (Reason + Act)：每轮循环中LLM先推理(Think)，再决定行动(Act/工具调用)，直到给出最终答案
// 对应 AGENTS.md §4.2 ReAct Loop执行流程
//
// 重要：本文件不依赖任何 eino 包，所有 LLM 操作通过 llm.ChatModel 接口进行，
// eino 细节完全封装在 llm 包的适配层中
package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/llm/contract"
	"genesis-agent/internal/capabilities/memory/contract"
	skillcollision "genesis-agent/internal/capabilities/skill/collision"
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/tool/scheduler"
	"genesis-agent/internal/capabilities/trace/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
	"genesis-agent/internal/runtime/progress"
	"genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/repeatguard"
)

// SkillNameMatcher 识别「把 Skill 名当成 Tool 名」的误调用（由产品注入，Gateway 不依赖 skill）。
type SkillNameMatcher interface {
	Match(ctx context.Context, name string) (canonical string, ok bool, err error)
}

// ReactLoopEngine ReAct Loop策略的RunEngine实现
type ReactLoopEngine struct {
	llm                       llm.ChatModel
	registry                  tool.Registry
	memory                    memory.ShortTermStore
	prompt                    prompt.Builder
	logger                    logger.Logger
	tracer                    trace.Tracer
	skillNameMatcher          SkillNameMatcher
	skillMentionSelector      SkillMentionSelector
	skillExplicitLoader       SkillExplicitLoader
	autoRewriteSkillCollision *bool // nil 表示默认开启
}

// EngineOption 可选依赖。
type EngineOption func(*ReactLoopEngine)

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
	store memory.ShortTermStore,
	promptBuilder prompt.Builder,
	log logger.Logger,
	tracer trace.Tracer,
	opts ...EngineOption,
) *ReactLoopEngine {
	e := &ReactLoopEngine{
		llm:      llmClient,
		registry: registry,
		memory:   store,
		prompt:   promptBuilder,
		logger:   log,
		tracer:   tracer,
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
	run := &domain.Run{
		ID:        newRunID(),
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
func (e *ReactLoopEngine) loop(ctx context.Context, rc *runtime.RunContext, req domain.StartRunRequest, log logger.Logger) error {
	agent := req.Agent
	maxIter := agent.RuntimePolicy.MaxIterations
	if maxIter <= 0 {
		maxIter = 50
	}

	// ── 步骤1: 获取工具定义（tool.Info，与外部框架无关）─────────
	toolInfos := e.getToolInfos(agent)
	activeToolNames := namesOfToolInfos(toolInfos)

	// ── 步骤2: 构建初始消息列表 ───────────────────────────────
	// System消息（注入提示词 + 当前时间）
	systemPrompt, err := e.prompt.BuildSystem(ctx, prompt.BuildRequest{Agent: agent, Run: rc.Run})
	if err != nil {
		return err
	}
	rc.Messages = append(rc.Messages, domain.NewSystemMessage(systemPrompt))

	// 加载Session历史对话
	history, err := e.memory.GetHistory(ctx, req.SessionID)
	if err != nil {
		log.Warn("加载历史记录失败，将从空历史开始", "error", err)
	}
	rc.Messages = append(rc.Messages, history...)

	// 本轮用户输入
	rc.Messages = append(rc.Messages, domain.NewUserMessage(req.UserInput))

	// mention 自动注入（在首轮 LLM 前；走 Skill 网关，含 Approval / 去重 / 收窄）
	e.injectMentionedSkills(ctx, rc, req.UserInput, &activeToolNames, &toolInfos, log)

	// 打印已加载工具
	toolNames := make([]string, 0, len(toolInfos))
	for _, t := range toolInfos {
		toolNames = append(toolNames, t.Name)
	}
	log.Info("准备执行Loop", "max_iterations", maxIter, "tools", strings.Join(toolNames, ","))

	// ── 步骤3: 主循环 ─────────────────────────────────────────
	for rc.Iteration = 0; rc.Iteration < maxIter; rc.Iteration++ {
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
				// CoT / reasoning：对用户可见的中间思考
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
			// 工具前正文也是中间思考：用 assistant_draft 展示，避免与最终回答块混淆。
			// 最终回答在确认无 tool_calls 后另发 final_answer。
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

		llmResp, err := e.llm.StreamGenerate(ctx, rc.Messages, toolInfos, onDelta)
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

		// ── 分支判断 ─────────────────────────────────────────
		if len(llmResp.ToolCalls) > 0 {

			// ── 路径A: 执行工具调用 ──────────────────────────
			step.ActionType = domain.ActionTypeToolCall
			rc.Messages = append(rc.Messages, llmResp)

			if llmResp.Content != "" {
				iterLog.Info("LLM思考内容", "thought", llmResp.Content)
			}

			toolResults := e.executeToolCalls(ctx, rc, llmResp.ToolCalls, iterLog)
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
			iterLog.Info("获得最终回答", "content_preview", truncate(llmResp.Content, 100))
			if rc.RepeatGuard != nil {
				_ = rc.RepeatGuard.EndIteration(rc.Iteration, true)
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
			progress.Emit(ctx, progress.Event{
				Kind:       progress.KindRun,
				Phase:      progress.PhaseComplete,
				RunID:      rc.Run.ID,
				StepID:     stepID,
				BlockIndex: &answerBlockIdx,
				BlockType:  "final_answer",
				StepIndex:  &stepIdx,
				Display:    &displayTrue,
				StopReason: "complete",
			})

			rc.Run.FinalAnswer = llmResp.Content
			rc.Run.Status = domain.RunStatusCompleted

			// 保存本轮对话到Session历史（方便下次对话加载）
			saveErr := e.memory.AppendMessages(ctx, req.SessionID, []*domain.Message{
				domain.NewUserMessage(req.UserInput),
				llmResp,
			})
			if saveErr != nil {
				log.Warn("保存对话历史失败", "error", saveErr)
			}

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

type toolExecutionResult struct {
	ID      string
	Name    string
	Content string
}

func (e *ReactLoopEngine) executeToolCalls(ctx context.Context, rc *runtime.RunContext, calls []domain.ToolCall, log logger.Logger) []toolExecutionResult {
	// 先做 CollisionGuard 改写，再判定 Skill 独占轮，确保 office-ppt 等同轮误调用也能走 Skill 路径。
	calls = e.rewriteSkillCollisions(ctx, calls, log)

	if skillIndex := indexOfLoadSkill(calls); skillIndex >= 0 && len(calls) > 1 {
		out := make([]toolExecutionResult, len(calls))
		for i, call := range calls {
			out[i] = toolExecutionResult{ID: call.ID, Name: call.Function.Name, Content: "跳过：Skill 加载必须独占本轮；注入完成后请在下一轮再调用其他工具。"}
		}
		out[skillIndex] = e.executeOneToolCall(ctx, rc, calls[skillIndex], log)
		return out
	}

	tasks := make([]scheduler.Task, 0, len(calls))
	for _, tc := range calls {
		tc := tc
		traits := tool.TraitsOf(nil)
		if registered := e.registry.Get(strings.TrimSpace(tc.Function.Name)); registered != nil {
			traits = tool.TraitsOf(registered.GetInfo())
		}
		tasks = append(tasks, scheduler.Task{ID: tc.ID, Name: tc.Function.Name, Params: tc.Function.Arguments, Traits: traits, Run: func(taskCtx context.Context) (string, error) {
			return e.runToolCall(taskCtx, rc, tc, log)
		}})
	}
	results := scheduler.NewQueue().Run(ctx, tasks)
	out := make([]toolExecutionResult, 0, len(results))
	for _, result := range results {
		content := result.Output
		if result.Err != nil {
			content = toolFailureContent(result.Output, result.Err)
		}
		out = append(out, toolExecutionResult{ID: result.ID, Name: result.Name, Content: content})
	}
	return out
}

// toolFailureContent 对齐 Codex/Kode：失败时仍把工具 stdout/JSON 交给模型，禁止只回 error 摘要。
func toolFailureContent(output string, err error) string {
	summary := "工具执行失败"
	if err != nil {
		summary = fmt.Sprintf("工具执行失败: %s", err.Error())
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

func (e *ReactLoopEngine) executeOneToolCall(ctx context.Context, rc *runtime.RunContext, tc domain.ToolCall, log logger.Logger) toolExecutionResult {
	content, err := e.runToolCall(ctx, rc, tc, log)
	if err != nil {
		content = toolFailureContent(content, err)
	}
	return toolExecutionResult{ID: tc.ID, Name: tc.Function.Name, Content: content}
}

func (e *ReactLoopEngine) runToolCall(ctx context.Context, rc *runtime.RunContext, tc domain.ToolCall, log logger.Logger) (string, error) {
	runID := rc.Run.ID
	stepIdx := rc.Iteration
	displayTrue := true
	toolName := strings.TrimSpace(tc.Function.Name)

	toolLog := log.With("tool", toolName, "call_id", tc.ID)
	toolLog.Info("执行工具调用", "args", tc.Function.Arguments)

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
			"stdout", truncate(stdout, 500),
			"stderr", truncate(stderr, 500),
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
		})
		// 保留 result JSON（如 ok=false + failure_kind），对齐 Codex RespondToModel(content)。
		return result, toolErr
	}
	toolLog.Info("工具执行成功", "result", result)
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
	})
	return result, nil
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
		))
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
		return true, nil
	}
	return false, nil
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

// newRunID 生成唯一RunID（MVP阶段用纳秒时间戳）
func newRunID() string {
	return fmt.Sprintf("run-%d", time.Now().UnixNano())
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
	if injection.Truncated {
		sb.WriteString("\n\n[skill上下文已截断，必要时用 read_skill_resource/search_skill_resources 读取引用资源]")
	}
	sb.WriteString("\n</skill_injection>")
	return sb.String()
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
	for _, meta := range []string{"Skill", "list_skill_resources", "read_skill_resource", "search_skill_resources", "run_skill_script", "install_skill_dependencies"} {
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
