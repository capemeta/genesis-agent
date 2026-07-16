package result

import (
	"context"
	"errors"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime/multiagent/model"
)

type denyArtifacts struct{}

func (denyArtifacts) ProjectArtifact(context.Context, model.Artifact) (model.Artifact, bool, error) {
	return model.Artifact{}, false, nil
}

type failingSanitizer struct{}

func (failingSanitizer) Sanitize(string) (string, error) { return "", errors.New("unavailable") }

type acceptingEvidence struct{}

func (acceptingEvidence) Validate(_ context.Context, _ model.ArtifactManifest, _ []model.Finding) (ValidatedEvidence, error) {
	return ValidatedEvidence{
		Artifacts: []model.Artifact{{ResourceID: "res-verified", Path: "output/report.json", Kind: "file", ContentHash: "hash"}},
		Findings:  []model.Finding{{Claim: "报告已生成", Evidence: []string{"res-verified"}}},
	}, nil
}

type failingEvidence struct{}

func (failingEvidence) Validate(context.Context, model.ArtifactManifest, []model.Finding) (ValidatedEvidence, error) {
	return ValidatedEvidence{}, errors.New("backend unavailable")
}

type rewritingProjector struct{}

func (rewritingProjector) ProjectArtifact(_ context.Context, artifact model.Artifact) (model.Artifact, bool, error) {
	artifact.Path = "shared/" + artifact.ResourceID
	artifact.Description = "已授权资源"
	return artifact, true, nil
}

type unsafePathProjector struct{}

func (unsafePathProjector) ProjectArtifact(_ context.Context, artifact model.Artifact) (model.Artifact, bool, error) {
	artifact.Path = "D:/private/report.txt"
	return artifact, true, nil
}

func TestReducerRedactsAndTruncatesFinalAnswer(t *testing.T) {
	reduced := (Reducer{MaxSummaryRunes: 18}).Reduce(context.Background(), TerminalCandidate{
		AgentID: "agent-1",
		Run:     &domain.Run{ID: "run-1", Status: domain.RunStatusCompleted, FinalAnswer: "token=super-secret 这是足够长的最终结论"},
	})
	if reduced.Status != model.ResultStatusCompleted || !reduced.Truncated {
		t.Fatalf("unexpected result: %+v", reduced)
	}
	if strings.Contains(reduced.Summary, "super-secret") || !strings.Contains(reduced.Summary, "[redacted]") {
		t.Fatalf("secret leaked in summary: %q", reduced.Summary)
	}
	if reduced.ResultID == "" || len(reduced.OmittedSections) == 0 {
		t.Fatalf("missing stable metadata: %+v", reduced)
	}
}

func TestReducerFailureHasSafeError(t *testing.T) {
	reduced := NewReducer().Reduce(context.Background(), TerminalCandidate{AgentID: "agent-1", Err: errors.New("authorization: Bearer secret-token")})
	if reduced.Status != model.ResultStatusFailed || reduced.Error == nil {
		t.Fatalf("unexpected result: %+v", reduced)
	}
	if strings.Contains(reduced.Error.Message, "secret-token") {
		t.Fatalf("secret leaked in error: %q", reduced.Error.Message)
	}
}

func TestReducerClassifiesBudgetFailure(t *testing.T) {
	reduced := NewReducer().Reduce(context.Background(), TerminalCandidate{AgentID: "agent-1", Err: errors.New("budget exceeded: tokens")})
	if reduced.Error == nil || reduced.Error.Code != "budget_exceeded" {
		t.Fatalf("unexpected budget error: %+v", reduced.Error)
	}
}

func TestReducerFailsClosedWhenSanitizerFails(t *testing.T) {
	reduced := (Reducer{Sanitizer: failingSanitizer{}}).Reduce(context.Background(), TerminalCandidate{AgentID: "agent-1", Run: &domain.Run{ID: "run-1", Status: domain.RunStatusCompleted, FinalAnswer: "sensitive final"}})
	if reduced.Status != model.ResultStatusFailed || reduced.Summary != "" || reduced.Error == nil {
		t.Fatalf("expected safe failure, got %+v", reduced)
	}
}

