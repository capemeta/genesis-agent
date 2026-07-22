package service

import (
	"context"
	"fmt"
	"testing"

	artifactmemory "genesis-agent/internal/capabilities/artifact/adapter/memory"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

func TestTaskDeliverableInitializerCreatesTypeOnlyPPTContract(t *testing.T) {
	store := artifactmemory.NewStore()
	initializer, _ := NewTaskDeliverableInitializer(store)
	err := initializer.InitializeRun(context.Background(), artifactcontract.RunInitializationRequest{
		TenantID: "tenant", RunID: "run-1234", Prompt: "根据 source.md 制作一个 PPT", ArtifactRequired: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	specs, _ := store.ListDeliverables(context.Background(), "tenant", "run-1234")
	if len(specs) != 1 || specs[0].QAPolicy != "visual-qa/v1" || specs[0].AcceptedSuffix[0] != ".pptx" || specs[0].DesiredName != "" {
		t.Fatalf("spec=%+v", specs)
	}
	if specs[0].QAEnforcement != "" {
		t.Fatalf("default pptx QA must be soft (empty enforcement), got %q", specs[0].QAEnforcement)
	}
}

func TestTaskDeliverableInitializerAppliesSkillQAOverride(t *testing.T) {
	store := artifactmemory.NewStore()
	initializer, _ := NewTaskDeliverableInitializer(store)
	err := initializer.InitializeRun(context.Background(), artifactcontract.RunInitializationRequest{
		TenantID: "tenant", RunID: "run-skill-qa", Prompt: "做个 PPT", ArtifactRequired: true,
		QAPolicy: "visual-qa/v1", QAEnforcement: "required",
	})
	if err != nil {
		t.Fatal(err)
	}
	specs, _ := store.ListDeliverables(context.Background(), "tenant", "run-skill-qa")
	if len(specs) != 1 || specs[0].QAPolicy != "visual-qa/v1" || specs[0].QAEnforcement != "required" {
		t.Fatalf("skill QA override not applied: %+v", specs)
	}
}

func TestTaskDeliverableInitializerDoesNotExtractNameFromNaturalLanguage(t *testing.T) {
	store := artifactmemory.NewStore()
	initializer, _ := NewTaskDeliverableInitializer(store)
	cases := []string{
		"生成 final-deck.pptx",
		"拷贝下生成的文件，重命名为aaa.pptx 内容修改下：价格改成 12999",
		"复制一份2026笔记本选型比较.pptx，把表格价格标红",
		"把 2026笔记本选型比较.pptx 重命名为 aaa.pptx",
	}
	for i, prompt := range cases {
		runID := fmt.Sprintf("run-nl-%d", i)
		err := initializer.InitializeRun(context.Background(), artifactcontract.RunInitializationRequest{
			TenantID: "tenant", RunID: runID, Prompt: prompt, ArtifactRequired: true,
		})
		if err != nil {
			t.Fatalf("prompt=%q: %v", prompt, err)
		}
		specs, _ := store.ListDeliverables(context.Background(), "tenant", runID)
		if len(specs) != 1 || specs[0].DesiredName != "" || specs[0].AcceptedSuffix[0] != ".pptx" {
			t.Fatalf("prompt=%q spec=%+v", prompt, specs)
		}
	}
}

func TestTaskDeliverableInitializerPrefersDeclaredOverPromptHeuristic(t *testing.T) {
	store := artifactmemory.NewStore()
	initializer, _ := NewTaskDeliverableInitializer(store)
	err := initializer.InitializeRun(context.Background(), artifactcontract.RunInitializationRequest{
		TenantID: "tenant", RunID: "run-declared", Prompt: "随便做个 PPT", ArtifactRequired: true,
		Deliverables: []artifactcontract.DeclaredDeliverable{{
			DesiredName: "contract-deck.pptx", AcceptedSuffix: []string{".pptx"}, Required: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	specs, _ := store.ListDeliverables(context.Background(), "tenant", "run-declared")
	if len(specs) != 1 || specs[0].DesiredName != "contract-deck.pptx" || specs[0].Role != artifactmodel.DeliverableRolePrimary || !specs[0].Required {
		t.Fatalf("spec=%+v", specs)
	}
	if specs[0].QAPolicy != "visual-qa/v1" {
		t.Fatalf("expected default pptx QA policy, got %q", specs[0].QAPolicy)
	}
}

func TestTaskDeliverableInitializerRejectsDeclaredWithoutTypeConstraint(t *testing.T) {
	store := artifactmemory.NewStore()
	initializer, _ := NewTaskDeliverableInitializer(store)
	err := initializer.InitializeRun(context.Background(), artifactcontract.RunInitializationRequest{
		TenantID: "tenant", RunID: "run-bad", Deliverables: []artifactcontract.DeclaredDeliverable{{DesiredName: "noext"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTaskDeliverableInitializerLeavesDeclaredNameEmptyWhenMissing(t *testing.T) {
	store := artifactmemory.NewStore()
	initializer, _ := NewTaskDeliverableInitializer(store)
	req := artifactcontract.RunInitializationRequest{
		TenantID: "tenant", RunID: "run-a",
		Deliverables: []artifactcontract.DeclaredDeliverable{{AcceptedSuffix: []string{".pptx"}, Required: true}},
	}
	if err := initializer.InitializeRun(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	specsA, _ := store.ListDeliverables(context.Background(), "tenant", "run-a")
	if len(specsA) != 1 || specsA[0].DesiredName != "" || specsA[0].AcceptedSuffix[0] != ".pptx" {
		t.Fatalf("specA=%+v", specsA)
	}
}
