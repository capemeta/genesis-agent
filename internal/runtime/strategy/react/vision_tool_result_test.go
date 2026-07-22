package react

import (
	"context"
	"strings"
	"testing"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactservice "genesis-agent/internal/capabilities/artifact/service"
	"genesis-agent/internal/capabilities/llm/vision"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/domain"
	"genesis-agent/internal/runtime"
	runtimevision "genesis-agent/internal/runtime/vision"
)

type fakeExpert struct {
	text string
	err  error
}

func (f fakeExpert) Analyze(context.Context, domain.ImageRef, string) (runtimevision.Result, error) {
	if f.err != nil {
		return runtimevision.Result{}, f.err
	}
	return runtimevision.Result{Text: f.text}, nil
}

type fakeQARecorder struct {
	got []artifactcontract.QAOutcomeRequest
}

func (f *fakeQARecorder) RecordOutcome(_ context.Context, req artifactcontract.QAOutcomeRequest) error {
	f.got = append(f.got, req)
	return nil
}

func TestAnnotateVisionUnavailable(t *testing.T) {
	t.Parallel()
	raw := `{"ok":false,"error":"vision_unavailable","message":"no vision"}`
	got := annotateVisionUnavailable(raw)
	if !strings.Contains(got, "[harness_bridge]") || !strings.Contains(got, "Pillow") {
		t.Fatalf("bridge missing: %s", got)
	}
	if annotateVisionUnavailable(got) != got {
		t.Fatal("should be idempotent")
	}
	e := &ReactLoopEngine{}
	rc := &runtime.RunContext{VisionMode: string(vision.ModeDegradedText)}
	content, parts := e.applyViewImageRuntimeBridge(context.Background(), rc, raw)
	if len(parts) != 0 {
		t.Fatalf("parts=%+v", parts)
	}
	if !strings.Contains(content, "honest_degrade_or_configure_vision") {
		t.Fatalf("content=%s", content)
	}
}

func TestAnnotateExpiredLeaseGuidance(t *testing.T) {
	t.Parallel()
	raw := `{"ok":false,"error":"PRODUCED_RESOURCE_EXPIRED","rerender_hint":"run thumbnail.py"}`
	got := annotateExpiredLeaseGuidance(raw, nil)
	if !strings.Contains(got, "[harness_bridge]") || !strings.Contains(got, "rerun_thumbnail_and_view_image") {
		t.Fatalf("bridge missing: %s", got)
	}
	if annotateExpiredLeaseGuidance(got, nil) != got {
		t.Fatal("should be idempotent")
	}
}

func TestRewriteViewImageExpertRouteRecordsVisualQA(t *testing.T) {
	t.Parallel()
	e := &ReactLoopEngine{visionExpert: fakeExpert{text: `{"passed":true,"defects":[]}`}}
	rec := &fakeQARecorder{}
	ctx := artifactcontract.WithQAEvidenceRecorder(context.Background(), rec)
	ctx = workcontract.WithPreparedRun(ctx, workmodel.PreparedRun{
		Manifest: workmodel.RunManifest{RunID: "r1", Scope: workmodel.ResourceScope{TenantID: "t1"}},
	})
	out := e.rewriteViewImageForExpertRoute(ctx, nil, `{"ok":true,"image_ref":{"path_alias":"a.png","media_type":"image/png"}}`)
	if !strings.Contains(out, `"passed":true`) || !strings.Contains(out, "[vision_expert]") {
		t.Fatalf("out=%s", out)
	}
	if len(rec.got) != 1 || rec.got[0].Validator != artifactservice.ValidatorVisualQA {
		t.Fatalf("recorder=%+v", rec.got)
	}
}

func TestApplyViewImageDirectInjectParts(t *testing.T) {
	t.Parallel()
	e := &ReactLoopEngine{}
	rc := &runtime.RunContext{VisionMode: string(vision.ModeDirectInject)}
	content, parts := e.applyViewImageRuntimeBridge(context.Background(), rc,
		`{"ok":true,"inject_image":true,"image_ref":{"path_alias":"a.png","media_type":"image/png"}}`)
	if content == "" || len(parts) != 2 || parts[1].Type != domain.ContentPartImage {
		t.Fatalf("content=%s parts=%+v", content, parts)
	}
}

func TestTryRecordVisualChecklist(t *testing.T) {
	t.Parallel()
	rec := &fakeQARecorder{}
	ctx := artifactcontract.WithQAEvidenceRecorder(context.Background(), rec)
	ctx = workcontract.WithPreparedRun(ctx, workmodel.PreparedRun{
		Manifest: workmodel.RunManifest{RunID: "r1", Scope: workmodel.ResourceScope{TenantID: "t1"}},
	})
	if err := tryRecordVisualQAFromText(ctx, `done [VISUAL_CHECKLIST: layout=ok, contrast=ok, overflow=none]`); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 1 || rec.got[0].Validator != artifactservice.ValidatorVisualQA {
		t.Fatalf("got=%+v", rec.got)
	}
}

func TestTryRecordFailedVisualChecklist(t *testing.T) {
	t.Parallel()
	rec := &fakeQARecorder{}
	ctx := artifactcontract.WithQAEvidenceRecorder(context.Background(), rec)
	ctx = workcontract.WithPreparedRun(ctx, workmodel.PreparedRun{
		Manifest: workmodel.RunManifest{RunID: "r1", Scope: workmodel.ResourceScope{TenantID: "t1"}},
	})
	if err := tryRecordVisualQAFromText(ctx, `{"passed":false,"defects":["overflow"]}`); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 1 || rec.got[0].Status != "failed" || rec.got[0].FailureCode != "visual_qa_failed" {
		t.Fatalf("got=%+v", rec.got)
	}
}

var _ runtimevision.Analyzer = fakeExpert{}
