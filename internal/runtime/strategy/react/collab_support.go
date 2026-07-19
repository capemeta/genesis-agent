package react

import (
	"context"
	"fmt"
	"strings"

	planmodeprompt "genesis-agent/internal/capabilities/planmode/prompt"
	"genesis-agent/internal/capabilities/llm/vision"
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
	"genesis-agent/internal/runtime/collab"
	multicontract "genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/prompt"
	"genesis-agent/internal/runtime/transcript"
)

// WithCollabStore 注入协作模式 Store。
func WithCollabStore(store collab.Store) EngineOption {
	return func(e *ReactLoopEngine) {
		e.collabStore = store
	}
}

func (e *ReactLoopEngine) prepareCollabContext(ctx context.Context, sessionID string) (context.Context, collab.Mode, bool, error) {
	if e.collabStore != nil {
		ctx = collab.WithStore(ctx, e.collabStore)
	}
	mode := collab.ModeDefault
	handoff := false
	if e.collabStore != nil && strings.TrimSpace(sessionID) != "" {
		st, err := e.collabStore.Get(ctx, sessionID)
		if err != nil {
			return ctx, collab.ModeDefault, false, fmt.Errorf("读取协作模式失败: %w", err)
		}
		mode = collab.Normalize(st.Mode)
		handoff = st.HandoffPending
	}
	ctx = collab.WithMode(ctx, mode)
	ctx = collab.WithHandoffPending(ctx, handoff)
	return ctx, mode, handoff, nil
}

func (e *ReactLoopEngine) clearHandoffPending(ctx context.Context, sessionID string) {
	if e.collabStore == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	st, err := e.collabStore.Get(ctx, sessionID)
	if err != nil || !st.HandoffPending {
		return
	}
	st.HandoffPending = false
	_ = e.collabStore.Set(ctx, sessionID, st)
}

func (e *ReactLoopEngine) applyCollabToolFilter(ctx context.Context, infos []*tool.Info) []*tool.Info {
	if len(infos) == 0 {
		return infos
	}
	mode := collab.ModeFromContext(ctx)
	depth := multicontract.DelegationDepth(ctx)
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		if info != nil {
			names = append(names, info.Name)
		}
	}
	allowed := make(map[string]struct{}, len(names))
	for _, n := range collab.FilterToolNames(mode, depth, names) {
		allowed[n] = struct{}{}
	}
	out := make([]*tool.Info, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		if _, ok := allowed[info.Name]; ok {
			out = append(out, info)
		}
	}
	return out
}

func (e *ReactLoopEngine) rebuildSystemForCollab(
	ctx context.Context,
	rc *runtime.RunContext,
	agent *domain.Agent,
	toolNames []string,
	mode collab.Mode,
	sessionID string,
	log logger.Logger,
) {
	if rc == nil || len(rc.Messages) == 0 || e.prompt == nil {
		return
	}
	visionMode := e.effectiveVisionMode
	if visionMode == "" {
		visionMode = vision.ModeDegradedText
	}
	systemPrompt, err := e.prompt.BuildSystem(ctx, prompt.BuildRequest{
		Agent:             agent,
		Run:               rc.Run,
		AvailableTools:    toolNames,
		VisionMode:        string(visionMode),
		Audience:          promptAudience(ctx),
		CollaborationMode: string(mode),
		PlanDocumentPath:  collab.PlanDocumentRelPath(sessionID),
	})
	if err != nil {
		log.Warn("重建规划模式 system 失败", "error", err)
		return
	}
	if rc.Messages[0] != nil && rc.Messages[0].Role == domain.RoleSystem {
		rc.Messages[0] = domain.NewSystemMessage(systemPrompt)
	}
}

func (e *ReactLoopEngine) syncCollabAfterTools(
	ctx context.Context,
	sessionID string,
	prevMode collab.Mode,
	rc *runtime.RunContext,
	agent *domain.Agent,
	toolInfos *[]*tool.Info,
	activeToolNames *[]string,
	log logger.Logger,
) (context.Context, collab.Mode, bool, error) {
	ctx, mode, handoff, err := e.prepareCollabContext(ctx, sessionID)
	if err != nil {
		return ctx, prevMode, false, err
	}
	if mode == prevMode {
		return ctx, mode, handoff, nil
	}
	base := e.getToolInfos(agent)
	filtered := e.applyCollabToolFilter(ctx, base)
	*toolInfos = filtered
	*activeToolNames = namesOfToolInfos(filtered)
	e.rebuildSystemForCollab(ctx, rc, agent, *activeToolNames, mode, sessionID, log)
	// 进入确认由 enter_plan_mode 工具结果给出；handoff 走 ephemeral reminder。
	log.Info("协作模式已切换", "mode", mode, "tools", strings.Join(*activeToolNames, ","))
	return ctx, mode, handoff, nil
}

func projectMessagesForModel(msgs []*domain.Message, planMode bool) []*domain.Message {
	if !planMode {
		return transcript.ProjectForModel(msgs)
	}
	// 规划模式：丢弃任务清单快照，不转为 reminder（与 todo_* 互斥）
	if len(msgs) == 0 {
		return nil
	}
	out := make([]*domain.Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		m.EnsureKind()
		if m.Kind == domain.MessageKindTaskListSnapshot {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (e *ReactLoopEngine) withPlanModeReminders(
	ctx context.Context,
	sessionID string,
	modelMsgs []*domain.Message,
	iteration int,
	handoff bool,
	mode collab.Mode,
) []*domain.Message {
	out := modelMsgs
	planPath := collab.PlanDocumentRelPath(sessionID)
	appendReminder := func(body string) {
		wrapped := planmodeprompt.WrapSystemReminder(body)
		if wrapped == "" {
			return
		}
		cp := make([]*domain.Message, 0, len(out)+1)
		cp = append(cp, out...)
		cp = append(cp, domain.NewReminderMessage(wrapped))
		out = cp
	}
	if handoff {
		appendReminder(planmodeprompt.HandoffReminder(planPath))
	}
	depth := multicontract.DelegationDepth(ctx)
	if mode == collab.ModePlan {
		if depth > 0 && (iteration == 0 || iteration%5 == 0) {
			appendReminder(planmodeprompt.SubAgentReminder(planPath))
		} else if depth == 0 && iteration > 0 && iteration%5 == 0 {
			appendReminder(planmodeprompt.SparseReminder(planPath))
		}
	}
	return out
}
