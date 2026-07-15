// Package file 提供基于本地 JSONL 文件的短期记忆持久化实现。
package file

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	llm "genesis-agent/internal/capabilities/llm/contract"
	contract "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
	runtimecontext "genesis-agent/internal/runtime/context"
)

// MessageEnvelope JSONL 存储用的信封结构，承载消息语义类型及恢复锚点 UUID
type MessageEnvelope struct {
	UUID      string             `json:"uuid"`
	Kind      domain.MessageKind `json:"kind"`
	Source    string             `json:"source,omitempty"`
	Timestamp int64              `json:"timestamp"`
	Message   *domain.Message    `json:"message"`
}

// FileShortTermMemory 本地 JSONL 文件短期记忆实现
type FileShortTermMemory struct {
	mu         sync.RWMutex
	baseDir    string // 存储根目录，如 ~/.genesis-agent/cli/sessions
	estimator  runtimecontext.TokenEstimator
	summarizer llm.ChatModel // 用于执行 L2 滚动摘要的大模型
}

// NewFileShortTermMemory 创建本地文件短期记忆存储
func NewFileShortTermMemory(baseDir string, estimator runtimecontext.TokenEstimator, summarizer llm.ChatModel) *FileShortTermMemory {
	return &FileShortTermMemory{
		baseDir:    baseDir,
		estimator:  estimator,
		summarizer: summarizer,
	}
}

// CreateSession 实现 SessionStore 接口
func (f *FileShortTermMemory) CreateSession(ctx context.Context, session *domain.Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	if err := validateSessionID(session.ID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := os.Stat(f.sessionMetaFilePath(session.ID)); err == nil {
		return fmt.Errorf("create session %q: already exists", session.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat session metadata: %w", err)
	}
	copy := *session
	if copy.Status == "" {
		copy.Status = domain.SessionStateActive
	}
	now := time.Now().UTC()
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = now
	}
	copy.UpdatedAt = now
	return f.writeSessionLocked(&copy)
}

// GetSession 实现 SessionStore 接口
func (f *FileShortTermMemory) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.readSessionLocked(sessionID)
}

// ListSessions 实现 SessionStore 接口。
func (f *FileShortTermMemory) ListSessions(ctx context.Context, query contract.SessionQuery) ([]*domain.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()

	entries, err := os.ReadDir(f.baseDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sessions directory: %w", err)
	}
	sessions := make([]*domain.Session, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || validateSessionID(entry.Name()) != nil {
			continue
		}
		session, err := f.readSessionLocked(entry.Name())
		if err != nil {
			if errors.Is(err, contract.ErrSessionNotFound) {
				continue
			}
			return nil, err
		}
		if !matchesSessionQuery(session, query) {
			continue
		}
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt) })
	if query.Limit > 0 && len(sessions) > query.Limit {
		sessions = sessions[:query.Limit]
	}
	return sessions, nil
}

// FindLatestSession 实现 SessionStore 接口。
func (f *FileShortTermMemory) FindLatestSession(ctx context.Context, query contract.SessionQuery) (*domain.Session, error) {
	query.Limit = 1
	sessions, err := f.ListSessions(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, contract.ErrSessionNotFound
	}
	return sessions[0], nil
}

// UpdateSession 实现 SessionStore 接口
func (f *FileShortTermMemory) UpdateSession(ctx context.Context, session *domain.Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	if err := validateSessionID(session.ID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	current, err := f.readSessionLocked(session.ID)
	if err != nil {
		return err
	}
	copy := *session
	copy.CreatedAt = current.CreatedAt
	copy.UpdatedAt = time.Now().UTC()
	return f.writeSessionLocked(&copy)
}

// UpdateStatus 实现 SessionStore 接口，带有 CAS 状态机安全锁控制
func (f *FileShortTermMemory) UpdateStatus(ctx context.Context, sessionID string, expected, target domain.SessionState) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := validateSessionID(sessionID); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	currentSession, err := f.readSessionLocked(sessionID)
	if err != nil {
		return false, err
	}
	current := currentSession.Status
	if current != expected {
		return false, nil
	}
	currentSession.Status = target
	currentSession.UpdatedAt = time.Now().UTC()
	return true, f.writeSessionLocked(currentSession)
}

// DeleteSession 实现 SessionStore 接口
func (f *FileShortTermMemory) DeleteSession(ctx context.Context, sessionID string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	session, err := f.readSessionLocked(sessionID)
	if err != nil {
		return err
	}
	session.Status = domain.SessionStateDeleted
	session.UpdatedAt = time.Now().UTC()
	return f.writeSessionLocked(session)
}

