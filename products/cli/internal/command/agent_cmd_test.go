package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"genesis-agent/internal/runtime/multiagent/contract"
	"genesis-agent/internal/runtime/multiagent/model"
	clisubagent "genesis-agent/products/cli/internal/subagent"
)

func TestAgentShowLoadsProjectDefinition(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".genesis", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review.md"), []byte("---\nname: review\ndescription: review changes\ntools: [read_file]\n---\nReview the changes."), 0o644); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	cmd := newAgentShowCmd()
	cmd.SetArgs([]string{"review"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentTasksAndTaskShowStoredSubagents(t *testing.T) {
	workspace := t.TempDir()
	withWorkingDir(t, workspace)
	store, err := clisubagent.NewFileStore(workspace)
	if err != nil {
		t.Fatal(err)
	}
	value := contract.StoredInstance{Instance: model.Instance{
		AgentID:      "agent-demo",
		ParentRunID:  "parent",
		SessionID:    "session",
		SubagentType: "explore",
		Status:       model.StatusCompleted,
		Result:       &model.TaskResult{ResultID: "result-demo", Status: model.ResultStatusCompleted, Summary: "safe summary"},
	}}
	if err := store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}

	var listOut bytes.Buffer
	listCmd := newAgentTasksCmd()
	listCmd.SetOut(&listOut)
	if err := listCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut.String(), "agent-demo") || !strings.Contains(listOut.String(), "safe summary") {
		t.Fatalf("tasks output missing stored summary: %q", listOut.String())
	}

	var showOut bytes.Buffer
	showCmd := newAgentTaskCmd()
	showCmd.SetOut(&showOut)
	showCmd.SetArgs([]string{"agent-demo"})
	if err := showCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(showOut.String(), "ResultID: result-demo") || !strings.Contains(showOut.String(), "safe summary") {
		t.Fatalf("task output missing details: %q", showOut.String())
	}
}

func TestAgentCleanupRemovesOldTerminalRecords(t *testing.T) {
	workspace := t.TempDir()
	withWorkingDir(t, workspace)
	store, err := clisubagent.NewFileStore(workspace)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour)
	if err := store.Save(context.Background(), contract.StoredInstance{Instance: model.Instance{AgentID: "agent-old", Status: model.StatusCompleted, CreatedAt: old}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, ".genesis", "runtime", "subagents", "agent-old.json")
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newAgentCleanupCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--days", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "删除 1 条") {
		t.Fatalf("cleanup output mismatch: %q", out.String())
	}
	if _, err := store.Get(context.Background(), "agent-old"); err == nil {
		t.Fatal("expected old record to be removed")
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}
