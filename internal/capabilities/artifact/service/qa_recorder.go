package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

type QAEvidenceRecorder struct {
	deliverables artifactcontract.DeliverableSpecStore
	selections   artifactcontract.DeliverableSelectionStore
	publications artifactcontract.ArtifactPublicationStore
	evidence     artifactcontract.QAEvidenceStore
	now          func() time.Time
}

func NewQAEvidenceRecorder(d artifactcontract.DeliverableSpecStore, s artifactcontract.DeliverableSelectionStore, p artifactcontract.ArtifactPublicationStore, e artifactcontract.QAEvidenceStore) (*QAEvidenceRecorder, error) {
	if d == nil || s == nil || p == nil || e == nil {
		return nil, fmt.Errorf("qa evidence recorder 依赖不完整")
	}
	return &QAEvidenceRecorder{deliverables: d, selections: s, publications: p, evidence: e, now: time.Now}, nil
}

func (r *QAEvidenceRecorder) RecordOutcome(ctx context.Context, req artifactcontract.QAOutcomeRequest) error {
	status := req.Status
	switch status {
	case artifactmodel.QAEvidencePassed, artifactmodel.QAEvidenceFailed, artifactmodel.QAEvidenceDegraded, artifactmodel.QAEvidenceSkipped:
	default:
		return fmt.Errorf("QA outcome status无效: %q", status)
	}
	validator := strings.TrimSpace(req.Validator)
	if validator == "" {
		return fmt.Errorf("QA outcome validator不能为空")
	}
	failure := strings.TrimSpace(req.FailureCode)
	if status != artifactmodel.QAEvidencePassed && failure == "" {
		return fmt.Errorf("非passed QA outcome必须提供failure_code")
	}
	specs, err := r.deliverables.ListDeliverables(ctx, req.TenantID, req.RunID)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if !spec.Required || spec.QAPolicy == "" || (req.PolicyID != "" && req.PolicyID != spec.QAPolicy) {
			continue
		}
		selection, err := r.selections.GetSelection(ctx, req.TenantID, req.RunID, spec.ID)
		if err != nil {
			if errors.Is(err, artifactcontract.ErrNotFound) {
				continue
			}
			return err
		}
		publications, err := r.publications.ListPublications(ctx, req.TenantID, req.RunID, spec.ID)
		if err != nil {
			return err
		}
		for _, publication := range publications {
			if publication.Status != artifactmodel.PublicationCommitted || publication.ProducedResourceID != selection.ProducedResourceID {
				continue
			}
			key := req.TenantID + "\x00" + req.RunID + "\x00" + spec.ID + "\x00" + publication.ID + "\x00" + validator + "\x00" + string(status) + "\x00" + failure
			digest := sha256.Sum256([]byte(key))
			// 禁止用模糊 skill-command:* 写入 visual-qa 通过证据
			if status == artifactmodel.QAEvidencePassed && spec.QAPolicy == ValidatorVisualQA && validator != ValidatorVisualQA && strings.HasPrefix(validator, "skill-command:") {
				continue
			}
			version := "qa-outcome/v1"
			if status == artifactmodel.QAEvidencePassed && validator == ValidatorVisualQA {
				version = "visual-checklist/v1"
			}
			record := artifactmodel.QAEvidenceRecord{ID: "qa-" + hex.EncodeToString(digest[:16]), TenantID: req.TenantID, RunID: req.RunID, DeliverableID: spec.ID, ProducedResourceID: selection.ProducedResourceID, PublicationID: publication.ID, SubjectVersion: publication.SubjectVersion, SubjectSHA256: publication.SubjectSHA256, PolicyID: spec.QAPolicy, Validator: validator, ValidatorVersion: version, Status: status, FailureCode: failure, CreatedAt: r.now().UTC()}
			if err := r.evidence.CreateQAEvidence(ctx, record); err != nil && !errors.Is(err, artifactcontract.ErrAlreadyExists) {
				return err
			}
		}
	}
	return nil
}

var _ artifactcontract.QAEvidenceRecorder = (*QAEvidenceRecorder)(nil)
