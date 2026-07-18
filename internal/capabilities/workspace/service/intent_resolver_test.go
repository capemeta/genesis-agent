package service

import (
	"context"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

func TestTaskIntentResolverSelectsTaskJobForFileProduction(t *testing.T) {
	resolver := NewTaskIntentResolver()
	intent, err := resolver.ResolveIntent(context.Background(), workcontract.ResolveIntentRequest{Prompt: "根据 ultra5-comparison-summary.md，写一个PPT文件", HasProject: true})
	if err != nil {
		t.Fatal(err)
	}
	if intent.RequiredMode != execmodel.WorkspaceModeTask || !intent.BoundedInputs || !intent.BoundedOutputs || !intent.ArtifactRequired || !intent.HasProject {
		t.Fatalf("intent = %+v", intent)
	}
}

func TestTaskIntentResolverKeepsCodeModificationInProject(t *testing.T) {
	intent, err := NewTaskIntentResolver().ResolveIntent(context.Background(), workcontract.ResolveIntentRequest{Prompt: "修复 internal/app/run_service.go 中的并发问题", HasProject: true})
	if err != nil {
		t.Fatal(err)
	}
	if intent.RequiredMode != execmodel.WorkspaceModeProject || !intent.ModifyProject || intent.ArtifactRequired {
		t.Fatalf("intent = %+v", intent)
	}
}

func TestTaskIntentResolverDoesNotOverrideTrustedIntent(t *testing.T) {
	supplied := workcontract.ExecutionIntent{ExplicitMode: execmodel.WorkspaceModeSession, NeedsPersistentRun: true}
	intent, err := NewTaskIntentResolver().ResolveIntent(context.Background(), workcontract.ResolveIntentRequest{Prompt: "根据 a.md 写一个PPT文件", Supplied: supplied, HasProject: true})
	if err != nil {
		t.Fatal(err)
	}
	if intent.ExplicitMode != execmodel.WorkspaceModeSession || intent.ArtifactRequired {
		t.Fatalf("intent = %+v", intent)
	}
}

func TestTaskIntentResolverCommitsDeliverableFilenameWithoutCreateVerb(t *testing.T) {
	// 多轮改名/改内容：只要给出交付目标名，就由 Harness 建契约，不依赖「生成/创建」动词。
	intent, err := NewTaskIntentResolver().ResolveIntent(context.Background(), workcontract.ResolveIntentRequest{
		Prompt:     "拷贝下生成的文件，重命名为aaa.pptx 内容修改下：极致轻薄与静音的价格改成 12999",
		HasProject: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if intent.RequiredMode != execmodel.WorkspaceModeTask || !intent.ArtifactRequired || !intent.BoundedOutputs {
		t.Fatalf("intent = %+v", intent)
	}
}

func TestTaskIntentResolverIgnoresReadOnlyDeliverableFilename(t *testing.T) {
	intent, err := NewTaskIntentResolver().ResolveIntent(context.Background(), workcontract.ResolveIntentRequest{
		Prompt:     "打开 aaa.pptx 看看内容",
		HasProject: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if intent.ArtifactRequired || intent.RequiredMode == execmodel.WorkspaceModeTask {
		t.Fatalf("read-only mention should not require artifact, intent=%+v", intent)
	}
}
