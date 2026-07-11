package chat

import (
	"testing"

	"genesis-agent/internal/runtime/progress"
)

func TestApplyProgressDedupsConsecutiveSummaries(t *testing.T) {
	m := &Model{loading: true}
	display := true
	ev := progress.Event{
		Kind:    progress.KindTool,
		Phase:   progress.PhaseStart,
		Name:    "search_skill_resources",
		Summary: "调用工具: search_skill_resources",
		Display: &display,
	}
	m.applyProgress(ev)
	m.applyProgress(ev)
	if len(m.progressLog) != 1 {
		t.Fatalf("expected 1 log line after dedup, got %d: %#v", len(m.progressLog), m.progressLog)
	}
}

func TestApplyProgressShowsAssistantDraft(t *testing.T) {
	m := &Model{loading: true, messages: []uiMessage{{role: "user", content: "hi"}}}
	display := true
	m.applyProgress(progress.Event{
		Kind:      progress.KindLLM,
		Phase:     progress.PhaseStart,
		BlockType: "assistant_draft",
		Display:   &display,
		Summary:   "思考中",
	})
	m.applyProgress(progress.Event{
		Kind:      progress.KindLLM,
		Phase:     progress.PhaseProgress,
		BlockType: "assistant_draft",
		Display:   &display,
		Detail:    "中间思考A",
	})
	if len(m.messages) != 2 || m.messages[1].content != "中间思考A" {
		t.Fatalf("draft should appear in assistant bubble: %#v", m.messages)
	}
}

func TestApplyProgressIgnoresHiddenDraft(t *testing.T) {
	m := &Model{loading: true, messages: []uiMessage{{role: "user", content: "hi"}}}
	displayFalse := false
	m.applyProgress(progress.Event{
		Kind:      progress.KindLLM,
		Phase:     progress.PhaseProgress,
		BlockType: "assistant_draft",
		Display:   &displayFalse,
		Detail:    "should-not-appear",
	})
	if len(m.messages) != 1 {
		t.Fatalf("hidden draft must not create assistant bubble: %#v", m.messages)
	}
}

func TestAppendStreamDeltaCumulative(t *testing.T) {
	got := appendStreamDelta("文件路径", "文件路径未正确")
	if got != "文件路径未正确" {
		t.Fatalf("cumulative merge failed: %q", got)
	}
	got = appendStreamDelta("abc", "def")
	if got != "abcdef" {
		t.Fatalf("incremental merge failed: %q", got)
	}
	got = appendStreamDelta("hello world", "world")
	if got != "hello world" {
		t.Fatalf("suffix dedup failed: %q", got)
	}
}

func TestApplyProgressFinalAnswerKeepsThoughts(t *testing.T) {
	m := &Model{loading: true, messages: []uiMessage{
		{role: "user", content: "hi"},
		{role: "assistant", content: "中间思考"},
	}}
	display := true
	m.applyProgress(progress.Event{
		Kind:      progress.KindRun,
		Phase:     progress.PhaseStart,
		BlockType: "final_answer",
		Display:   &display,
	})
	m.applyProgress(progress.Event{
		Kind:      progress.KindRun,
		Phase:     progress.PhaseProgress,
		BlockType: "final_answer",
		Display:   &display,
		Detail:    "最终回答",
	})
	want := "中间思考\n\n—— 最终回答 ——\n\n最终回答"
	if m.messages[1].content != want {
		t.Fatalf("got %q want %q", m.messages[1].content, want)
	}
}
