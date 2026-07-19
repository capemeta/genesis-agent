package collab

import (
	"strings"
	"testing"
)

func TestFilterToolNamesPlanMode(t *testing.T) {
	names := []string{
		"read_file", "write_file", "todo_write", "run_command",
		"write_implementation_plan", "exit_plan_mode", "enter_plan_mode", "Task",
	}
	got := FilterToolNames(ModePlan, 0, names)
	joined := strings.Join(got, ",")
	for _, want := range []string{"read_file", "write_implementation_plan", "exit_plan_mode", "Task"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, got)
		}
	}
	for _, ban := range []string{"write_file", "todo_write", "run_command", "enter_plan_mode"} {
		if strings.Contains(joined, ban) {
			t.Fatalf("should ban %s: %v", ban, got)
		}
	}
}

func TestFilterToolNamesSubagentBansEnterExit(t *testing.T) {
	names := []string{"read_file", "exit_plan_mode", "enter_plan_mode", "Task"}
	got := FilterToolNames(ModePlan, 1, names)
	for _, n := range got {
		if n == "enter_plan_mode" || n == "exit_plan_mode" {
			t.Fatalf("subagent must not see %s", n)
		}
	}
}

func TestFilterToolNamesDefaultHidesPlanTools(t *testing.T) {
	got := FilterToolNames(ModeDefault, 0, []string{"todo_write", "write_implementation_plan", "exit_plan_mode", "enter_plan_mode"})
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "todo_write") || !strings.Contains(joined, "enter_plan_mode") {
		t.Fatalf("default should allow todo/enter: %v", got)
	}
	if strings.Contains(joined, "write_implementation_plan") || strings.Contains(joined, "exit_plan_mode") {
		t.Fatalf("default should hide plan tools: %v", got)
	}
}

func TestPlanDocumentRelPath(t *testing.T) {
	p := PlanDocumentRelPath("abc/../x")
	if !strings.HasPrefix(p, ".genesis/plans/") || !strings.HasSuffix(p, ".md") {
		t.Fatalf("path=%q", p)
	}
	if strings.Contains(p, "..") {
		t.Fatalf("unsafe path: %q", p)
	}
}