func (f *FileShortTermMemory) sessionMetaFilePath(sessionID string) string {
	return filepath.Join(f.baseDir, sessionID, "session.json")
}

func (f *FileShortTermMemory) readSessionLocked(sessionID string) (*domain.Session, error) {
	data, err := os.ReadFile(f.sessionMetaFilePath(sessionID))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("get session %q: %w", sessionID, contract.ErrSessionNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read session metadata: %w", err)
	}
	var session domain.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("unmarshal session metadata: %w", err)
	}
	if session.ID != sessionID {
		return nil, fmt.Errorf("session metadata id mismatch for %q", sessionID)
	}
	return &session, nil
}

func (f *FileShortTermMemory) writeSessionLocked(session *domain.Session) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session metadata: %w", err)
	}
	dir := filepath.Dir(f.sessionMetaFilePath(session.ID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, "session-*.tmp")
	if err != nil {
		return fmt.Errorf("create session metadata temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write session metadata: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close session metadata: %w", err)
	}
	if err := os.Rename(tempPath, f.sessionMetaFilePath(session.ID)); err != nil {
		return fmt.Errorf("replace session metadata: %w", err)
	}
	return nil
}

func validateSessionID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" || sessionID == "." || filepath.Base(sessionID) != sessionID {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	return nil
}

func matchesSessionQuery(session *domain.Session, query contract.SessionQuery) bool {
	if session.Status == domain.SessionStateDeleted && !query.IncludeDeleted {
		return false
	}
	if session.Status == domain.SessionStateArchived && !query.IncludeArchived {
		return false
	}
	return (query.TenantID == "" || session.TenantID == query.TenantID) &&
		(query.UserID == "" || session.UserID == query.UserID) &&
		(query.AgentID == "" || session.AgentID == query.AgentID) &&
		(query.AppID == "" || session.AppID == query.AppID)
}

// sessionFilePath 获取指定 session 的持久化 JSONL 文件路径
func (f *FileShortTermMemory) sessionFilePath(sessionID string) string {
	return filepath.Join(f.baseDir, sessionID, "messages.jsonl")
}

// summaryFilePath 获取指定 session 的滚动摘要 JSON 文件路径
func (f *FileShortTermMemory) summaryFilePath(sessionID string) string {
	return filepath.Join(f.baseDir, sessionID, "summary.json")
}

