package react

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	tool "genesis-agent/internal/capabilities/tool/contract"
	traceadapter "genesis-agent/internal/capabilities/trace/adapter"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/platform/logger"
	"genesis-agent/internal/runtime"
	"genesis-agent/internal/runtime/repeatguard"
)

func TestRunToolCallRepeatGuardBlocksThirdIdentical(t *testing.T) {
	reg := &countingFailRegistry{
		output: `{"ok":false,"failure_kind":"path_contract_violation","error":"bad"}`,
		err:    fmt.Errorf("path contract"),
	}
	e := &ReactLoopEngine{
		registry: reg,
		logger:   logger.NewNop(),
		tracer:   traceadapter.NewNopTracer(),
	}
	maxIdent := 2
	agent := &domain.Agent{RuntimePolicy: domain.RuntimePolicy{
		MaxIdenticalToolFailures: &maxIdent,
	}}
	rc := runtime.NewRunContext(&domain.Run{ID: "run-rg"}, agent)
	args := `{"script":"create_pptx.js","skill":"office-ppt"}`
	tc := domain.ToolCall{ID: "c1", Function: domain.FunctionCall{Name: "run_skill_command", Arguments: args}}

	for i := 0; i < 2; i++ {
		content, err := e.runToolCall(context.Background(), rc, tc, logger.NewNop())
		if err == nil {
			t.Fatalf("attempt %d expected tool err", i+1)
		}
		if !strings.Contains(content, "path_contract_violation") {
			t.Fatalf("attempt %d content=%q", i+1, content)
		}
	}
	if got := reg.calls.Load(); got != 2 {
		t.Fatalf("expected 2 executes, got %d", got)
	}

	content, err := e.runToolCall(context.Background(), rc, tc, logger.NewNop())
	if err != nil {
		t.Fatalf("block should return nil err, got %v", err)
	}
	if !strings.Contains(content, `"failure_kind":"repeated_failure"`) {
		t.Fatalf("expected repeated_failure, got %q", content)
	}
	if got := reg.calls.Load(); got != 2 {
		t.Fatalf("blocked call must not Execute, got %d", got)
	}
}

func TestApplyRepeatGuardProgressPartialComplete(t *testing.T) {
	e := &ReactLoopEngine{logger: logger.NewNop(), tracer: traceadapter.NewNopTracer()}
	maxStag := 1
	agent := &domain.Agent{RuntimePolicy: domain.RuntimePolicy{MaxStagnantIterations: &maxStag}}
	rc := runtime.NewRunContext(&domain.Run{ID: "run-pg"}, agent)

	stop, err := e.applyRepeatGuardProgress(context.Background(), rc, logger.NewNop(), false)
	if stop || err != nil {
		t.Fatalf("first stagnant should inject only, stop=%v err=%v", stop, err)
	}
	if len(rc.Messages) == 0 || !strings.Contains(rc.Messages[len(rc.Messages)-1].Content, "no_progress") {
		t.Fatalf("expected no_progress system message, msgs=%v", rc.Messages)
	}

	rc.RepeatGuard.BeginIteration()
	stop, err = e.applyRepeatGuardProgress(context.Background(), rc, logger.NewNop(), false)
	if !stop || err != nil {
		t.Fatalf("second stagnant should partial_complete, stop=%v err=%v", stop, err)
	}
	if !rc.Run.Incomplete || rc.Run.Status != domain.RunStatusCompleted {
		t.Fatalf("incomplete=%v status=%s", rc.Run.Incomplete, rc.Run.Status)
	}
}

func TestRepeatGuardDisabledSkips(t *testing.T) {
	off := false
	g := repeatguard.New(repeatguard.ConfigFromPolicy(domain.RuntimePolicy{RepeatGuardEnabled: &off}))
	g.Record("t", `{}`, `{"ok":false,"failure_kind":"tool_error"}`, nil, nil)
	g.Record("t", `{}`, `{"ok":false,"failure_kind":"tool_error"}`, nil, nil)
	if g.Check("t", `{}`, nil).Blocked {
		t.Fatal("disabled guard must not block")
	}
}

type countingFailRegistry struct {
	output string
	err    error
	calls  atomic.Int64
}

func (f *countingFailRegistry) Register(tool.Tool) error { return nil }
func (f *countingFailRegistry) Replace(string, string, tool.Tool) error {
	return errors.New("unsupported")
}
func (f *countingFailRegistry) Owner(string) (string, bool) { return "", false }
func (f *countingFailRegistry) Unregister(string)           {}
func (f *countingFailRegistry) Get(string) tool.Tool {
	return nil
}
func (f *countingFailRegistry) Execute(context.Context, string, string) (string, error) {
	f.calls.Add(1)
	return f.output, f.err
}
func (f *countingFailRegistry) ListInfos() []*tool.Info           { return nil }
func (f *countingFailRegistry) FilterInfos([]string) []*tool.Info { return nil }
func (f *countingFailRegistry) Names() []string                   { return nil }
