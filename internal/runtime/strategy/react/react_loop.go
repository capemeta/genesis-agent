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
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/capabilities/trace/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
	"genesis-agent/internal/runtime/prompt"
)

// ReactLoopEngine ReAct Loop策略的RunEngine实现
type ReactLoopEngine struct {
	llm      llm.ChatModel
	registry tool.Registry
	memory   memory.ShortTermStore
	prompt   prompt.Builder
	logger   logger.Logger
	tracer   trace.Tracer
}

// NewReactLoopEngine 创建ReAct Loop引擎，所有依赖通过构造函数注入
func NewReactLoopEngine(
	llmClient llm.ChatModel,
	registry tool.Registry,
	store memory.ShortTermStore,
	promptBuilder prompt.Builder,
	log logger.Logger,
	tracer trace.Tracer,
) *ReactLoopEngine {
	return &ReactLoopEngine{
		llm:      llmClient,
		registry: registry,
		memory:   store,
		prompt:   promptBuilder,
		logger:   log,
		tracer:   tracer,
	}
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

	// 启动根Span（覆盖整个Run生命周期）
	runSpan := e.tracer.StartSpan(ctx, "run", run.ID)

	rc := runtime.NewRunContext(run, req.Agent)
	err := e.loop(ctx, rc, req, log)

	e.tracer.EndSpan(ctx, runSpan, err)

	now := time.Now()
	run.FinishedAt = &now

	if err != nil {
		run.Status = domain.RunStatusFailed
		log.Error("Run失败", "error", err, "steps", len(run.Steps), "tokens", run.TotalTokens)
		return run, err
	}

	log.Info("Run完成", "steps", len(run.Steps), "tokens", run.TotalTokens, "answer_len", len(run.FinalAnswer))
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

	// ── 步骤2: 构建初始消息列表 ───────────────────────────────
	// System消息（注入提示词 + 当前时间）
	rc.Messages = append(rc.Messages, domain.NewSystemMessage(e.prompt.BuildSystem(agent)))

	// 加载Session历史对话
	history, err := e.memory.GetHistory(ctx, req.SessionID)
	if err != nil {
		log.Warn("加载历史记录失败，将从空历史开始", "error", err)
	}
	rc.Messages = append(rc.Messages, history...)

	// 本轮用户输入
	rc.Messages = append(rc.Messages, domain.NewUserMessage(req.UserInput))

	// 打印已加载工具
	toolNames := make([]string, 0, len(toolInfos))
	for _, t := range toolInfos {
		toolNames = append(toolNames, t.Name)
	}
	log.Info("准备执行Loop", "max_iterations", maxIter, "tools", strings.Join(toolNames, ","))

	// ── 步骤3: 主循环 ─────────────────────────────────────────
	for rc.Iteration = 0; rc.Iteration < maxIter; rc.Iteration++ {
		iterLog := log.With("iteration", rc.Iteration)

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
		llmResp, err := e.llm.Generate(ctx, rc.Messages, toolInfos)
		if err != nil {
			e.tracer.EndSpan(ctx, stepSpan, err)
			return fmt.Errorf("第%d轮LLM调用失败: %w", rc.Iteration, err)
		}
		iterLog.Debug("LLM响应", "tool_calls", len(llmResp.ToolCalls), "content_len", len(llmResp.Content))

		// ── 分支判断 ─────────────────────────────────────────
		if len(llmResp.ToolCalls) > 0 {

			// ── 路径A: 执行工具调用 ──────────────────────────
			step.ActionType = domain.ActionTypeToolCall
			rc.Messages = append(rc.Messages, llmResp)

			if llmResp.Content != "" {
				iterLog.Info("LLM思考内容", "thought", llmResp.Content)
			}

			// 执行所有工具调用
			for _, tc := range llmResp.ToolCalls {
				toolLog := iterLog.With("tool", tc.Function.Name, "call_id", tc.ID)
				toolLog.Info("执行工具调用", "args", tc.Function.Arguments)

				toolSpanID := fmt.Sprintf("%s-tool-%s", rc.Run.ID, tc.ID)
				toolSpan := e.tracer.StartSpan(ctx, "tool:"+tc.Function.Name, toolSpanID)

				result, toolErr := e.registry.Execute(ctx, tc.Function.Name, tc.Function.Arguments)

				var toolResult string
				if toolErr != nil {
					toolResult = fmt.Sprintf("工具执行失败: %s", toolErr.Error())
					toolLog.Warn("工具执行失败", "error", toolErr)
					e.tracer.EndSpan(ctx, toolSpan, toolErr)
				} else {
					toolResult = result
					toolLog.Info("工具执行成功", "result", result)
					e.tracer.EndSpan(ctx, toolSpan, nil)
				}

				// 将工具结果加入消息上下文（role=tool，关联 ToolCallID）
				rc.Messages = append(rc.Messages, domain.NewToolResultMessage(tc.ID, toolResult))
			}

			payload, _ := json.Marshal(llmResp.ToolCalls)
			step.ActionPayload = payload

		} else if llmResp.Content != "" {
			// ── 路径B: 最终回答 ──────────────────────────────
			step.ActionType = domain.ActionTypeFinalAnswer
			iterLog.Info("获得最终回答", "content_preview", truncate(llmResp.Content, 100))

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

// ==================== 辅助函数 ====================

// getToolInfos 根据Agent配置获取可用工具列表
func (e *ReactLoopEngine) getToolInfos(agent *domain.Agent) []*tool.Info {
	if len(agent.Tools) == 0 {
		// 未配置工具限制时，使用所有已注册工具
		return e.registry.ListInfos()
	}
	names := make([]string, 0, len(agent.Tools))
	for _, ref := range agent.Tools {
		names = append(names, ref.Name)
	}
	return e.registry.FilterInfos(names)
}

// newRunID 生成唯一RunID（MVP阶段用纳秒时间戳）
func newRunID() string {
	return fmt.Sprintf("run-%d", time.Now().UnixNano())
}

// truncate 截断字符串，用于日志展示
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