// Append 追加消息到会话本地 JSONL 文件中
func (f *FileShortTermMemory) Append(ctx context.Context, ref contract.SessionRef, msgs []*domain.Message) error {
	if err := validateSessionID(ref.SessionID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	filePath := f.sessionFilePath(ref.SessionID)
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create session dir failed: %w", err)
	}

	// 追加写入模式打开
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open session log file failed: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, m := range msgs {
		if m == nil {
			continue
		}
		m.EnsureKind()

		// 确保 UUID 生成并写入 domain.Message 实体以实现内存/磁盘绑定
		if m.UUID == "" {
			m.UUID = newUUID()
		}

		// 封装信封
		envelope := MessageEnvelope{
			UUID:      m.UUID,
			Kind:      m.NormalizedKind(),
			Source:    m.Source,
			Timestamp: time.Now().UnixNano(),
			Message:   m,
		}

		data, err := json.Marshal(envelope)
		if err != nil {
			return fmt.Errorf("marshal message envelope failed: %w", err)
		}

		if _, err := writer.Write(data); err != nil {
			return fmt.Errorf("write message envelope failed: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return fmt.Errorf("write newline failed: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush session log file failed: %w", err)
	}

	return nil
}

// GetRecent 从新到旧加载消息历史，并在满足 recent 限制时进行就地成对性保护与截断
// 支持基于 summary.json 的 leafID 会话恢复重组
func (f *FileShortTermMemory) GetRecent(ctx context.Context, ref contract.SessionRef, opt contract.RecentOptions) (contract.RecentResult, error) {
	if err := validateSessionID(ref.SessionID); err != nil {
		return contract.RecentResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return contract.RecentResult{}, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()

	filePath := f.sessionFilePath(ref.SessionID)
	// 文件不存在且摘要不存在返回空
	summaryPath := f.summaryFilePath(ref.SessionID)
	hasSummary := false
	var summary domain.SessionSummary
	if _, err := os.Stat(summaryPath); err == nil {
		if sData, sErr := os.ReadFile(summaryPath); sErr == nil {
			if json.Unmarshal(sData, &summary) == nil {
				hasSummary = true
			}
		}
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) && !hasSummary {
		return contract.RecentResult{Messages: []*domain.Message{}, Truncated: false}, nil
	}

	var envelopes []MessageEnvelope
	if file, err := os.Open(filePath); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var env MessageEnvelope
			if err := json.Unmarshal(line, &env); err == nil && env.Message != nil {
				env.Message.EnsureKind()
				if env.Message.UUID == "" {
					env.Message.UUID = env.UUID
				}
				envelopes = append(envelopes, env)
			}
		}
		if sErr := scanner.Err(); sErr != nil {
			return contract.RecentResult{}, fmt.Errorf("scan log file failed: %w", sErr)
		}
	}

	// 1. 根据 L2 摘要 leaf_id 裁剪重组历史消息段
	var allMsgs []*domain.Message
	if hasSummary && summary.LeafID != "" {
		cutIdx := -1
		for i, env := range envelopes {
			if env.UUID == summary.LeafID {
				cutIdx = i
				break
			}
		}
		if cutIdx >= 0 && cutIdx < len(envelopes) {
			for _, env := range envelopes[cutIdx+1:] {
				allMsgs = append(allMsgs, env.Message)
			}
		} else {
			// 如果没找到，退化为全部加载
			for _, env := range envelopes {
				allMsgs = append(allMsgs, env.Message)
			}
		}
		// 重构头部摘要消息回填
		summaryMsg := domain.NewConversationSummaryMessage(summary.Content)
		summaryMsg.UUID = summary.LeafID
		allMsgs = append([]*domain.Message{summaryMsg}, allMsgs...)
	} else {
		allMsgs = make([]*domain.Message, len(envelopes))
		for i, env := range envelopes {
			allMsgs[i] = env.Message
		}
	}

	if len(allMsgs) == 0 {
		return contract.RecentResult{Messages: []*domain.Message{}, Truncated: false}, nil
	}

	// 若未设置限制，直接返回全量重组历史
	if opt.MaxTurns <= 0 && opt.MaxTokens <= 0 {
		return contract.RecentResult{Messages: allMsgs, Truncated: false}, nil
	}

	// 2. 逆序回溯计算，识别截断边界并保留成对的 Tool 调用关系
	truncatedIdx := 0
	userTurnsCount := 0
	usedTokens := 0
	truncated := false

	// 为实现 ToolChain 保护，我们需要向前查找 Tool Calls
	pendingToolCallIDs := make(map[string]struct{})

	for i := len(allMsgs) - 1; i >= 0; i-- {
		msg := allMsgs[i]

		// 如果此消息是 tool_result，将 tool_call_id 记入待闭合列表
		if msg.NormalizedKind() == domain.MessageKindToolResult && msg.ToolCallID != "" {
			pendingToolCallIDs[msg.ToolCallID] = struct{}{}
		}

		// 如果此消息是 assistant 且发起了 tool_calls，从待闭合列表闭合对应的 ID
		if msg.Role == domain.RoleAssistant && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				delete(pendingToolCallIDs, tc.ID)
			}
		}

		// 计算本消息 Token 消耗
		msgTokens := f.estimator.EstimateMessages(ctx, []*domain.Message{msg}, opt.Model)

		// 统计用户轮数 (MessageKindUserTurn)
		isUserTurn := msg.NormalizedKind() == domain.MessageKindUserTurn

		// 判定是否超出预算。
		inMiddleOfToolChain := len(pendingToolCallIDs) > 0

		if !inMiddleOfToolChain {
			// 只有在 tool chain 闭合的干净边界上，才能进行截断判定
			turnExceeded := opt.MaxTurns > 0 && userTurnsCount >= opt.MaxTurns && isUserTurn
			tokenExceeded := opt.MaxTokens > 0 && usedTokens+msgTokens > opt.MaxTokens

			if turnExceeded || tokenExceeded {
				truncatedIdx = i + 1
				truncated = true
				break
			}
		}

		// 确认载入该消息
		if isUserTurn {
			userTurnsCount++
		}
		usedTokens += msgTokens
	}

	return contract.RecentResult{
		Messages:  allMsgs[truncatedIdx:],
		Truncated: truncated,
	}, nil
}

