package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"

	hookcontract "genesis-agent/internal/capabilities/hook/contract"
	hookmodel "genesis-agent/internal/capabilities/hook/model"
	"genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime"
)

// CompactionKind 压缩类型安全枚举
type CompactionKind string

const (
	CompactionKindNone  CompactionKind = "none"  // 未触发压缩
	CompactionKindMicro CompactionKind = "micro" // L1：工具结果外置卸载（就地、无 LLM、幂等）
	CompactionKindAuto  CompactionKind = "auto"  // L2：LLM 结构化滚动摘要（CAS 会话锁，串行安全）
)

// Compaction 压缩操作的结果摘要
type Compaction struct {
	Triggered   bool           // 是否触发了压缩
	Kind        CompactionKind // 压缩类型
	TokensSaved int            // 节省的估算 Token 数
	SummaryID   string         // L2 摘要对应的 ID（SessionSummary.LeafID）
}

// Compactor 编排两级压缩的内核接口
type Compactor interface {
	// MaybeMicroCompact 在每轮 LLM 调用前尝试卸载大工具结果（就地、无 LLM、幂等）。
	MaybeMicroCompact(ctx context.Context, rc *runtime.RunContext) (Compaction, error)
	// MaybeAutoCompact 在使用率超阈时用 LLM 生成摘要并替换历史（CAS 会话状态，串行安全）。
	MaybeAutoCompact(ctx context.Context, rc *runtime.RunContext, ref memory.SessionRef) (Compaction, error)
}

// AsyncExtractor 异步记忆抽取队列提交接口
type AsyncExtractor interface {
	Submit(ref memory.SessionRef, msgs []*domain.Message)
}

// DefaultCompactor 实现 Compactor 接口，处理 L1/L2 两级上下文压缩
type DefaultCompactor struct {
	mu           sync.Mutex
	estimator    TokenEstimator
	sessionStore memory.SessionStore
	shortTermMem memory.ShortTermMemory
	sessionDir   string // 用于存放 L1 micro-compact artifacts 的本地根目录
	extractor    AsyncExtractor

	// 压缩调控参数
	contextWindow         int
	keepRecentTurns       int
	keepRecentTokenBudget int
	compactRatio          float64
	warnRatio             float64
	toolResultMaxTokens   int
}

// NewDefaultCompactor 创建默认两级压缩编排器
func NewDefaultCompactor(
	estimator TokenEstimator,
	sessionStore memory.SessionStore,
	shortTermMem memory.ShortTermMemory,
	sessionDir string,
	extractor AsyncExtractor,
	contextWindow int,
	keepRecentTurns int,
	keepRecentTokenBudget int,
	compactRatio float64,
	warnRatio float64,
	toolResultMaxTokens int,
) *DefaultCompactor {
	if keepRecentTurns <= 0 {
		keepRecentTurns = 6
	}
	if compactRatio <= 0 {
		compactRatio = 0.85
	}
	if warnRatio <= 0 {
		warnRatio = 0.75
	}
	if toolResultMaxTokens <= 0 {
		toolResultMaxTokens = 8000
	}
	return &DefaultCompactor{
		estimator:             estimator,
		sessionStore:          sessionStore,
		shortTermMem:          shortTermMem,
		sessionDir:            sessionDir,
		extractor:             extractor,
		contextWindow:         contextWindow,
		keepRecentTurns:       keepRecentTurns,
		keepRecentTokenBudget: keepRecentTokenBudget,
		compactRatio:          compactRatio,
		warnRatio:             warnRatio,
		toolResultMaxTokens:   toolResultMaxTokens,
	}
}

