package model

import (
	"testing"
	"time"
)

func TestProducedResourceDescriptorValidate(t *testing.T) {
	now := time.Now().UTC()
	d := ProducedResourceDescriptor{ID: "produced-1", TenantID: "tenant", RunID: "run-1", BindingID: "binding-1", LogicalRef: "run:/work/binding-1/output.pptx", Source: ResourceRef{Authority: "executor", Scheme: "session-file", ID: "opaque-1", Version: "sha256:abc", Scope: ResourceScope{TenantID: "tenant"}}, ObservedName: "output.pptx", Size: 10, Availability: ResourceAvailabilityLeased, ExpiresAt: ptrTime(now.Add(time.Hour)), CreatedAt: now}
	if err := d.Validate(); err != nil {
		t.Fatalf("valid descriptor rejected: %v", err)
	}
	d.Source.Version = ""
	if err := d.Validate(); err == nil {
		t.Fatal("descriptor without version accepted")
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