// Replay 返回可审计的会话消息流；leafID 非空时截至指定消息（含）。
func (f *FileShortTermMemory) Replay(ctx context.Context, ref contract.SessionRef, leafID string) ([]*domain.Message, error) {
	res, err := f.GetRecent(ctx, ref, contract.RecentOptions{})
	if err != nil {
		return nil, err
	}
	if leafID == "" {
		return res.Messages, nil
	}
	for i, message := range res.Messages {
		if message != nil && message.UUID == leafID {
			return res.Messages[:i+1], nil
		}
	}
	return nil, fmt.Errorf("replay leaf %q not found", leafID)
}

// Fork 将源会话的可恢复历史复制到目标会话，不修改源会话。
func (f *FileShortTermMemory) Fork(ctx context.Context, source, target contract.SessionRef, leafID string) error {
	if err := validateSessionID(target.SessionID); err != nil {
		return err
	}
	messages, err := f.Replay(ctx, source, leafID)
	if err != nil {
		return fmt.Errorf("replay source session: %w", err)
	}
	cloned := make([]*domain.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		copy := *message
		copy.UUID = ""
		cloned = append(cloned, &copy)
	}
	return f.Append(ctx, target, cloned)
}

const defaultCompactPrompt = `Please provide a comprehensive summary of our conversation structured as follows:

## Technical Context
Development environment, tools, frameworks, and configurations in use. Programming languages, libraries, and technical constraints.

## Project Overview
Main project goals, features, and scope. Key components.

## Code Changes
Files created, modified, or analyzed. Specific code implementations, functions.

## Debugging & Issues
Problems encountered and their root causes. Solutions implemented.

## Current Status
What we just completed successfully. Current state of the codebase.

## Pending Tasks
Immediate next steps and priorities. Planned features.

## User Preferences
Coding style, formatting, and organizational preferences.

## Key Decisions
Important technical decisions made and their rationale.

Focus on information essential for continuing the conversation effectively, including specific details about code, files, errors, and plans.`

