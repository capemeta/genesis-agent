package service

import (
	"context"
	"fmt"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

type qaState string

const (
	qaStateMissing  qaState = "missing"
	qaStatePassed   qaState = "passed"
	qaStateDegraded qaState = "degraded"
	qaStateFailed   qaState = "failed"
)

// evaluateQAState 只接受绑定到当前 committed publication 精确版本的 passed 证据。
// degraded/skipped 仅用于 optional 策略的诚实降级，并且绝不能满足 required enforcement。
func evaluateQAState(ctx context.Context, publications artifactcontract.ArtifactPublicationStore, evidence artifactcontract.QAEvidenceStore, tenantID, runID string, spec artifactmodel.DeliverableSpec, producedID string) (qaState, error) {
	records, err := publications.ListPublications(ctx, tenantID, runID, spec.ID)
	if err != nil {
		return qaStateMissing, err
	}
	var publication artifactmodel.ArtifactPublicationRecord
	for _, candidate := range records {
		if candidate.Status != artifactmodel.PublicationCommitted || candidate.ProducedResourceID != producedID {
			continue
		}
		if publication.ID != "" {
			return qaStateMissing, artifactcontract.NewError(artifactcontract.ErrCodeArtifactPublicationConflict, fmt.Errorf("同一 deliverable/candidate 存在多个 committed publication"))
		}
		publication = candidate
	}
	if publication.ID == "" {
		return qaStateMissing, nil
	}
	qaRecords, err := evidence.ListQAEvidence(ctx, tenantID, runID, spec.ID)
	if err != nil {
		return qaStateMissing, err
	}
	degraded := false
	failed := false
	for _, record := range qaRecords {
		if record.PolicyID != spec.QAPolicy {
			continue
		}
		if record.Status == artifactmodel.QAEvidenceDegraded || record.Status == artifactmodel.QAEvidenceSkipped {
			if record.ProducedResourceID == producedID && record.PublicationID == publication.ID && record.SubjectVersion == publication.SubjectVersion && record.SubjectSHA256 == publication.SubjectSHA256 {
				degraded = true
			}
			continue
		}
		if record.Status == artifactmodel.QAEvidenceFailed {
			if record.ProducedResourceID == producedID && record.PublicationID == publication.ID && record.SubjectVersion == publication.SubjectVersion && record.SubjectSHA256 == publication.SubjectSHA256 {
				failed = true
			}
			continue
		}
		if record.Status != artifactmodel.QAEvidencePassed || record.ProducedResourceID != producedID || record.PublicationID != publication.ID || record.SubjectVersion != publication.SubjectVersion || record.SubjectSHA256 != publication.SubjectSHA256 {
			continue
		}
		if strings.EqualFold(spec.QAPolicy, ValidatorVisualQA) && record.Validator != ValidatorVisualQA {
			continue
		}
		return qaStatePassed, nil
	}
	if degraded {
		return qaStateDegraded, nil
	}
	if failed {
		return qaStateFailed, nil
	}
	return qaStateMissing, nil
}