// MaybeMicroCompact 尝试卸载大工具结果（就地、无 LLM、幂等）
func (c *DefaultCompactor) MaybeMicroCompact(ctx context.Context, rc *runtime.RunContext) (Compaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(rc.Messages) == 0 {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
	}

	// 1. 扫描最近 keep_last_tool_uses 次工具调用的 tool_call_id
	keepLast := 3
	recentToolCallIDs := make([]string, 0, keepLast)
	seenIDs := make(map[string]struct{})

	// 从后往前查找最近的 assistant tool_call 或 tool_result
	for i := len(rc.Messages) - 1; i >= 0; i-- {
		msg := rc.Messages[i]
		if msg.NormalizedKind() == domain.MessageKindToolResult && msg.ToolCallID != "" {
			if _, ok := seenIDs[msg.ToolCallID]; !ok {
				seenIDs[msg.ToolCallID] = struct{}{}
				recentToolCallIDs = append(recentToolCallIDs, msg.ToolCallID)
				if len(recentToolCallIDs) >= keepLast {
					break
				}
			}
		}
	}

	recentSet := make(map[string]struct{})
	for _, id := range recentToolCallIDs {
		recentSet[id] = struct{}{}
	}
	if c.hasMicroCompactionCandidate(rc, recentSet) {
		if blocked, err := dispatchPreCompact(ctx, CompactionKindMicro, rc, 0); err != nil {
			return Compaction{Triggered: false, Kind: CompactionKindNone}, err
		} else if blocked {
			return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
		}
	}

	// 2. 执行就地卸载
	tokensSaved := 0
	triggered := false
	artifactsDir := filepath.Join(c.sessionDir, rc.Run.SessionID, "artifacts")

	lastSkillIdx := -1
	for i := len(rc.Messages) - 1; i >= 0; i-- {
		if rc.Messages[i] != nil && rc.Messages[i].NormalizedKind() == domain.MessageKindSkillInjection {
			lastSkillIdx = i
			break
		}
	}

	for i, msg := range rc.Messages {
		if msg == nil {
			continue
		}
		if msg.NormalizedKind() == domain.MessageKindToolResult && msg.ToolCallID != "" {
			// 若属于最近 3 次的 tool_result，保留不卸载
			if _, ok := recentSet[msg.ToolCallID]; ok {
				continue
			}
			// 校验长度超限，且非已经持久化的标签
			if len(msg.Content) > c.toolResultMaxTokens && !isAlreadyMicrocompacted(msg.Content) {
				if err := os.MkdirAll(artifactsDir, 0755); err != nil {
					return Compaction{Triggered: false, Kind: CompactionKindNone}, fmt.Errorf("create artifacts dir failed: %w", err)
				}

				// 保存完整内容至本地 artifacts
				fileName := fmt.Sprintf("tool-%s.log", msg.ToolCallID)
				filePath := filepath.Join(artifactsDir, fileName)
				if err := os.WriteFile(filePath, []byte(msg.Content), 0600); err != nil {
					return Compaction{Triggered: false, Kind: CompactionKindNone}, fmt.Errorf("write tool output file failed: %w", err)
				}

				// 计算 Token 节省值
				originalTokens := c.estimator.EstimateMessages(ctx, []*domain.Message{msg}, getModel(rc))

				// 原位替换为占位文本 (相对路径引用 artifacts/...)
				preview := ""
				if len(msg.Content) > 400 {
					preview = msg.Content[:400]
				} else {
					preview = msg.Content
				}
				msg.Content = fmt.Sprintf("<persisted-output ref=\"artifacts/%s\">[Oversized tool result (%d chars) persisted. First 400 chars:\n%s...]</persisted-output>", fileName, len(msg.Content), preview)

				newTokens := c.estimator.EstimateMessages(ctx, []*domain.Message{msg}, getModel(rc))
				tokensSaved += (originalTokens - newTokens)
				triggered = true
			}
		}

		// 历史陈旧技能指引消息：若非末端最新一个且长度超限，在 L1 阶段就地原位折叠为标签
		if i < lastSkillIdx && msg.NormalizedKind() == domain.MessageKindSkillInjection {
			if len(msg.Content) > c.toolResultMaxTokens && !isAlreadyMicrocompacted(msg.Content) {
				originalTokens := c.estimator.EstimateMessages(ctx, []*domain.Message{msg}, getModel(rc))

				handle := extractSkillHandle(msg.Content)
				kbSize := float64(len(msg.Content)) / 1024.0
				msg.Content = fmt.Sprintf("<skill-injection-summary handle=\"%s\">[历史技能指引已执行完毕，已折叠 %.1fKB]</skill-injection-summary>", handle, kbSize)

				newTokens := c.estimator.EstimateMessages(ctx, []*domain.Message{msg}, getModel(rc))
				tokensSaved += (originalTokens - newTokens)
				triggered = true
			}
		}
	}

	if triggered {
		return Compaction{
			Triggered:   true,
			Kind:        CompactionKindMicro,
			TokensSaved: tokensSaved,
		}, nil
	}

	return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
}

