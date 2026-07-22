package service

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
)

// AdoptionSource 提供父 Run 已接纳的子产物列表（可选依赖；nil 时不做委派销账）。
type AdoptionSource interface {
	ListByConsumer(consumerRunID string) []AdoptionRecord
}

// CompletionEvaluator 只根据持久化控制面事实判断 required deliverable 是否完成。
// SkillFollow、模型回答和文件 basename 均不参与判定。
//
// 销账路径：
//  1. 本 Run 本地 selection + publication + delivery（+ 仅 QAEnforcement=required 时的 QA）；
//  2. 或存在「已接纳 + 子 Run 已成功交付且类型匹配」的 adoption（父信任子完成门禁，不再重复 QA）。
//
// 证据驱动：评估前若尚无 primary required，但已有可交付产物，则补建 Spec，避免「有产物却无门禁」逃逸。
type CompletionEvaluator struct {
	deliverables artifactcontract.DeliverableSpecStore
	selections   artifactcontract.DeliverableSelectionStore
	publications artifactcontract.ArtifactPublicationStore
	deliveries   artifactcontract.DeliveryRecordStore
	evidence     artifactcontract.QAEvidenceStore
	produced     workcontract.ProducedResourceStore
	adoptions    AdoptionSource
	now          func() time.Time
}

func NewCompletionEvaluator(
	deliverables artifactcontract.DeliverableSpecStore,
	selections artifactcontract.DeliverableSelectionStore,
	publications artifactcontract.ArtifactPublicationStore,
	deliveries artifactcontract.DeliveryRecordStore,
	evidence artifactcontract.QAEvidenceStore,
	produced workcontract.ProducedResourceStore,
) (*CompletionEvaluator, error) {
	if deliverables == nil || selections == nil || publications == nil || deliveries == nil || evidence == nil {
		return nil, errors.New("completion evaluator 缺少 deliverable/selection/publication/delivery/qa store")
	}
	return &CompletionEvaluator{
		deliverables: deliverables, selections: selections, publications: publications,
		deliveries: deliveries, evidence: evidence, produced: produced,
		adoptions: GlobalAdoptionStore, now: time.Now,
	}, nil
}

// WithAdoptions 覆盖接纳来源（单测可注入；生产默认 GlobalAdoptionStore）。
func (e *CompletionEvaluator) WithAdoptions(src AdoptionSource) *CompletionEvaluator {
	if e != nil {
		e.adoptions = src
	}
	return e
}

func (e *CompletionEvaluator) EvaluateCompletion(ctx context.Context, tenantID, runID string) (artifactcontract.CompletionDecision, error) {
	if e.produced != nil {
		resources, err := e.produced.ListByRun(ctx, tenantID, runID)
		if err != nil {
			return artifactcontract.CompletionDecision{}, err
		}
		now := time.Now().UTC()
		if e.now != nil {
			now = e.now().UTC()
		}
		if err := EnsurePrimaryDeliverableFromProduced(ctx, e.deliverables, tenantID, runID, resources, now); err != nil {
			return artifactcontract.CompletionDecision{}, err
		}
	}
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
				// 本 Run 未亲手 select：尝试以「子已交付 + 父已接纳」销账。
				ok, adoptErr := e.adoptedSatisfies(ctx, tenantID, runID, spec)
				if adoptErr != nil {
					return artifactcontract.CompletionDecision{}, adoptErr
				}
				if !ok {
					decision.MissingDeliverableIDs = append(decision.MissingDeliverableIDs, spec.ID)
				}
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
		// 仅 QAEnforcement=required 才阻塞；空/optional 与不配置等价，不查 QA。
		if spec.QAPolicy != "" && artifactmodel.IsRequiredEnforcement(spec.QAEnforcement) {
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

// adoptedSatisfies：父已接纳的子产物，且子 Run 上该产物已有 committed publication + succeeded delivery，
// 并与父 DeliverableSpec 的后缀/MIME 约束匹配 → 父账可销（不再要求父侧 QA）。
func (e *CompletionEvaluator) adoptedSatisfies(ctx context.Context, tenantID, consumerRunID string, spec artifactmodel.DeliverableSpec) (bool, error) {
	if e.adoptions == nil {
		return false, nil
	}
	for _, adoption := range e.adoptions.ListByConsumer(consumerRunID) {
		if strings.EqualFold(strings.TrimSpace(adoption.Role), "qa_asset") {
			continue
		}
		if strings.TrimSpace(adoption.OwnerRunID) == "" || strings.TrimSpace(adoption.ProducedID) == "" {
			continue
		}
		ownerTenant := firstNonEmpty(adoption.OwnerTenantID, tenantID)
		ok, err := e.ownerDeliveredMatching(ctx, ownerTenant, adoption.OwnerRunID, adoption.ProducedID, spec)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (e *CompletionEvaluator) ownerDeliveredMatching(ctx context.Context, ownerTenant, ownerRunID, producedID string, parentSpec artifactmodel.DeliverableSpec) (bool, error) {
	ownerSpecs, err := e.deliverables.ListDeliverables(ctx, ownerTenant, ownerRunID)
	if err != nil {
		return false, err
	}
	for _, ownerSpec := range ownerSpecs {
		pubs, err := e.publications.ListPublications(ctx, ownerTenant, ownerRunID, ownerSpec.ID)
		if err != nil {
			return false, err
		}
		for _, pub := range pubs {
			if pub.ProducedResourceID != producedID || pub.Status != artifactmodel.PublicationCommitted {
				continue
			}
			delivered, err := e.hasSuccessfulDelivery(ctx, ownerTenant, ownerRunID, ownerSpec.ID, pub)
			if err != nil {
				return false, err
			}
			if !delivered {
				continue
			}
			name := firstNonEmpty(pub.DesiredName, ownerSpec.DesiredName)
			media := mimeHintFromName(name)
			// 无文件名时：若父契约无后缀/MIME 约束则放行；有约束则无法证明类型匹配。
			if name == "" {
				if len(parentSpec.AcceptedSuffix) == 0 && len(parentSpec.AcceptedMIMEs) == 0 {
					return true, nil
				}
				continue
			}
			if parentSpec.MatchesObserved(name, media) {
				return true, nil
			}
		}
	}
	return false, nil
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
		if record.Status != artifactmodel.QAEvidencePassed || record.PolicyID != spec.QAPolicy {
			continue
		}
		// visual-qa/v1 等视觉策略必须由同名 Validator 证明；content/render 证据不得冒充
		if spec.QAPolicy == "visual-qa/v1" && record.Validator != "visual-qa/v1" {
			continue
		}
		if record.ProducedResourceID == selection.ProducedResourceID && record.PublicationID == publication.ID &&
			record.SubjectVersion == publication.SubjectVersion && record.SubjectSHA256 == publication.SubjectSHA256 {
			return true, nil
		}
	}
	return false, nil
}

func mimeHintFromName(name string) string {
	ext := strings.ToLower(path.Ext(strings.TrimSpace(name)))
	return map[string]string{
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".pdf":  "application/pdf",
		".md":   "text/markdown",
	}[ext]
}

var _ artifactcontract.CompletionPolicy = (*CompletionEvaluator)(nil)
