// Package progress 定义 Agent 运行过程中的结构化进度事件。
package progress

import (
	"context"
	"time"
)

// Kind 描述进度事件类型。
type Kind string

const (
	KindRun     Kind = "run"
	KindLLM     Kind = "llm"
	KindTool    Kind = "tool"
	KindSkill   Kind = "skill"
	KindSandbox Kind = "sandbox"
	KindFile    Kind = "file"
	KindSystem  Kind = "system"
)

// Phase 描述一个动作的生命周期。
type Phase string

const (
	PhaseStart    Phase = "start"
	PhaseProgress Phase = "progress"
	PhaseComplete Phase = "complete"
	PhaseError    Phase = "error"
)

// Level 描述展示重要性。
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Event 是跨 CLI、Desktop、Enterprise 的统一运行进度事件。
type Event struct {
	Kind        Kind              `json:"kind"`
	Phase       Phase             `json:"phase"`
	Level       Level             `json:"level,omitempty"`
	RunID       string            `json:"run_id,omitempty"`
	StepID      string            `json:"step_id,omitempty"`
	CallID      string            `json:"call_id,omitempty"`
	Component   string            `json:"component,omitempty"`
	Name        string            `json:"name,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Detail      string            `json:"detail,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Time        time.Time         `json:"time"`
	BlockIndex  *int              `json:"block_index,omitempty"`
	BlockType   string            `json:"block_type,omitempty"`
	StepIndex   *int              `json:"step_index,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	StopReason  string            `json:"stop_reason,omitempty"`
	DeltaType   string            `json:"delta_type,omitempty"`
	Display     *bool             `json:"display,omitempty"`
}

// Sink 接收进度事件。实现必须快速返回，不能阻塞主执行流程。
type Sink func(Event)

type sinkKey struct{}

// WithSink 把进度事件接收器放入 context。
func WithSink(ctx context.Context, sink Sink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, sinkKey{}, sink)
}

// FromContext 取出进度事件接收器。
func FromContext(ctx context.Context) Sink {
	if ctx == nil {
		return nil
	}
	sink, _ := ctx.Value(sinkKey{}).(Sink)
	return sink
}

// Emit 发送事件；没有 sink 时为空操作。
func Emit(ctx context.Context, event Event) {
	sink := FromContext(ctx)
	if sink == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	if event.Level == "" {
		event.Level = LevelInfo
	}
	defer func() {
		_ = recover()
	}()
	sink(event)
}
