package vision

import (
	"context"
	"testing"

	domainvision "genesis-agent/internal/capabilities/llm/vision"
	"genesis-agent/internal/domain"
	tool "genesis-agent/internal/capabilities/tool/contract"
)

type fakeVisionLLM struct {
	name string
	out  string
}

func (f fakeVisionLLM) Generate(context.Context, []*domain.Message, []*tool.Info) (*domain.Message, error) {
	return domain.NewAssistantMessage(f.out), nil
}
func (f fakeVisionLLM) StreamGenerate(context.Context, []*domain.Message, []*tool.Info, func(string, bool)) (*domain.Message, error) {
	return domain.NewAssistantMessage(f.out), nil
}
func (f fakeVisionLLM) GetModelName() string { return f.name }

func TestExpertAnalyzeRecordsUsage(t *testing.T) {
	t.Parallel()
	var gotTokens int64
	e := &Expert{
		Mode:  domainvision.ModeExpertRoute,
		Model: fakeVisionLLM{name: "vision-helper", out: `{"passed":true,"defects":[]}`},
		OnUsage: func(_ context.Context, model string, tokens int64) {
			if model != "vision-helper" {
				t.Fatalf("model=%s", model)
			}
			gotTokens = tokens
		},
		Estimator: func(context.Context, string, string) int { return 42 },
	}
	res, err := e.Analyze(context.Background(), domain.ImageRef{PathAlias: "a.png", LocalReadPath: "x"}, "layout")
	if err != nil {
		t.Fatal(err)
	}
	if res.EstimatedTokens != 42 || gotTokens != 42 {
		t.Fatalf("tokens=%d got=%d", res.EstimatedTokens, gotTokens)
	}
	if res.Text == "" {
		t.Fatal("empty text")
	}
}
