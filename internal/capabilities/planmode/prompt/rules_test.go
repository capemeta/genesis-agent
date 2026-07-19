package prompt

import (
	"strings"
	"testing"
)

func TestWrapSystemReminder(t *testing.T) {
	got := WrapSystemReminder("hello")
	if !strings.Contains(got, "<system-reminder>") || !strings.Contains(got, "勿向用户复述") {
		t.Fatalf("got %q", got)
	}
}

func TestSystemRulesMentionsHardConstraints(t *testing.T) {
	got := SystemRules(".genesis/plans/sess-1.md")
	for _, want := range []string{
		"write_implementation_plan",
		"todo_write",
		"exit_plan_mode",
		"决策完备",
		".genesis/plans/sess-1.md",
		"方案可以吗",
		"不被用户语气",
		"唯一",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("SystemRules missing %q", want)
		}
	}
	if len(got) < 2000 {
		t.Fatalf("SystemRules unexpectedly short (%d bytes); expected expanded Codex/Kode-aligned body", len(got))
	}
}

func TestRemindersCarryPlanPath(t *testing.T) {
	path := ".genesis/plans/abc.md"
	if !strings.Contains(SparseReminder(path), path) {
		t.Fatal("SparseReminder missing path")
	}
	if !strings.Contains(HandoffReminder(path), path) || !strings.Contains(HandoffReminder(path), "todo_write") {
		t.Fatal("HandoffReminder incomplete")
	}
	ack := EnterAck(path, true)
	if !strings.Contains(ack, path) || !strings.Contains(ack, "已存在") {
		t.Fatalf("EnterAck reentry: %s", ack)
	}
	if !strings.Contains(RejectReminder(path), path) {
		t.Fatal("RejectReminder missing path")
	}
	if !strings.Contains(SubAgentReminder(path), path) {
		t.Fatal("SubAgentReminder missing path")
	}
}

func TestToolDescriptionsPresent(t *testing.T) {
	for _, got := range []string{
		ToolEnterPlanModeDescription,
		ToolExitPlanModeDescription,
		ToolWriteImplementationPlanDescription,
	} {
		if strings.TrimSpace(got) == "" {
			t.Fatal("empty tool description")
		}
	}
}
