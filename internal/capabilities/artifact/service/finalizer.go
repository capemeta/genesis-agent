package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type publicationPort interface {
	Publish(context.Context, PublicationRequest) (artifactmodel.ArtifactRef, error)
}
type deliveryPort interface {
	Deliver(context.Context, DeliveryRequest) (artifactmodel.DeliveryResult, error)
}

// DeterministicFinalizer 实现架构文档中的 ArtifactPublicationPolicy：
// 唯一匹配由 Harness 自动发布；多候选仅允许选择 candidate_id；无匹配返回结构化失败。
// 多候选绝不让模型提交路径或 locator。
// 同槽位 produced supersede 后，若旧 selection 已不是当前 head，则重绑到唯一新候选并重新发布。
type DeterministicFinalizer struct {
	deliverables artifactcontract.DeliverableSpecStore
	selections   artifactcontract.DeliverableSelectionStore
	produced     workcontract.ProducedResourceStore
	publications publicationPort
	deliveries   deliveryPort
	now          func() time.Time
}

func NewDeterministicFinalizer(deliverables artifactcontract.DeliverableSpecStore, selections artifactcontract.DeliverableSelectionStore, produced workcontract.ProducedResourceStore, publications publicationPort, deliveries deliveryPort) (*DeterministicFinalizer, error) {
	if deliverables == nil || selections == nil || produced == nil || publications == nil || deliveries == nil {
		return nil, fmt.Errorf("deterministic finalizer 依赖不完整")
	}
	return &DeterministicFinalizer{deliverables: deliverables, selections: selections, produced: produced, publications: publications, deliveries: deliveries, now: time.Now}, nil
}

func (s *DeterministicFinalizer) FinalizeRequired(ctx context.Context, tenantID, runID string) (artifactmodel.FinalizationResult, error) {
	specs, err := s.deliverables.ListDeliverables(ctx, tenantID, runID)
	if err != nil {
		return artifactmodel.FinalizationResult{}, err
	}
	resources, err := s.produced.ListByRun(ctx, tenantID, runID)
	if err != nil {
		return artifactmodel.FinalizationResult{}, err
	}
	result := artifactmodel.FinalizationResult{}
	for _, spec := range specs {
		if !spec.Required {
			continue
		}
		resolution, err := s.resolve(ctx, tenantID, runID, spec, resources)
		if err != nil {
			return result, err
		}
		result.Resolutions = append(result.Resolutions, resolution)
	}
	return result, nil
}

func (s *DeterministicFinalizer) SelectAndFinalize(ctx context.Context, tenantID, runID, deliverableID, candidateID string) (artifactmodel.DeliveryResult, error) {
	spec, resource, err := s.loadCandidate(ctx, tenantID, runID, deliverableID, candidateID)
	if err != nil {
		return artifactmodel.DeliveryResult{}, err
	}
	if !matchesDeliverable(spec, resource, effectiveName(spec, resource)) {
		return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("candidate 不满足 deliverable contract"))
	}
	if err := s.bindSelection(ctx, tenantID, runID, deliverableID, candidateID, "model-candidate-id"); err != nil {
		return artifactmodel.DeliveryResult{}, err
	}
	return s.publishAndDeliver(ctx, tenantID, runID, deliverableID, candidateID)
}

func (s *DeterministicFinalizer) resolve(ctx context.Context, tenantID, runID string, spec artifactmodel.DeliverableSpec, resources []workmodel.ProducedResourceDescriptor) (artifactmodel.DeliverableResolution, error) {
	candidates := matchingCandidateIDs(spec, resources)
	if selected, err := s.selections.GetSelection(ctx, tenantID, runID, spec.ID); err == nil {
		if containsID(candidates, selected.ProducedResourceID) {
			return s.finalizeSelected(ctx, tenantID, runID, spec, selected.ProducedResourceID, candidates)
		}
		// 旧 selection 指向已被 supersede 的 head：仅在唯一新候选时自动重绑。
		switch len(candidates) {
		case 1:
			if err := s.bindSelection(ctx, tenantID, runID, spec.ID, candidates[0], "harness-supersede"); err != nil {
				return artifactmodel.DeliverableResolution{}, err
			}
			return s.finalizeSelected(ctx, tenantID, runID, spec, candidates[0], candidates)
		case 0:
			return artifactmodel.DeliverableResolution{DeliverableID: spec.ID, Status: "missing", CandidateIDs: candidates}, nil
		default:
			return artifactmodel.DeliverableResolution{DeliverableID: spec.ID, Status: "selection_required", CandidateIDs: candidates}, nil
		}
	} else if !errors.Is(err, artifactcontract.ErrNotFound) {
		return artifactmodel.DeliverableResolution{}, err
	}
	resolution := artifactmodel.DeliverableResolution{DeliverableID: spec.ID, CandidateIDs: candidates}
	switch len(candidates) {
	case 0:
		resolution.Status = "missing"
		return resolution, nil
	case 1:
		if err := s.bindSelection(ctx, tenantID, runID, spec.ID, candidates[0], "harness-unique-match"); err != nil {
			return resolution, err
		}
		return s.finalizeSelected(ctx, tenantID, runID, spec, candidates[0], candidates)
	default:
		resolution.Status = "selection_required"
		return resolution, nil
	}
}

