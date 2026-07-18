package react

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	approvalcontract "genesis-agent/internal/capabilities/approval/contract"
	tool "genesis-agent/internal/capabilities/tool/contract"
	traceadapter "genesis-agent/internal/capabilities/trace/adapter"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
)

// TestToolFailureContentPreservesJSON：对齐 Codex format_exec_output_for_model /
// Kode renderResultForAssistant——失败时仍把 stdout/结构化正文交给模型。
func TestToolFailureContentPreservesJSON(t *testing.T) {
	jsonBody := `{"ok":false,"failure_kind":"dependency_missing","suggested_action":"install_then_retry"}`
	got := toolFailureContent(jsonBody, fmt.Errorf("run_skill_command failed"))
	if !strings.Contains(got, `"ok":false`) || !strings.Contains(got, `dependency_missing`) {
		t.Fatalf("content discarded json: %q", got)
	}
	if !strings.Contains(got, "工具执行失败:") {
		t.Fatalf("expected failure prefix, got %q", got)
	}
}

func TestToolFailureContentEmptyOutputFallsBackToError(t *testing.T) {
	got := toolFailureContent("", fmt.Errorf("boom"))
	if got != "工具执行失败: boom" {
		t.Fatalf("got=%q", got)
	}
}

func TestExecuteOneToolCallPreservesJSONOnError(t *testing.T) {
	jsonBody := `{"ok":false,"failure_kind":"dependency_missing"}`
	e := &ReactLoopEngine{
		registry: failingJSONRegistry{output: jsonBody, err: fmt.Errorf("run_skill_command failed")},
		logger:   logger.NewNop(),
		tracer:   traceadapter.NewNopTracer(),
	}
	rc := runtime.NewRunContext(&domain.Run{ID: "run-1"}, &domain.Agent{})
	got, err := e.executeOneToolCall(context.Background(), rc, domain.ToolCall{
		ID:       "c1",
		Function: domain.FunctionCall{Name: "run_skill_command", Arguments: `{}`},
	}, logger.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Content, `"ok":false`) || !strings.Contains(got.Content, `dependency_missing`) {
		t.Fatalf("content discarded json: %q", got.Content)
	}
}

func TestExecuteToolCallsPropagatesRunAbort(t *testing.T) {
	e := &ReactLoopEngine{
		registry: failingJSONRegistry{err: approvalcontract.ErrRunAborted},
		logger:   logger.NewNop(),
		tracer:   traceadapter.NewNopTracer(),
	}
	rc := runtime.NewRunContext(&domain.Run{ID: "run-abort"}, &domain.Agent{})
	_, err := e.executeToolCalls(context.Background(), rc, []domain.ToolCall{{ID: "c1", Function: domain.FunctionCall{Name: "run_command", Arguments: `{}`}}}, logger.NewNop())
	if !errors.Is(err, approvalcontract.ErrRunAborted) {
		t.Fatalf("err = %v, want ErrRunAborted", err)
	}
}

type failingJSONRegistry struct {
	output string
	err    error
}

func (f failingJSONRegistry) Register(tool.Tool) error { return nil }
func (f failingJSONRegistry) Replace(string, string, tool.Tool) error {
	return errors.New("unsupported")
}
func (f failingJSONRegistry) Owner(string) (string, bool) { return "", false }
func (f failingJSONRegistry) Unregister(string)           {}
func (f failingJSONRegistry) Get(string) tool.Tool {
	return nil
}
func (f failingJSONRegistry) Execute(context.Context, string, string) (string, error) {
	return f.output, f.err
}
func (f failingJSONRegistry) ListInfos() []*tool.Info           { return nil }
func (f failingJSONRegistry) FilterInfos([]string) []*tool.Info { return nil }
func (f failingJSONRegistry) Names() []string                   { return nil }
