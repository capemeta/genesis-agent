package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	llm "genesis-agent/internal/capabilities/llm/contract"
	contract "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
)

// DefaultMemoryExtractor 默认长期记忆提取器实现
type DefaultMemoryExtractor struct {
	llmClient llm.ChatModel
}

// NewDefaultMemoryExtractor 创建默认提取器
func NewDefaultMemoryExtractor(llmClient llm.ChatModel) *DefaultMemoryExtractor {
	return &DefaultMemoryExtractor{
		llmClient: llmClient,
	}
}

// rawEntry 大模型提取输出的 JSON 结构体格式
type rawEntry struct {
	Content    string   `json:"content"`
	Importance float64  `json:"importance"` // 0 ~ 1 浮点数
	MemoryType string   `json:"memory_type"` // semantic / episodic / procedural / negative
	Tags       []string `json:"tags"`
}

const stageOneSystemPrompt = `You are a long-term memory extraction agent.
Your goal is to digest the given developers' conversation segment and extract key semantic knowledge, episodic decisions, workflow preferences, or debugging/failure lessons that should be saved for future sessions.

Strictly return a JSON array of objects. Each object MUST have:
1. "content": (string) The specific memory detail, preference, or project decision. Keep it factual and clear.
2. "importance": (float) A score from 0.0 to 1.0 representing how critical this is (e.g., 0.9 for breaking workflow decisions, 0.4 for minor styling pref).
3. "memory_type": (string) Must be one of: "semantic", "episodic", "procedural", "negative".
4. "tags": (array of strings) 2-4 keywords representing the topic (e.g. ["golang", "concurrency", "test-failure"]).

If there is nothing worth remembering, return an empty JSON array: [].
Do NOT wrap your JSON in markdown codeblocks (such as ` + "`" + "`" + "`" + `json...` + "`" + "`" + "`" + `), return ONLY the raw JSON string.`

// Extract 提取消息历史中的候选记忆
func (e *DefaultMemoryExtractor) Extract(ctx context.Context, in contract.ExtractInput) ([]*domain.LongTermEntry, error) {
	if e.llmClient == nil {
		return nil, fmt.Errorf("llm client is nil in DefaultMemoryExtractor")
	}

	// 1. 过滤消息
	var filteredLines []string
	for _, m := range in.Messages {
		if m == nil {
			continue
		}
		// 忽略临时注入和 reminder
		if m.NormalizedKind() == domain.MessageKindSkillInjection || m.NormalizedKind() == domain.MessageKindReminder {
			continue
		}

		role := string(m.Role)
		content := m.Content

		// 对超大 tool_result 做截断只留预览，防止干扰
		if m.NormalizedKind() == domain.MessageKindToolResult {
			role = "tool_result"
			if len(content) > 400 {
				content = fmt.Sprintf("[Truncated Tool Output Preview: %s...]", content[:400])
			}
		}

		filteredLines = append(filteredLines, fmt.Sprintf("[%s]: %s", role, content))
	}

	if len(filteredLines) == 0 {
		return nil, nil
	}

	chatContent := strings.Join(filteredLines, "\n")
	promptContent := fmt.Sprintf("Please extract memories from this conversation segment:\n\n%s\n", chatContent)

	llmMsgs := []*domain.Message{
		domain.NewSystemMessage(stageOneSystemPrompt),
		domain.NewUserMessage(promptContent),
	}

	// 2. 调用大模型提炼
	resp, err := e.llmClient.Generate(ctx, llmMsgs, nil)
	if err != nil {
		return nil, fmt.Errorf("llm memory extraction failed: %w", err)
	}

	cleanedResp := strings.TrimSpace(resp.Content)
	// 剥离大模型可能包裹的 markdown 标记
	cleanedResp = strings.TrimPrefix(cleanedResp, "```json")
	cleanedResp = strings.TrimPrefix(cleanedResp, "```")
	cleanedResp = strings.TrimSuffix(cleanedResp, "```")
	cleanedResp = strings.TrimSpace(cleanedResp)

	if cleanedResp == "" || cleanedResp == "[]" {
		return nil, nil
	}

	var rawEntries []rawEntry
	if err := json.Unmarshal([]byte(cleanedResp), &rawEntries); err != nil {
		return nil, fmt.Errorf("unmarshal extracted memories failed: %w (raw response: %s)", err, cleanedResp)
	}

	// 3. 封装为领域模型 LongTermEntry
	var results []*domain.LongTermEntry
	for _, raw := range rawEntries {
		if strings.TrimSpace(raw.Content) == "" {
			continue
		}

		mType := domain.MemoryTypeSemantic
		switch raw.MemoryType {
		case "episodic":
			mType = domain.MemoryTypeEpisodic
		case "procedural":
			mType = domain.MemoryTypeProcedural
		case "negative":
			mType = domain.MemoryTypeNegative
		}

		entry := &domain.LongTermEntry{
			ID:               fmt.Sprintf("ltm-%d", time.Now().UnixNano()),
			TenantID:         in.SessionRef.TenantID,
			Scope: domain.MemoryScope{
				Type: domain.MemoryScopeType(in.SessionRef.AppID), // 默认为 AppID 或是 user 级别隔离
				ID:   in.SessionRef.SessionID,
			},
			MemoryType:       mType,
			Content:          raw.Content,
			Importance:       raw.Importance,
			Confidence:       0.8, // 初始默认置信度
			Status:           "active",
			SensitivityLevel: "internal",
			DecayPolicy:      "time_decay",
			Tags:             raw.Tags,
			LastAccessedAt:   time.Now(),
		}

		// 自适应把 Scope 的默认映射修正为合理的值：
		// 如果 ref.UserID 存在，则设为 User Scope
		if in.SessionRef.UserID != "" {
			entry.Scope = domain.MemoryScope{
				Type: domain.MemoryScopeUser,
				ID:   in.SessionRef.UserID,
			}
		} else {
			entry.Scope = domain.MemoryScope{
				Type: domain.MemoryScopeWorkspace,
				ID:   in.SessionRef.SessionID,
			}
		}

		results = append(results, entry)
	}

	return results, nil
}
