package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestCompleteSlashCommand(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	m.textarea.SetValue("/co")

	m = m.completeSlashCommand()

	if got, want := m.textarea.Value(), "/copy "; got != want {
		t.Fatalf("completion = %q, want %q", got, want)
	}
	m = m.completeSlashCommand()
	if got, want := m.textarea.Value(), "/copy all"; got != want {
		t.Fatalf("second completion = %q, want %q", got, want)
	}
}

func TestSlashCommandMenuOpensAndAppliesSelection(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	m.textarea.SetValue(" /")
	m.syncCommandMenu()
	if !m.commandMenuOpen {
		t.Fatal("typing a slash command prefix should open the menu")
	}
	if got, want := m.commandMenuHeight(), 6; got != want {
		t.Fatalf("menu height = %d, want %d", got, want)
	}

	m = m.moveCommandMenu(1)
	m = m.applyCommandMenuSelection()
	if got, want := m.textarea.Value(), "/copy "; got != want {
		t.Fatalf("selection = %q, want %q", got, want)
	}
	if m.commandMenuOpen {
		t.Fatal("applying a command should close the menu")
	}
}

func TestSlashCommandMenuInterceptsArrowAndEnter(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	m.textarea.SetValue("/")
	m.syncCommandMenu()

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	selected := model.(Model)
	if selected.commandMenuIndex != 1 {
		t.Fatalf("menu index = %d, want 1", selected.commandMenuIndex)
	}
	model, _ = selected.Update(tea.KeyMsg{Type: tea.KeyEnter})
	applied := model.(Model)
	if got, want := applied.textarea.Value(), "/copy "; got != want {
		t.Fatalf("selection = %q, want %q", got, want)
	}
}

func TestSelectModeMovesAcrossConversationMessages(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	m.messages = []uiMessage{
		{role: "system", content: "欢迎"},
		{role: "user", content: "问题"},
		{role: "assistant", content: "回答"},
	}

	model, _ := m.enterSelectMode()
	selected := model.(Model)
	if !selected.selectMode || selected.selectCursor != 1 || selected.selectAnchor != 1 {
		t.Fatalf("unexpected initial selection: %+v", selected)
	}

	selected = selected.moveSelection(-1)
	if selected.selectCursor != 0 {
		t.Fatalf("cursor = %d, want 0", selected.selectCursor)
	}
	if marker := renderSelectionMarker(1, selected.selectableMessageIndexes(), true, selected.selectAnchor, selected.selectCursor); marker == "" {
		t.Fatal("selected message should have a marker")
	}
}

func TestCtrlDQuitsTUI(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if model == nil || cmd == nil {
		t.Fatal("Ctrl+D should return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("command result = %T, want tea.QuitMsg", cmd())
	}
}

func TestRunningCtrlCCancelsWithoutQuit(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	m.loading = true

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	updated := model.(Model)
	if updated.loading {
		t.Fatal("Ctrl+C should leave the model idle")
	}
	if cmd == nil {
		t.Fatal("Ctrl+C should schedule a toast clear command")
	}
}

func TestQuitSlashCommandReturnsQuit(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	model, cmd := m.handleSlashCmd("/quit")
	if model == nil || cmd == nil {
		t.Fatal("/quit should return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("command result = %T, want tea.QuitMsg", cmd())
	}
}

func TestCopyWithoutAssistantShowsToast(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	model, _ := m.copyLastAssistant()
	if got, want := model.(Model).toast, "暂无可复制的回答"; got != want {
		t.Fatalf("toast = %q, want %q", got, want)
	}
}

func TestLastMessageContentSelectsLatestAssistant(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	m.messages = []uiMessage{
		{role: "assistant", content: "旧回答"},
		{role: "user", content: "追问"},
		{role: "assistant", content: "  新回答  "},
	}
	got, ok := m.lastMessageContent("assistant")
	if !ok || got != "新回答" {
		t.Fatalf("content = %q, ok = %t", got, ok)
	}
}

func TestWindowSizeCalculatesViewportHeight(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	updated := model.(Model)
	if got, want := updated.viewport.Height, 30; got != want {
		t.Fatalf("viewport height = %d, want %d", got, want)
	}
}

func TestActivitySummaryIncludesCompletionMetadata(t *testing.T) {
	got := activitySummary(uiMessage{
		progressLog:     []string{"调用工具"},
		activityTokens:  42,
		activityElapsed: 1500 * time.Millisecond,
		activityOutcome: "完成",
	})
	for _, part := range []string{"1 步", "42 tokens", "1.5s", "完成"} {
		if !strings.Contains(got, part) {
			t.Fatalf("summary %q does not contain %q", got, part)
		}
	}
}

func TestHelpOverlayClosesWithEsc(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	model, _ := m.showHelpOverlay()
	opened := model.(Model)
	if !opened.helpOverlay || opened.textarea.Focused() {
		t.Fatal("help overlay should blur the composer")
	}

	model, _ = opened.Update(tea.KeyMsg{Type: tea.KeyEsc})
	closed := model.(Model)
	if closed.helpOverlay || !closed.textarea.Focused() {
		t.Fatal("Esc should close the help overlay and focus the composer")
	}
}

func TestHeaderViewStaysWithinNarrowTerminal(t *testing.T) {
	m := NewModel(context.Background(), nil, nil)
	for _, width := range []int{10, 20} {
		m.width = width
		header := m.headerView()
		for _, line := range strings.Split(header, "\n") {
			if got := lipgloss.Width(line); got > m.width {
				t.Fatalf("header line width = %d, terminal width = %d", got, m.width)
			}
		}
	}
}
