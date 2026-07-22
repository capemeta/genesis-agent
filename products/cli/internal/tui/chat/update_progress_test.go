package chat

import (
	"context"
	"strings"
	"testing"

	"genesis-agent/internal/runtime/progress"
	"github.com/charmbracelet/bubbles/textarea"
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

func TestNewModelProgressExpandedByDefault(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	if !m.progressExpanded {
		t.Fatal("expected progressExpanded to be true by default in NewModel")
	}
}

func TestExpandProgressScrollsToBottom(t *testing.T) {
	m := Model{viewport: viewport.New(40, 4), historyIdx: -1, width: 40, progressExpanded: false}
	m.messages = []uiMessage{{
		role:        "system",
		isProgress:  true,
		progressLog: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"},
	}}
	m.refreshViewportContent()
	m.viewport.SetYOffset(0)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	got := updated.(Model)
	if !got.progressExpanded {
		t.Fatal("expected progress to expand")
	}
	if !got.viewport.AtBottom() {
		t.Fatalf("expand should scroll to bottom: offset=%d", got.viewport.YOffset)
	}
}

func TestPageKeysScrollViewportEvenWithComposerText(t *testing.T) {
	ta := textarea.New()
	ta.SetValue("draft")
	m := Model{viewport: viewport.New(40, 4), textarea: ta, historyIdx: -1}
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()
	before := m.viewport.YOffset

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	got := updated.(Model)
	if got.viewport.YOffset >= before {
		t.Fatalf("pgup should scroll with composer text: before=%d after=%d", before, got.viewport.YOffset)
	}

	mid := got.viewport.YOffset
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	got = updated.(Model)
	if got.viewport.YOffset <= mid {
		t.Fatalf("pgdn should scroll down: before=%d after=%d", mid, got.viewport.YOffset)
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

func TestProgressSummaryDisplaysRunSkillPhaseTimings(t *testing.T) {
	got := progressSummary(progress.Event{
		Kind:     progress.KindTool,
		Phase:    progress.PhaseComplete,
		Name:     "run_skill_command",
		Detail:   `{"command":"node index.js"}`,
		Metadata: map[string]string{"duration_ms": "3749", "approval_duration_ms": "2000", "staging_duration_ms": "500", "execution_duration_ms": "1100"},
	})
	for _, want := range []string{"node index.js", "总计 3.7s", "审批 2.0s", "staging 500ms", "执行 1.1s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("TUI 耗时摘要缺少 %q: %s", want, got)
		}
	}
}

func TestApplyProgressSubagentTagPreservedOnReplace(t *testing.T) {
	m := Model{}
	display := true

	// 1. Subagent Tool Input Start
	m.applyProgress(progress.Event{
		Kind:        progress.KindTool,
		Phase:       progress.PhaseStart,
		CallID:      "call-sub-1",
		SubAgentID:  "skill-fork:office-ppt",
		Depth:       1,
		Name:        "run_skill_command",
		Detail:      `{"command":"node build.js"}`,
		BlockType:   "tool_input",
		Display:     &display,
	})

	if len(m.progressLog) != 1 || !strings.HasPrefix(m.progressLog[0], "[Sub-Agent: skill-fork:office-ppt]") {
		t.Fatalf("expected subagent tag prefix, got: %#v", m.progressLog)
	}

	// 2. Tool Result Complete -> Replaces CallID and retains subagent tag
	m.applyProgress(progress.Event{
		Kind:        progress.KindTool,
		Phase:       progress.PhaseComplete,
		CallID:      "call-sub-1",
		SubAgentID:  "skill-fork:office-ppt",
		Depth:       1,
		Name:        "run_skill_command",
		Detail:      `{"command":"node build.js"}`,
		BlockType:   "tool_result",
		Display:     &display,
	})

	if len(m.progressLog) != 1 || !strings.HasPrefix(m.progressLog[0], "[Sub-Agent: skill-fork:office-ppt]") {
		t.Fatalf("expected subagent tag prefix preserved on replace, got: %#v", m.progressLog)
	}
}

func TestApplyProgressSubagentThinkingInPlaceStreaming(t *testing.T) {
	m := Model{}
	subAgentID := "skill-fork:office-ppt"
	
	// Stream 3 thinking delta tokens
	for _, chunk := range []string{"the input", " file to", " understand"} {
		m.applyProgress(progress.Event{
			Kind:       progress.KindLLM,
			Phase:      progress.PhaseProgress,
			SubAgentID: subAgentID,
			Depth:      1,
			BlockType:  "thinking",
			Detail:     chunk,
		})
	}

	// Verify that all 3 delta tokens were updated IN-PLACE into a single progressLog line
	if len(m.progressLog) != 1 {
		t.Fatalf("expected exactly 1 progressLog line for thinking stream, got %d: %#v", len(m.progressLog), m.progressLog)
	}
	wantPrefix := "[Sub-Agent: skill-fork:office-ppt] 思考: the input file to understand"
	if m.progressLog[0] != wantPrefix {
		t.Fatalf("expected in-place accumulated thinking line %q, got %q", wantPrefix, m.progressLog[0])
	}
}
func TestApplyProgressPreservesChronologicalOrderWithInterleavedThinking(t *testing.T) {
	m := &Model{loading: true}
	display := true

	// 1. Tool 1 Start
	m.applyProgress(progress.Event{
		Kind: progress.KindTool, Phase: progress.PhaseStart, CallID: "call-1", Name: "run_skill_command", Detail: `{"command":"python -m markitdown"}`, Display: &display,
	})

	// 2. Interleaved Thinking
	m.applyProgress(progress.Event{
		Kind: progress.KindLLM, Phase: progress.PhaseProgress, SubAgentID: "Worker", Depth: 1, BlockType: "thinking", Detail: "Content QA looks good",
	})

	// 3. Tool 1 Complete (arrives after thinking)
	m.applyProgress(progress.Event{
		Kind: progress.KindTool, Phase: progress.PhaseComplete, CallID: "call-1", Name: "run_skill_command", Detail: `{"command":"python -m markitdown"}`, Display: &display,
	})

	if len(m.progressLog) != 3 {
		t.Fatalf("expected 3 chronological log lines, got %d: %#v", len(m.progressLog), m.progressLog)
	}
	// Verify that thinking remains at index 1 and completion appends at index 2 (timeline order preserved)
	if !strings.Contains(m.progressLog[1], "思考") {
		t.Fatalf("index 1 should be thinking line: %s", m.progressLog[1])
	}
	if !strings.Contains(m.progressLog[2], "工具执行完成") {
		t.Fatalf("index 2 should be completion line appended at end: %s", m.progressLog[2])
	}
}
