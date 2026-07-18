package service

import (
	"context"
	"errors"
	"sort"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

// CompletionEvaluator 只根据持久化控制面事实判断 required deliverable 是否完成。
// SkillFollow、模型回答和文件 basename 均不参与判定。
type CompletionEvaluator struct {
	deliverables artifactcontract.DeliverableSpecStore
	selections   artifactcontract.DeliverableSelectionStore
	publications artifactcontract.ArtifactPublicationStore
	deliveries   artifactcontract.DeliveryRecordStore
	evidence     artifactcontract.QAEvidenceStore
}

func NewCompletionEvaluator(
	deliverables artifactcontract.DeliverableSpecStore,
	selections artifactcontract.DeliverableSelectionStore,
	publications artifactcontract.ArtifactPublicationStore,
	deliveries artifactcontract.DeliveryRecordStore,
	evidence artifactcontract.QAEvidenceStore,
) (*CompletionEvaluator, error) {
	if deliverables == nil || selections == nil || publications == nil || deliveries == nil || evidence == nil {
		return nil, errors.New("completion evaluator 缺少 deliverable/selection/publication/delivery/qa store")
	}
	return &CompletionEvaluator{deliverables: deliverables, selections: selections, publications: publications, deliveries: deliveries, evidence: evidence}, nil
}

func (e *CompletionEvaluator) EvaluateCompletion(ctx context.Context, tenantID, runID string) (artifactcontract.CompletionDecision, error) {
	specs, err := e.deliverables.ListDeliverables(ctx, tenantID, runID)
	if err != nil {
		return artifactcontract.CompletionDecision{}, err
	}
	decision := artifactcontract.CompletionDecision{Complete: true}
	for _, spec := range specs {
		if !spec.Required {
			continue
		}
		selection, err := e.selections.GetSelection(ctx, tenantID, runID, spec.ID)
		if err != nil {
			if errors.Is(err, artifactcontract.ErrNotFound) {
				decision.MissingDeliverableIDs = append(decision.MissingDeliverableIDs, spec.ID)
				continue
			}
			return artifactcontract.CompletionDecision{}, err
		}
		publication, ok, err := e.committedPublication(ctx, tenantID, runID, spec.ID, selection.ProducedResourceID)
		if err != nil {
			return artifactcontract.CompletionDecision{}, err
		}
		if !ok {
			decision.MissingDeliverableIDs = append(decision.MissingDeliverableIDs, spec.ID)
			continue
		}
		delivered, err := e.hasSuccessfulDelivery(ctx, tenantID, runID, spec.ID, publication)
		if err != nil {
			return artifactcontract.CompletionDecision{}, err
		}
		if !delivered {
			decision.MissingDeliverableIDs = append(decision.MissingDeliverableIDs, spec.ID)
			continue
		}
		if spec.QAPolicy != "" {
			passed, err := e.hasMatchingQA(ctx, tenantID, runID, spec, selection, publication)
			if err != nil {
				return artifactcontract.CompletionDecision{}, err
			}
			if !passed {
				decision.PendingQAIDs = append(decision.PendingQAIDs, spec.ID)
			}
		}
	}
	decision.Complete = len(decision.MissingDeliverableIDs) == 0 && len(decision.PendingQAIDs) == 0 && len(decision.FailureCodes) == 0
	sort.Strings(decision.MissingDeliverableIDs)
	sort.Strings(decision.PendingQAIDs)
	sort.Strings(decision.FailureCodes)
	return decision, nil
}

func (e *CompletionEvaluator) committedPublication(ctx context.Context, tenantID, runID, deliverableID, producedID string) (artifactmodel.ArtifactPublicationRecord, bool, error) {
	records, err := e.publications.ListPublications(ctx, tenantID, runID, deliverableID)
	if err != nil {
		return artifactmodel.ArtifactPublicationRecord{}, false, err
	}
	var matched artifactmodel.ArtifactPublicationRecord
	count := 0
	for _, record := range records {
		if record.ProducedResourceID == producedID && record.Status == artifactmodel.PublicationCommitted {
			matched = record
			count++
		}
	}
	// 同一 selection 对应多个正式 Artifact 是控制面冲突，不能任选一个完成。
	return matched, count == 1, nil
}

func (e *CompletionEvaluator) hasSuccessfulDelivery(ctx context.Context, tenantID, runID, deliverableID string, publication artifactmodel.ArtifactPublicationRecord) (bool, error) {
	records, err := e.deliveries.ListDeliveries(ctx, tenantID, runID, deliverableID)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.PublicationID == publication.ID && record.ArtifactID == publication.ArtifactID && record.Status == artifactmodel.DeliverySucceeded {
			return true, nil
		}
	}
	return false, nil
}

func (e *CompletionEvaluator) hasMatchingQA(ctx context.Context, tenantID, runID string, spec artifactmodel.DeliverableSpec, selection artifactmodel.DeliverableSelection, publication artifactmodel.ArtifactPublicationRecord) (bool, error) {
	records, err := e.evidence.ListQAEvidence(ctx, tenantID, runID, spec.ID)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.Status == artifactmodel.QAEvidencePassed && record.PolicyID == spec.QAPolicy &&
			record.ProducedResourceID == selection.ProducedResourceID && record.PublicationID == publication.ID &&
			record.SubjectVersion == publication.SubjectVersion && record.SubjectSHA256 == publication.SubjectSHA256 {
			return true, nil
		}
	}
	return false, nil
}

var _ artifactcontract.CompletionPolicy = (*CompletionEvaluator)(nil)