func TestReducerOnlyDeliversValidatedManifest(t *testing.T) {
	candidate := TerminalCandidate{
		AgentID: "agent-1",
		Run:     &domain.Run{ID: "run-1", Status: domain.RunStatusCompleted, FinalAnswer: "安全结论"},
		Manifest: model.ArtifactManifest{Artifacts: []model.Artifact{{
			ResourceID: "res-unverified", Path: "D:/secret.txt", Kind: "file",
		}}},
		Findings: []model.Finding{{Claim: "未验证结论", Evidence: []string{"D:/secret.txt"}}},
	}
	reduced := (Reducer{Evidence: acceptingEvidence{}}).Reduce(context.Background(), candidate)
	if reduced.Status != model.ResultStatusCompleted || len(reduced.Artifacts) != 1 || reduced.Artifacts[0].ResourceID != "res-verified" {
		t.Fatalf("validated manifest was not used: %+v", reduced)
	}
	if len(reduced.Findings) != 1 || reduced.Findings[0].Evidence[0] != "res-verified" {
		t.Fatalf("validated findings were not used: %+v", reduced.Findings)
	}
}

func TestReducerDegradesToPartialWhenEvidenceValidationFails(t *testing.T) {
	reduced := (Reducer{Evidence: failingEvidence{}}).Reduce(context.Background(), TerminalCandidate{
		AgentID:  "agent-1",
		Run:      &domain.Run{ID: "run-1", Status: domain.RunStatusCompleted, FinalAnswer: "安全结论"},
		Manifest: model.ArtifactManifest{Artifacts: []model.Artifact{{ResourceID: "res-1", Kind: "file"}}},
	})
	if reduced.Status != model.ResultStatusPartial || reduced.Error == nil || reduced.Error.Code != "evidence_validation_failed" {
		t.Fatalf("expected partial evidence failure, got %+v", reduced)
	}
	if len(reduced.Artifacts) != 0 || len(reduced.Findings) != 0 || reduced.Summary != "安全结论" {
		t.Fatalf("unsafe optional result leaked: %+v", reduced)
	}
}

func TestProjectorOmitsUnauthorizedArtifacts(t *testing.T) {
	record := model.TaskResult{ResultID: "result-1", Artifacts: []model.Artifact{{ResourceID: "res-1", Kind: "file"}}}
	projected := NewProjector(denyArtifacts{}).Project(context.Background(), record)
	if len(projected.Artifacts) != 0 || len(projected.OmittedSections) != 1 {
		t.Fatalf("artifact authorization projection failed: %+v", projected)
	}
}

func TestProjectorUsesResourceProjection(t *testing.T) {
	record := model.TaskResult{ResultID: "result-1", Artifacts: []model.Artifact{{ResourceID: "res-1", Path: "output/private.txt", Kind: "file"}}}
	projected := NewProjector(rewritingProjector{}).Project(context.Background(), record)
	if len(projected.Artifacts) != 1 || projected.Artifacts[0].Path != "shared/res-1" || projected.Artifacts[0].Description != "已授权资源" {
		t.Fatalf("resource was not projected: %+v", projected)
	}
}

func TestProjectorRejectsUnsafePathReturnedByResourceProjector(t *testing.T) {
	record := model.TaskResult{ResultID: "result-1", Artifacts: []model.Artifact{{ResourceID: "res-1", Kind: "file"}}}
	projected := NewProjector(unsafePathProjector{}).Project(context.Background(), record)
	if len(projected.Artifacts) != 0 || len(projected.OmittedSections) != 1 {
		t.Fatalf("unsafe projected path was delivered: %+v", projected)
	}
}