// finalizeSelected 发布并交付；目标冲突不向上抛错，以免毒化后续 skill 命令。
func (s *DeterministicFinalizer) finalizeSelected(ctx context.Context, tenantID, runID string, spec artifactmodel.DeliverableSpec, selectedID string, candidates []string) (artifactmodel.DeliverableResolution, error) {
	base := artifactmodel.DeliverableResolution{
		DeliverableID: spec.ID,
		SelectedID:    selectedID,
		CandidateIDs:  candidates,
	}
	delivery, err := s.publishAndDeliver(ctx, tenantID, runID, spec.ID, selectedID)
	if err != nil {
		if isDeliveryTargetConflict(err) {
			base.Status = "delivery_conflict"
			base.Warning = fmt.Sprintf("DELIVERY_TARGET_CONFLICT: deliverable %s 目标无法覆盖交付（非普通文件或权限拒绝）", spec.ID)
			return base, nil
		}
		return base, err
	}
	base.Status = "delivered"
	base.Delivery = delivery
	return base, nil
}

var _ artifactcontract.RequiredDeliverableFinalizer = (*DeterministicFinalizer)(nil)

func (s *DeterministicFinalizer) bindSelection(ctx context.Context, tenantID, runID, deliverableID, candidateID, selectedBy string) error {
	selection := artifactmodel.DeliverableSelection{DeliverableID: deliverableID, ProducedResourceID: candidateID, SelectedBy: selectedBy, CreatedAt: s.now().UTC()}
	err := s.selections.CreateSelection(ctx, tenantID, runID, selection)
	if err == nil {
		return nil
	}
	if !errors.Is(err, artifactcontract.ErrAlreadyExists) {
		return err
	}
	existing, getErr := s.selections.GetSelection(ctx, tenantID, runID, deliverableID)
	if getErr != nil {
		return getErr
	}
	if existing.ProducedResourceID == candidateID {
		return nil
	}
	current, currentErr := s.isCurrentProducedHead(ctx, tenantID, runID, existing.ProducedResourceID)
	if currentErr != nil {
		return currentErr
	}
	if current {
		return artifactcontract.NewError(artifactcontract.ErrCodeArtifactPublicationConflict, fmt.Errorf("deliverable selection 幂等键冲突"))
	}
	return s.selections.ReplaceSelection(ctx, tenantID, runID, selection)
}

func (s *DeterministicFinalizer) isCurrentProducedHead(ctx context.Context, tenantID, runID, producedResourceID string) (bool, error) {
	descriptor, err := s.produced.Get(ctx, tenantID, runID, producedResourceID)
	if err != nil {
		if workspaceErrorCode(err) == workcontract.ErrCodeProducedResourceNotFound {
			return false, nil
		}
		return false, err
	}
	head, err := s.produced.GetByLogicalRef(ctx, tenantID, runID, descriptor.LogicalRef)
	if err != nil {
		if workspaceErrorCode(err) == workcontract.ErrCodeProducedResourceNotFound {
			return false, nil
		}
		return false, err
	}
	return head.ID == producedResourceID, nil
}

func (s *DeterministicFinalizer) publishAndDeliver(ctx context.Context, tenantID, runID, deliverableID, candidateID string) (artifactmodel.DeliveryResult, error) {
	if _, err := s.publications.Publish(ctx, PublicationRequest{TenantID: tenantID, RunID: runID, DeliverableID: deliverableID, ProducedResourceID: candidateID}); err != nil {
		return artifactmodel.DeliveryResult{}, err
	}
	return s.deliveries.Deliver(ctx, DeliveryRequest{TenantID: tenantID, RunID: runID, DeliverableID: deliverableID})
}

func (s *DeterministicFinalizer) loadCandidate(ctx context.Context, tenantID, runID, deliverableID, candidateID string) (artifactmodel.DeliverableSpec, workmodel.ProducedResourceDescriptor, error) {
	specs, err := s.deliverables.ListDeliverables(ctx, tenantID, runID)
	if err != nil {
		return artifactmodel.DeliverableSpec{}, workmodel.ProducedResourceDescriptor{}, err
	}
	var spec artifactmodel.DeliverableSpec
	for _, item := range specs {
		if item.ID == deliverableID {
			spec = item
			break
		}
	}
	if spec.ID == "" {
		return spec, workmodel.ProducedResourceDescriptor{}, artifactcontract.ErrNotFound
	}
	resource, err := s.produced.Get(ctx, tenantID, runID, candidateID)
	return spec, resource, err
}

func matchingCandidateIDs(spec artifactmodel.DeliverableSpec, resources []workmodel.ProducedResourceDescriptor) []string {
	candidates := make([]string, 0)
	for _, resource := range resources {
		if matchesDeliverable(spec, resource, effectiveName(spec, resource)) {
			candidates = append(candidates, resource.ID)
		}
	}
	sort.Strings(candidates)
	return candidates
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func effectiveName(spec artifactmodel.DeliverableSpec, resource workmodel.ProducedResourceDescriptor) string {
	if spec.DesiredName != "" {
		return spec.DesiredName
	}
	return resource.ObservedName
}

func workspaceErrorCode(err error) workcontract.ErrorCode {
	var classified *workcontract.Error
	if errors.As(err, &classified) {
		return classified.Code
	}
	return ""
}