func (c *DefaultCompactor) hasMicroCompactionCandidate(rc *runtime.RunContext, recentSet map[string]struct{}) bool {
	lastSkillIdx := -1
	for i := len(rc.Messages) - 1; i >= 0; i-- {
		if rc.Messages[i] != nil && rc.Messages[i].NormalizedKind() == domain.MessageKindSkillInjection {
			lastSkillIdx = i
			break
		}
	}

	for i, msg := range rc.Messages {
		if msg == nil {
			continue
		}
		if msg.NormalizedKind() == domain.MessageKindToolResult && msg.ToolCallID != "" {
			if _, recent := recentSet[msg.ToolCallID]; !recent {
				if len(msg.Content) > c.toolResultMaxTokens && !isAlreadyMicrocompacted(msg.Content) {
					return true
				}
			}
		}
		if i < lastSkillIdx && msg.NormalizedKind() == domain.MessageKindSkillInjection {
			if len(msg.Content) > c.toolResultMaxTokens && !isAlreadyMicrocompacted(msg.Content) {
				return true
			}
		}
	}
	return false
}

// MaybeAutoCompact 在使用率超阈时用 LLM 生成摘要并替换历史（CAS 会话状态，安全互斥）
func (c *DefaultCompactor) MaybeAutoCompact(ctx context.Context, rc *runtime.RunContext, ref memory.SessionRef) (Compaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. 估算 Token 占用并校验是否超标
	totalTokens := c.estimator.EstimateMessages(ctx, rc.Messages, getModel(rc))
	limit := int(float64(c.contextWindow) * c.compactRatio)
	if totalTokens < limit {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
	}

	// 2. 校验用户轮数下界（防抖动）
	userTurnsCount := 0
	for _, m := range rc.Messages {
		if m.NormalizedKind() == domain.MessageKindUserTurn {
			userTurnsCount++
		}
	}
	if userTurnsCount < 4 {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
	}
	if blocked, err := dispatchPreCompact(ctx, CompactionKindAuto, rc, totalTokens); err != nil {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, err
	} else if blocked {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
	}

	// 3. 申请 CAS 状态机锁：active -> compacting
	locked, err := c.sessionStore.UpdateStatus(ctx, ref.SessionID, domain.SessionStateActive, domain.SessionStateCompacting)
	if err != nil {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, fmt.Errorf("lock session for compaction failed: %w", err)
	}
	if !locked {
		// 并发锁定冲突，直接跳过不挂起
		return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
	}

	// 确保发生异常时能够强制释放锁，回归 active 保证会话正常流转
	defer func() {
		_, _ = c.sessionStore.UpdateStatus(ctx, ref.SessionID, domain.SessionStateCompacting, domain.SessionStateActive)
	}()

	// 4. 以持久化历史与当前 Run 增量的并集生成确定性摘要快照。
	// 不能只让后端自行读取历史：当前 Run 尚未落盘时会造成摘要和内存替换边界不一致。
	compactMessages, err := c.compactionMessages(ctx, rc, ref)
	if err != nil {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, fmt.Errorf("build compaction message snapshot failed: %w", err)
	}
	if countUserTurns(compactMessages) <= c.keepRecentTurns {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
	}

	// 调用 ShortTermMemory 的 Summarize 触发滚动摘要生成与持久化。
	opt := memory.SummarizeOptions{
		KeepRecentTurns: c.keepRecentTurns,
		Model:           getModel(rc),
		Messages:        compactMessages,
	}

	sumResult, err := c.shortTermMem.Summarize(ctx, ref, opt)
	if err != nil {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, fmt.Errorf("execute shortTermMem Summarize failed: %w", err)
	}

	if sumResult.Summary == nil {
		return Compaction{Triggered: false, Kind: CompactionKindNone}, nil
	}

	// 5. 用摘要叶节点替换内存中已经被摘要覆盖的历史。
	var systemMsgs []*domain.Message
	var retained []*domain.Message
	leafInRunContext := containsMessageID(rc.Messages, sumResult.Summary.LeafID)
	leafFound := false
	for _, m := range rc.Messages {
		if m == nil {
			continue
		}
		if m.NormalizedKind() == domain.MessageKindSystem {
			systemMsgs = append(systemMsgs, m)
			continue
		}
		if m.NormalizedKind() == domain.MessageKindConversationSummary {
			continue
		}
		if m.UUID == sumResult.Summary.LeafID {
			leafFound = true
			continue
		}
		if leafInRunContext && !leafFound {
			continue
		}
		retained = append(retained, m)
	}

	// 构造新的摘要消息，填入 domain.Message 序列中作为头部背景
	summaryMsg := domain.NewConversationSummaryMessage(sumResult.Summary.Content)
	summaryMsg.UUID = sumResult.Summary.LeafID

	// 拼接：[System] + [Summary] + [KeepRecent]
	newMsgs := append(systemMsgs, summaryMsg)
	newMsgs = append(newMsgs, retained...)

	// 异步提交被折叠的历史给长期记忆提炼队列
	if c.extractor != nil {
		var dialogueMsgs []*domain.Message
		for _, m := range compactedMessages(compactMessages, sumResult.Summary.LeafID) {
			if m != nil && m.NormalizedKind() != domain.MessageKindSystem {
				dialogueMsgs = append(dialogueMsgs, m)
			}
		}
		c.extractor.Submit(ref, dialogueMsgs)
	}

	rc.Messages = newMsgs
	c.recoverRecentArtifacts(rc)

	return Compaction{
		Triggered:   true,
		Kind:        CompactionKindAuto,
		TokensSaved: sumResult.TokensSaved,
		SummaryID:   sumResult.Summary.LeafID,
	}, nil
}

