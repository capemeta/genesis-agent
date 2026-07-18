package model

import (
	"testing"
	"time"
)

func TestCommittedPublicationRequiresStableArtifactHash(t *testing.T) {
	now := time.Now().UTC()
	v := ArtifactPublicationRecord{ID: "p", TenantID: "t", RunID: "r", ProducedResourceID: "source", DeliverableID: "d", DesiredName: "a.pdf", GateVersion: "v1", IdempotencyKey: "key", Status: PublicationCommitted, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if v.Validate() == nil {
		t.Fatal("expected missing artifact/hash error")
	}
	v.ArtifactID = "a"
	v.SubjectVersion = "sha256:abc"
	v.SubjectSHA256 = "abc"
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceRequiresVersionedSubjectAndFailureCode(t *testing.T) {
	now := time.Now().UTC()
	v := QAEvidenceRecord{ID: "q", TenantID: "t", RunID: "r", DeliverableID: "d", ProducedResourceID: "p", SubjectVersion: "sha256:x", SubjectSHA256: "x", PolicyID: "visual", Validator: "ppt", ValidatorVersion: "1", Status: QAEvidenceFailed, CreatedAt: now}
	if v.Validate() == nil {
		t.Fatal("expected failure code error")
	}
	v.FailureCode = "BLANK_SLIDE"
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
}
