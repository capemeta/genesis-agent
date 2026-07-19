package tool_test

import (
	"context"
	"testing"

	tool "genesis-agent/internal/capabilities/tool/contract"
)

type staticTool struct {
	info *tool.Info
}

func (t staticTool) GetInfo() *tool.Info { return t.info }
func (t staticTool) Execute(context.Context, string) (string, error) {
	return "", nil
}

type assessingTool struct {
	staticTool
	assess tool.ConcurrencyAssessment
}

func (t assessingTool) AssessConcurrency(context.Context, string) tool.ConcurrencyAssessment {
	return t.assess
}

func TestResolveExecutionTraitsUsesStaticWhenNoAssessor(t *testing.T) {
	registered := staticTool{info: tool.WithTraits(&tool.Info{Name: "read_file"}, tool.ToolTraits{
		Exposure: tool.ToolExposureDirect, ReadOnly: true, ConcurrencySafe: true,
	})}
	got := tool.ResolveExecutionTraits(context.Background(), registered, `{}`)
	if !got.ConcurrencySafe || !got.ReadOnly {
		t.Fatalf("got=%+v", got)
	}
}

func TestResolveExecutionTraitsAppliesAssessorOverride(t *testing.T) {
	registered := assessingTool{
		staticTool: staticTool{info: tool.WithTraits(&tool.Info{Name: "run_command"}, tool.ToolTraits{
			Exposure: tool.ToolExposureDirect, ReadOnly: false, ConcurrencySafe: false, NeedsPermission: true,
		})},
		assess: tool.ConcurrencyAssessment{ConcurrencySafe: true, ReadOnly: true},
	}
	got := tool.ResolveExecutionTraits(context.Background(), registered, `{"command":"git status"}`)
	if !got.ConcurrencySafe || !got.ReadOnly {
		t.Fatalf("got=%+v, want concurrency-safe read-only upgrade", got)
	}
}

func TestResolveExecutionTraitsAssessorDowngrade(t *testing.T) {
	registered := assessingTool{
		staticTool: staticTool{info: tool.WithTraits(&tool.Info{Name: "search_mcp_tools"}, tool.ToolTraits{
			Exposure: tool.ToolExposureDirect, ConcurrencySafe: true,
		})},
		assess: tool.ConcurrencyAssessment{},
	}
	got := tool.ResolveExecutionTraits(context.Background(), registered, `{"promote":true}`)
	if got.ConcurrencySafe {
		t.Fatalf("got=%+v, want unsafe after assessor downgrade", got)
	}
}

func TestDefaultTraitsTaskIsConcurrencySafe(t *testing.T) {
	got := tool.DefaultTraits("Task")
	if !got.ConcurrencySafe || got.ReadOnly {
		t.Fatalf("Task traits=%+v, want ConcurrencySafe=true ReadOnly=false", got)
	}
}