// dispatchPreCompact 在实际变更运行上下文前执行 Hook。压缩被拒绝时只跳过本次压缩，
// 保持原有消息不变；Hook 执行失败仍向调用方显式返回，避免静默绕过治理规则。
func dispatchPreCompact(ctx context.Context, kind CompactionKind, rc *runtime.RunContext, estimatedTokens int) (bool, error) {
	dispatcher := hookcontract.FromContext(ctx)
	if dispatcher == nil {
		return false, nil
	}
	payload := map[string]any{
		"compaction_kind":  string(kind),
		"estimated_tokens": estimatedTokens,
		"message_count":    len(rc.Messages),
	}
	result, err := dispatcher.Dispatch(ctx, hookmodel.Event{Name: hookmodel.EventPreCompact, MatchKey: string(kind), Payload: payload})
	if err != nil {
		return false, fmt.Errorf("执行 PreCompact Hook 失败: %w", err)
	}
	hookcontract.AppendAdditionalContext(ctx, result.AdditionalContext...)
	return result.Blocked || result.NeedApproval, nil
}

var artifactRefPattern = regexp.MustCompile(`ref="(artifacts/[^"]+)"`)

// recoverRecentArtifacts 将最近外置工具结果的相对引用回填为小型 reminder，避免 L2 摘要后丢失可恢复工件。
func (c *DefaultCompactor) recoverRecentArtifacts(rc *runtime.RunContext) {
	const keep = 3
	refs := make([]string, 0, keep)
	seen := make(map[string]struct{})
	for i := len(rc.Messages) - 1; i >= 0 && len(refs) < keep; i-- {
		m := rc.Messages[i]
		if m == nil || m.NormalizedKind() != domain.MessageKindToolResult {
			continue
		}
		match := artifactRefPattern.FindStringSubmatch(m.Content)
		if len(match) != 2 {
			continue
		}
		if _, ok := seen[match[1]]; ok {
			continue
		}
		seen[match[1]] = struct{}{}
		refs = append(refs, match[1])
	}
	if len(refs) == 0 {
		return
	}
	for i, j := 0, len(refs)-1; i < j; i, j = i+1, j-1 {
		refs[i], refs[j] = refs[j], refs[i]
	}
	rc.Messages = append(rc.Messages, domain.NewReminderMessage("<artifact_recovery>\nRecent persisted tool outputs remain available at:\n- "+strings.Join(refs, "\n- ")+"\n</artifact_recovery>").WithSource(domain.MessageSourceCompactor))
}