// Summarize 将 keepRecentTurns 之外的旧历史提取，并与旧摘要进行 LLM 滚动合并成新摘要写入磁盘
func (f *FileShortTermMemory) Summarize(ctx context.Context, ref contract.SessionRef, opt contract.SummarizeOptions) (contract.SummaryResult, error) {
	if f.summarizer == nil {
		return contract.SummaryResult{}, fmt.Errorf("summarizer ChatModel is not configured in FileShortTermMemory")
	}

	// 1. 优先使用运行时传入的确定性快照；为空时才从持久层恢复。
	allMsgs := opt.Messages
	if allMsgs == nil {
		res, err := f.GetRecent(ctx, ref, contract.RecentOptions{})
		if err != nil {
			return contract.SummaryResult{}, fmt.Errorf("load full history for summary failed: %w", err)
		}
		allMsgs = res.Messages
	}
	if len(allMsgs) == 0 {
		return contract.SummaryResult{Summary: nil, TokensSaved: 0}, nil
	}

	// 2. 划定保留的历史范围：从后往前数 keepRecentTurns 个 user_turn
	keepTurns := opt.KeepRecentTurns
	if keepTurns <= 0 {
		keepTurns = 6
	}

	splitIdx := 0
	userTurnsSeen := 0
	for i := len(allMsgs) - 1; i >= 0; i-- {
		if allMsgs[i].NormalizedKind() == domain.MessageKindUserTurn {
			userTurnsSeen++
			if userTurnsSeen > keepTurns {
				splitIdx = i + 1
				break
			}
		}
	}

	// 如果根本没有超出 keepTurns 轮，说明无需做 L2 摘要压缩
	if splitIdx <= 0 {
		return contract.SummaryResult{Summary: nil, TokensSaved: 0}, nil
	}

	// 3. 提取老历史与已有摘要
	oldHistory := allMsgs[:splitIdx]
	var oldSummary *domain.SessionSummary
	if s, sErr := f.GetSummary(ctx, ref); sErr == nil {
		oldSummary = s
	}

	// 获取老历史最尾部（即截断线前）的最后一条消息 UUID 作为 LeafID
	var leafID string
	for i := len(oldHistory) - 1; i >= 0; i-- {
		if oldHistory[i].UUID != "" {
			leafID = oldHistory[i].UUID
			break
		}
	}

	// 4. 构建 LLM 滚动压缩提示词
	var previousSummaryText string
	if oldSummary != nil {
		previousSummaryText = oldSummary.Content
	} else {
		previousSummaryText = "No previous summary."
	}

	// 提取出所有对话文本用于 LLM 提炼
	var chatLines []string
	for _, m := range oldHistory {
		if m.NormalizedKind() == domain.MessageKindConversationSummary {
			continue // 避免重复携带老摘要的本体
		}
		roleLabel := string(m.Role)
		if m.NormalizedKind() == domain.MessageKindSkillInjection {
			roleLabel = "skill_injection"
		}
		chatLines = append(chatLines, fmt.Sprintf("[%s]: %s", roleLabel, m.Content))
	}
	chatContent := strings.Join(chatLines, "\n")

	promptTokens := f.estimator.EstimateMessages(ctx, oldHistory, opt.Model)

	// 组装提问消息
	compactInstruction := fmt.Sprintf("%s\n\n## Previous Summary:\n%s\n\n## Conversation History segment to digest:\n%s\n",
		defaultCompactPrompt, previousSummaryText, chatContent)

	llmMsgs := []*domain.Message{
		domain.NewUserMessage(compactInstruction),
	}

	// 5. 调用大模型生成综合新摘要
	respMsg, err := f.summarizer.Generate(ctx, llmMsgs, nil)
	if err != nil {
		return contract.SummaryResult{}, fmt.Errorf("call summarizer LLM failed: %w", err)
	}

	newContent := respMsg.Content
	if strings.TrimSpace(newContent) == "" {
		return contract.SummaryResult{}, fmt.Errorf("summarizer LLM returned empty content")
	}

	// 6. 保存新的 SessionSummary 至本地 summary.json
	newIteration := 1
	if oldSummary != nil {
		newIteration = oldSummary.Iteration + 1
	}

	summaryRecord := domain.SessionSummary{
		SessionID:   ref.SessionID,
		Content:     newContent,
		LeafID:      leafID,
		TokensCount: f.estimator.EstimateMessages(ctx, []*domain.Message{respMsg}, opt.Model),
		Iteration:   newIteration,
		CreatedAt:   time.Now(),
	}

	summaryPath := f.summaryFilePath(ref.SessionID)
	summaryDir := filepath.Dir(summaryPath)
	if err := os.MkdirAll(summaryDir, 0755); err != nil {
		return contract.SummaryResult{}, fmt.Errorf("create summary dir failed: %w", err)
	}

	sData, err := json.Marshal(summaryRecord)
	if err != nil {
		return contract.SummaryResult{}, fmt.Errorf("marshal summary failed: %w", err)
	}

	if err := os.WriteFile(summaryPath, sData, 0600); err != nil {
		return contract.SummaryResult{}, fmt.Errorf("write summary file failed: %w", err)
	}

	return contract.SummaryResult{
		Summary:     &summaryRecord,
		TokensSaved: promptTokens - summaryRecord.TokensCount,
	}, nil
}

// GetSummary 读取本地 summary.json 内容
func (f *FileShortTermMemory) GetSummary(ctx context.Context, ref contract.SessionRef) (*domain.SessionSummary, error) {
	if err := validateSessionID(ref.SessionID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	summaryPath := f.summaryFilePath(ref.SessionID)
	if _, err := os.Stat(summaryPath); os.IsNotExist(err) {
		return nil, nil
	}

	sData, err := os.ReadFile(summaryPath)
	if err != nil {
		return nil, fmt.Errorf("read summary file failed: %w", err)
	}

	var summary domain.SessionSummary
	if err := json.Unmarshal(sData, &summary); err != nil {
		return nil, fmt.Errorf("unmarshal summary failed: %w", err)
	}

	return &summary, nil
}

// Clear 软清除或物理删除本地 JSONL 文件
func (f *FileShortTermMemory) Clear(ctx context.Context, ref contract.SessionRef) error {
	if err := validateSessionID(ref.SessionID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	filePath := f.sessionFilePath(ref.SessionID)
	if _, err := os.Stat(filePath); err == nil {
		if err := os.Remove(filePath); err != nil {
			return fmt.Errorf("remove session file failed: %w", err)
		}
	}
	summaryPath := f.summaryFilePath(ref.SessionID)
	if _, err := os.Stat(summaryPath); err == nil {
		_ = os.Remove(summaryPath)
	}
	return nil
}

// newUUID 快速生成一个唯一的元数据 Uuid 标，用于截断定位
func newUUID() string {
	return "msg-" + uuid.NewString()
}
