package service

import (
	"testing"
	"time"

	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

func TestUniqueMatchingDeliverableIDFillsWhenSingleMatch(t *testing.T) {
	specs := []artifactmodel.DeliverableSpec{
		{ID: "deliverable-primary", DesiredName: "deck.pptx", AcceptedSuffix: []string{".pptx"}, Required: true},
		{ID: "deliverable-notes", DesiredName: "notes.txt", AcceptedSuffix: []string{".txt"}},
	}
	descriptor := workmodel.ProducedResourceDescriptor{
		ID: "produced-1", ObservedName: "deck.pptx", MediaType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
	if got := uniqueMatchingDeliverableID(specs, descriptor); got != "deliverable-primary" {
		t.Fatalf("got %q", got)
	}
}

func TestUniqueMatchingDeliverableIDEmptyWhenAmbiguous(t *testing.T) {
	specs := []artifactmodel.DeliverableSpec{
		{ID: "a", AcceptedSuffix: []string{".pptx"}},
		{ID: "b", AcceptedSuffix: []string{".pptx"}},
	}
	descriptor := workmodel.ProducedResourceDescriptor{ID: "produced-1", ObservedName: "deck.pptx", MediaType: "application/vnd.openxmlformats-officedocument.presentationml.presentation", CreatedAt: time.Now()}
	if got := uniqueMatchingDeliverableID(specs, descriptor); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