func compactedMessages(msgs []*domain.Message, leafID string) []*domain.Message {
	if leafID == "" {
		return nil
	}
	for i, m := range msgs {
		if m != nil && m.UUID == leafID {
			return msgs[:i+1]
		}
	}
	return nil
}

func (c *DefaultCompactor) compactionMessages(ctx context.Context, rc *runtime.RunContext, ref memory.SessionRef) ([]*domain.Message, error) {
	persisted, err := c.shortTermMem.GetRecent(ctx, ref, memory.RecentOptions{})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	result := make([]*domain.Message, 0, len(persisted.Messages)+len(rc.Messages))
	appendMessage := func(m *domain.Message) {
		if m == nil || m.NormalizedKind() == domain.MessageKindSystem || m.NormalizedKind() == domain.MessageKindConversationSummary {
			return
		}
		if m.UUID == "" {
			m.UUID = uuid.NewString()
		}
		if _, exists := seen[m.UUID]; exists {
			return
		}
		seen[m.UUID] = struct{}{}
		result = append(result, m)
	}
	for _, m := range persisted.Messages {
		appendMessage(m)
	}
	for _, m := range rc.Messages {
		appendMessage(m)
	}
	return result, nil
}

func countUserTurns(msgs []*domain.Message) int {
	count := 0
	for _, m := range msgs {
		if m != nil && m.NormalizedKind() == domain.MessageKindUserTurn {
			count++
		}
	}
	return count
}

func containsMessageID(msgs []*domain.Message, id string) bool {
	if id == "" {
		return false
	}
	for _, m := range msgs {
		if m != nil && m.UUID == id {
			return true
		}
	}
	return false
}

// isAlreadyMicrocompacted 校验该正文是否已经是持久化或原位微折叠后的占位符
func isAlreadyMicrocompacted(content string) bool {
	return strings.Contains(content, "<persisted-output") || strings.Contains(content, "<skill-injection-summary")
}

func extractSkillHandle(content string) string {
	re := regexp.MustCompile(`(?:name|handle|qualified_name)="([^"]+)"`)
	matches := re.FindStringSubmatch(content)
	if len(matches) > 1 && strings.TrimSpace(matches[1]) != "" {
		return strings.TrimSpace(matches[1])
	}
	return "skill"
}

func getModel(rc *runtime.RunContext) string {
	if rc != nil && rc.Agent != nil && rc.Agent.DefaultModel != "" {
		return rc.Agent.DefaultModel
	}
	return "default"
}
