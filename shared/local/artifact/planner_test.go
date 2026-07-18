package artifact

import (
	"context"
	"testing"

	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestPolicyTargetPlannerUsesOnlyConfiguredPolicy(t *testing.T) {
	planner, err := NewPolicyTargetPlanner(map[string]artifactmodel.DeliveryTarget{"download": {Kind: artifactmodel.DeliveryProductInbox, Resource: workmodel.ResourceRef{Authority: "host", Scheme: "inbox", ID: "inbox"}, Name: "$artifact_name"}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := planner.PlanDelivery(context.Background(), artifactmodel.DeliverableSpec{DeliveryPolicy: "download"}, artifactmodel.ArtifactRef{Name: "report.pdf"})
	if err != nil || target.Name != "report.pdf" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	if _, err := planner.PlanDelivery(context.Background(), artifactmodel.DeliverableSpec{DeliveryPolicy: "unknown"}, artifactmodel.ArtifactRef{}); err == nil {
		t.Fatal("unknown policy must be denied")
	}
}
