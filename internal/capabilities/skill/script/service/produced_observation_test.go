package service

import (
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestResolveProducedObservationLeasedUsesWorkspaceRelativeLogicalPath(t *testing.T) {
	binding := execmodel.ExecutionBinding{ID: "binding-f39b3f5e-6361-4316-8573-fb6175184a18"}
	meta := skillmodel.Metadata{PackageID: "office-ppt"}
	logical := "run:/work/binding-f39b3f5e-6361-4316-8573-fb6175184a18/skills/office-ppt/ultra5-comparison.pptx"
	got, gotLogical, err := resolveProducedObservation(binding, meta, "skills/office-ppt", "ultra5-comparison.pptx", logical, workmodel.ResourceAvailabilityLeased)
	if err != nil {
		t.Fatal(err)
	}
	want := "work/binding-f39b3f5e-6361-4316-8573-fb6175184a18/skills/office-ppt/ultra5-comparison.pptx"
	if string(got) != want {
		t.Fatalf("ObservedPath=%q want=%q", got, want)
	}
	if gotLogical != logical {
		t.Fatalf("LogicalRef=%q want=%q", gotLogical, logical)
	}
}

func TestResolveProducedObservationDurableKeepsHostRelativeSkillPath(t *testing.T) {
	binding := execmodel.ExecutionBinding{ID: "binding-1"}
	meta := skillmodel.Metadata{PackageID: "office-ppt"}
	logical := "run:/work/binding-1/skills/office-ppt/deck.pptx"
	got, gotLogical, err := resolveProducedObservation(binding, meta, "skills/office-ppt", "deck.pptx", logical, workmodel.ResourceAvailabilityDurable)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "skills/office-ppt/deck.pptx" {
		t.Fatalf("ObservedPath=%q", got)
	}
	if gotLogical != logical {
		t.Fatalf("LogicalRef=%q", gotLogical)
	}
}

func TestProducedResourceRefsAlignWithLeasedObservation(t *testing.T) {
	binding := execmodel.ExecutionBinding{ID: "binding-abc"}
	meta := skillmodel.Metadata{PackageID: "office-ppt"}
	produced := []string{"ultra5-comparison.pptx"}
	refs := producedResourceRefs(binding, meta, produced)
	if len(refs) != 1 {
		t.Fatalf("refs=%v", refs)
	}
	got, _, err := resolveProducedObservation(binding, meta, "skills/office-ppt", produced[0], refs[0], workmodel.ResourceAvailabilityLeased)
	if err != nil {
		t.Fatal(err)
	}
	want := "work/binding-abc/skills/office-ppt/ultra5-comparison.pptx"
	if string(got) != want {
		t.Fatalf("ObservedPath=%q want=%q (was wrongly skills/office-ppt/...)", got, want)
	}
}
