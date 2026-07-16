package chat

import (
	"strings"
	"testing"

	"genesis-agent/internal/runtime/progress"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func TestLoadingArrowKeysScrollViewport(t *testing.T) {
	m := Model{loading: true, viewport: viewport.New(40, 4)}
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()
	before := m.viewport.YOffset
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	got := updated.(Model)
	if got.viewport.YOffset >= before {
		t.Fatalf("up should scroll during loading: before=%d after=%d", before, got.viewport.YOffset)
	}
}

func TestIdleArrowKeysScrollViewportWhenComposerEmpty(t *testing.T) {
	m := Model{viewport: viewport.New(40, 4), historyIdx: -1}
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()
	before := m.viewport.YOffset
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	got := updated.(Model)
	if got.viewport.YOffset >= before {
		t.Fatalf("up should scroll with an empty composer: before=%d after=%d", before, got.viewport.YOffset)
	}
}

func TestMouseWheelScrollsViewport(t *testing.T) {
	m := Model{viewport: viewport.New(40, 4), historyIdx: -1}
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()
	before := m.viewport.YOffset
	updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.viewport.YOffset >= before {
		t.Fatalf("mouse wheel should scroll: before=%d after=%d", before, got.viewport.YOffset)
	}
}

func TestProgressRefreshDoesNotForceTailAfterUserScroll(t *testing.T) {
	m := Model{loading: true, width: 40, viewport: viewport.New(40, 4), progressCh: make(chan progressMsg, 1)}
	m.messages = []uiMessage{{role: "system", content: strings.Repeat("history\n", 20)}}
	m.progressLog = []string{"first"}
	m.refreshViewportContent()
	m.viewport.GotoBottom()
	m.viewport.LineUp(2)
	before := m.viewport.YOffset
	display := true
	updated, _ := m.Update(progressMsg{event: progress.Event{Kind: progress.KindTool, Phase: progress.PhaseStart, Name: "read_file", Summary: "调用工具: read_file", Display: &display}})
	got := updated.(Model)
	if got.viewport.YOffset != before {
		t.Fatalf("progress refresh forced scroll: before=%d after=%d", before, got.viewport.YOffset)
	}
}

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

func TestApplyProgressReplacesByCallID(t *testing.T) {
	m := &Model{loading: true}
	display := true

	// 1. Tool Input Start -> Appends to progressLog
	m.applyProgress(progress.Event{
		Kind:      progress.KindTool,
		Phase:     progress.PhaseStart,
		CallID:    "call-1",
		Name:      "write_file",
		Summary:   "调用工具: write_file (路径: a.txt)",
		BlockType: "tool_input",
		Display:   &display,
	})

	if len(m.progressLog) != 1 || m.progressLog[0] != "调用工具: write_file (路径: a.txt)" {
		t.Fatalf("expected initial log line, got: %#v", m.progressLog)
	}

	// 2. Tool Result Start -> Replaces the first entry using CallID
	m.applyProgress(progress.Event{
		Kind:      progress.KindTool,
		Phase:     progress.PhaseStart,
		CallID:    "call-1",
		Name:      "write_file",
		Summary:   "执行工具: write_file",
		BlockType: "tool_result",
		Display:   &display,
	})

	if len(m.progressLog) != 1 || m.progressLog[0] != "执行工具: write_file" {
		t.Fatalf("expected replaced log line for tool start, got: %#v", m.progressLog)
	}

	// 3. Tool Result Complete -> Replaces again using CallID
	m.applyProgress(progress.Event{
		Kind:      progress.KindTool,
		Phase:     progress.PhaseComplete,
		CallID:    "call-1",
		Name:      "write_file",
		Summary:   "工具执行完成: write_file (路径: a.txt)",
		BlockType: "tool_result",
		Display:   &display,
	})

	if len(m.progressLog) != 1 || m.progressLog[0] != "工具执行完成: write_file (路径: a.txt)" {
		t.Fatalf("expected finally replaced log line for tool complete, got: %#v", m.progressLog)
	}
}
