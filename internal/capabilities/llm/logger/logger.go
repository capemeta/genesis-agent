// Package llmlogger 提供全量 LLM 交互日志拦截器与记录模型
package llmlogger

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"genesis-agent/internal/capabilities/llm/contract"
	"genesis-agent/internal/capabilities/tool/contract"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/contextutil"
	"genesis-agent/internal/platform/logger"
)

// ToolLogItem 专用于日志调用的 Tool 序列化结构（剔除 DescriptionFunc 等不可 JSON 化的函数）
type ToolLogItem struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Parameters  *tool.ParameterSchema `json:"parameters,omitempty"`
}

// LLMCallRecord 表示单次 LLM 调用的完整输入、输出、参数与元数据记录
type LLMCallRecord struct {
	Timestamp  string            `json:"timestamp"`
	RunID      string            `json:"run_id,omitempty"`
	SessionID  string            `json:"session_id,omitempty"`
	DurationMS int64             `json:"duration_ms"`
	CallType   string            `json:"call_type"`
	Model      string            `json:"model"`
	Parameters map[string]any    `json:"parameters,omitempty"`
	Messages   []*domain.Message `json:"messages"`
	Tools      []ToolLogItem     `json:"tools,omitempty"`
	Response   *domain.Message   `json:"response,omitempty"`
	Error      string            `json:"error,omitempty"`
}

type loggingChatModel struct {
	inner  llm.ChatModel
	writer io.Writer
	params map[string]any
	mu     sync.Mutex
}

// Wrap 包装 ChatModel 实例，使其在每轮 Generate / StreamGenerate 时全量记录交互日志到 llm.log
func Wrap(model llm.ChatModel, writer io.Writer, params map[string]any) llm.ChatModel {
	if model == nil {
		return nil
	}
	if _, ok := model.(*loggingChatModel); ok {
		return model
	}
	return &loggingChatModel{
		inner:  model,
		writer: writer,
		params: params,
	}
}

func (m *loggingChatModel) GetModelName() string {
	return m.inner.GetModelName()
}

func (m *loggingChatModel) getWriter() (io.Writer, bool) {
	if m.writer != nil {
		return m.writer, false
	}
	if w := logger.GetGlobalLLMWriter(); w != nil {
		return w, false
	}
	logDir := filepath.Join(".", ".genesis", "logs")
	if fi, err := os.Stat(logDir); err == nil && fi.IsDir() {
		f, err := os.OpenFile(filepath.Join(logDir, "llm.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			return f, true
		}
	}
	return nil, false
}

func (m *loggingChatModel) logCall(ctx context.Context, callType string, start time.Time, messages []*domain.Message, tools []*tool.Info, resp *domain.Message, err error) {
	w, needsClose := m.getWriter()
	if w == nil {
		return
	}
	if needsClose {
		if c, ok := w.(io.Closer); ok {
			defer c.Close()
		}
	}

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	var toolLogItems []ToolLogItem
	if len(tools) > 0 {
		toolLogItems = make([]ToolLogItem, 0, len(tools))
		for _, t := range tools {
			if t == nil {
				continue
			}
			toolLogItems = append(toolLogItems, ToolLogItem{
				Name:        t.Name,
				Description: tool.ResolveDescription(context.Background(), t),
				Parameters:  t.Parameters,
			})
		}
	}

	runID, sessionID := contextutil.CorrelationIDs(ctx)

	rec := LLMCallRecord{
		Timestamp:  start.Format(time.RFC3339Nano),
		RunID:      runID,
		SessionID:  sessionID,
		DurationMS: time.Since(start).Milliseconds(),
		CallType:   callType,
		Model:      m.inner.GetModelName(),
		Parameters: m.params,
		Messages:   sanitizeMessagesForLog(messages),
		Tools:      toolLogItems,
		Response:   sanitizeMessageForLog(resp),
		Error:      errMsg,
	}

	data, errMarshal := json.Marshal(rec)
	if errMarshal != nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = w.Write(append(data, '\n'))
}

func sanitizeMessageForLog(m *domain.Message) *domain.Message {
	if m == nil {
		return nil
	}
	clone := *m
	if len(clone.Parts) > 0 && !clone.HasImageParts() {
		clone.Parts = nil
	}
	return &clone
}

func sanitizeMessagesForLog(msgs []*domain.Message) []*domain.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]*domain.Message, len(msgs))
	for i, m := range msgs {
		out[i] = sanitizeMessageForLog(m)
	}
	return out
}

func (m *loggingChatModel) Generate(ctx context.Context, messages []*domain.Message, tools []*tool.Info) (*domain.Message, error) {
	start := time.Now()
	resp, err := m.inner.Generate(ctx, messages, tools)
	m.logCall(ctx, "Generate", start, messages, tools, resp, err)
	return resp, err
}

func (m *loggingChatModel) StreamGenerate(ctx context.Context, messages []*domain.Message, tools []*tool.Info, onDelta func(delta string, isThought bool)) (*domain.Message, error) {
	start := time.Now()
	resp, err := m.inner.StreamGenerate(ctx, messages, tools, onDelta)
	m.logCall(ctx, "StreamGenerate", start, messages, tools, resp, err)
	return resp, err
}
