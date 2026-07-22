package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

type DeliveryRequest struct {
	TenantID      string
	RunID         string
	DeliverableID string
}

type DeliveryService struct {
	deliverables artifactcontract.DeliverableSpecStore
	selections   artifactcontract.DeliverableSelectionStore
	publications artifactcontract.ArtifactPublicationStore
	evidence     artifactcontract.QAEvidenceStore
	deliveries   artifactcontract.DeliveryRecordStore
	artifacts    artifactcontract.TransactionalStore
	planner      artifactcontract.DeliveryTargetPlanner
	materializer artifactcontract.RecoverableMaterializer
	now          func() time.Time
	locks        sync.Map
}

func NewDeliveryService(deliverables artifactcontract.DeliverableSpecStore, selections artifactcontract.DeliverableSelectionStore, publications artifactcontract.ArtifactPublicationStore, evidence artifactcontract.QAEvidenceStore, deliveries artifactcontract.DeliveryRecordStore, artifacts artifactcontract.TransactionalStore, planner artifactcontract.DeliveryTargetPlanner, materializer artifactcontract.RecoverableMaterializer) (*DeliveryService, error) {
	if deliverables == nil || selections == nil || publications == nil || evidence == nil || deliveries == nil || artifacts == nil || planner == nil || materializer == nil {
		return nil, fmt.Errorf("delivery service 依赖不完整")
	}
	return &DeliveryService{deliverables: deliverables, selections: selections, publications: publications, evidence: evidence, deliveries: deliveries, artifacts: artifacts, planner: planner, materializer: materializer, now: time.Now}, nil
}

func (s *DeliveryService) Deliver(ctx context.Context, req DeliveryRequest) (artifactmodel.DeliveryResult, error) {
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.DeliverableID) == "" {
		return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryTargetDenied, fmt.Errorf("delivery request 信息不完整"))
	}
	spec, publication, artifact, target, err := s.load(ctx, req)
	if err != nil {
		return artifactmodel.DeliveryResult{}, err
	}
	if strings.TrimSpace(spec.QAPolicy) != "" {
		state, qaErr := evaluateQAState(ctx, s.publications, s.evidence, req.TenantID, req.RunID, spec, publication.ProducedResourceID)
		if qaErr != nil {
			return artifactmodel.DeliveryResult{}, qaErr
		}
		allowed := state == qaStatePassed || ((state == qaStateDegraded || state == qaStateFailed) && !artifactmodel.IsRequiredEnforcement(spec.QAEnforcement))
		if !allowed {
			return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeQARequired, fmt.Errorf("QA 尚未形成可交付证据或与当前 publication 版本不匹配"))
		}
	}
	key := deliveryKey(req, artifact, target)
	id := "delivery-" + deliveryShortHash(key)
	lock := s.deliveryLock(id)
	lock.Lock()
	defer lock.Unlock()
	now := s.now().UTC()
	initial := artifactmodel.DeliveryRecord{ID: id, TenantID: req.TenantID, RunID: req.RunID, DeliverableID: req.DeliverableID, PublicationID: publication.ID, ArtifactID: artifact.ID, Target: target.Resource, TargetKind: target.Kind, TargetName: target.Name, IdempotencyKey: key, Status: artifactmodel.DeliveryPending, Revision: 1, CreatedAt: now, UpdatedAt: now}
	record, _, err := s.deliveries.CreateDelivery(ctx, initial)
	if err != nil {
		return artifactmodel.DeliveryResult{}, err
	}
	owned := false
	for attempt := 0; attempt < 6; attempt++ {
		switch record.Status {
		case artifactmodel.DeliverySucceeded:
			return deliveryResultFromRecord(record, artifact), nil
		case artifactmodel.DeliveryPending, artifactmodel.DeliveryFailed:
			next := record
			next.Status = artifactmodel.DeliveryDelivering
			next.FailureCode = ""
			next.UpdatedAt = s.now().UTC()
			record, err = s.deliveries.UpdateDelivery(ctx, record.Revision, next)
			if err != nil {
				if errors.Is(err, artifactcontract.ErrRevisionConflict) {
					record, err = s.deliveries.GetDelivery(ctx, req.TenantID, id)
					if err == nil {
						continue
					}
				}
				return artifactmodel.DeliveryResult{}, err
			}
			owned = true
		case artifactmodel.DeliveryDelivering:
			if !owned {
				if recovered, ok, recoverErr := s.materializer.GetMaterialized(ctx, artifact, target); recoverErr != nil {
					return artifactmodel.DeliveryResult{}, recoverErr
				} else if ok {
					return s.finishDelivery(ctx, record, recovered)
				}
				return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryInProgress, fmt.Errorf("delivery 正由其他 worker 执行"))
			}
			result, materializeErr := s.materializer.Materialize(ctx, artifact, target)
			if materializeErr != nil {
				if recovered, ok, recoverErr := s.materializer.GetMaterialized(ctx, artifact, target); recoverErr == nil && ok {
					return s.finishDelivery(ctx, record, recovered)
				}
				resolved, resolveErr := s.resolveMaterializeConflict(ctx, req, artifact, target, materializeErr)
				if resolveErr != nil {
					s.failDelivery(ctx, record, resolveErr)
					return artifactmodel.DeliveryResult{}, resolveErr
				}
				return s.finishDelivery(ctx, record, resolved)
			}
			return s.finishDelivery(ctx, record, result)
		default:
			return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryMaterializeFailed, fmt.Errorf("delivery status 无效: %s", record.Status))
		}
	}
	return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryInProgress, fmt.Errorf("delivery 状态推进超过限制"))
}

func (s *DeliveryService) load(ctx context.Context, req DeliveryRequest) (artifactmodel.DeliverableSpec, artifactmodel.ArtifactPublicationRecord, artifactmodel.ArtifactRef, artifactmodel.DeliveryTarget, error) {
	specs, err := s.deliverables.ListDeliverables(ctx, req.TenantID, req.RunID)
	if err != nil {
		return artifactmodel.DeliverableSpec{}, artifactmodel.ArtifactPublicationRecord{}, artifactmodel.ArtifactRef{}, artifactmodel.DeliveryTarget{}, err
	}
	var spec artifactmodel.DeliverableSpec
	for _, v := range specs {
		if v.ID == req.DeliverableID {
			spec = v
			break
		}
	}
	if spec.ID == "" {
		return spec, artifactmodel.ArtifactPublicationRecord{}, artifactmodel.ArtifactRef{}, artifactmodel.DeliveryTarget{}, artifactcontract.ErrNotFound
	}
	selection, err := s.selections.GetSelection(ctx, req.TenantID, req.RunID, req.DeliverableID)
	if err != nil {
		return spec, artifactmodel.ArtifactPublicationRecord{}, artifactmodel.ArtifactRef{}, artifactmodel.DeliveryTarget{}, err
	}
	records, err := s.publications.ListPublications(ctx, req.TenantID, req.RunID, req.DeliverableID)
	if err != nil {
		return spec, artifactmodel.ArtifactPublicationRecord{}, artifactmodel.ArtifactRef{}, artifactmodel.DeliveryTarget{}, err
	}
	var publication artifactmodel.ArtifactPublicationRecord
	for _, v := range records {
		if v.Status == artifactmodel.PublicationCommitted && v.ProducedResourceID == selection.ProducedResourceID {
			if publication.ID != "" {
				return spec, publication, artifactmodel.ArtifactRef{}, artifactmodel.DeliveryTarget{}, artifactcontract.ErrIdempotencyConflict
			}
			publication = v
		}
	}
	if publication.ID == "" {
		return spec, publication, artifactmodel.ArtifactRef{}, artifactmodel.DeliveryTarget{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactDeliveryRequired, fmt.Errorf("deliverable 尚无 committed publication"))
	}
	artifact, ok, err := s.artifacts.GetCommitted(ctx, publication.ArtifactID)
	if err != nil {
		return spec, publication, artifact, artifactmodel.DeliveryTarget{}, err
	}
	if !ok || artifact.ID != publication.ArtifactID || artifact.SHA256 != publication.SubjectSHA256 {
		return spec, publication, artifact, artifactmodel.DeliveryTarget{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("publication 对应 Artifact 不存在或 hash 不一致"))
	}
	target, err := s.planner.PlanDelivery(ctx, spec, artifact)
	if err != nil {
		return spec, publication, artifact, target, err
	}
	if strings.TrimSpace(target.Name) == "" || strings.TrimSpace(target.Resource.Authority) == "" || strings.TrimSpace(target.Resource.Scheme) == "" || strings.TrimSpace(target.Resource.ID) == "" {
		return spec, publication, artifact, target, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryTargetDenied, fmt.Errorf("planner 返回无效 target"))
	}
	return spec, publication, artifact, target, nil
}

func (s *DeliveryService) finishDelivery(ctx context.Context, record artifactmodel.DeliveryRecord, result artifactmodel.DeliveryResult) (artifactmodel.DeliveryResult, error) {
	if result.Artifact.ID != record.ArtifactID || strings.TrimSpace(result.Resource.ID) == "" {
		return artifactmodel.DeliveryResult{}, artifactcontract.NewError(artifactcontract.ErrCodeDeliveryMaterializeFailed, fmt.Errorf("materializer result 身份无效"))
	}
	next := record
	next.Status = artifactmodel.DeliverySucceeded
	next.ResultResource = result.Resource
	next.Display = result.Display
	if name := strings.TrimSpace(result.Target.Name); name != "" {
		next.TargetName = name
	}
	next.UpdatedAt = s.now().UTC()
	if _, err := s.deliveries.UpdateDelivery(ctx, record.Revision, next); err != nil {
		if errors.Is(err, artifactcontract.ErrRevisionConflict) {
			current, getErr := s.deliveries.GetDelivery(ctx, record.TenantID, record.ID)
			if getErr == nil && current.Status == artifactmodel.DeliverySucceeded {
				return deliveryResultFromRecord(current, result.Artifact), nil
			}
		}
		return artifactmodel.DeliveryResult{}, err
	}
	return result, nil
}
func (s *DeliveryService) failDelivery(ctx context.Context, record artifactmodel.DeliveryRecord, cause error) {
	next := record
	next.Status = artifactmodel.DeliveryFailed
	next.FailureCode = deliveryFailureCode(cause)
	next.UpdatedAt = s.now().UTC()
	_, _ = s.deliveries.UpdateDelivery(ctx, record.Revision, next)
}
func deliveryFailureCode(err error) string {
	var classified *artifactcontract.Error
	if errors.As(err, &classified) {
		return string(classified.Code)
	}
	return string(artifactcontract.ErrCodeDeliveryMaterializeFailed)
}

func isDeliveryTargetConflict(err error) bool {
	var classified *artifactcontract.Error
	return errors.As(err, &classified) && classified.Code == artifactcontract.ErrCodeDeliveryTargetConflict
}

// resolveMaterializeConflict：普通文件目标冲突时统一原子覆盖（跨 Run / 同 Run supersede 同策略）。
// 非普通文件（symlink/目录等）由 ReplaceMaterialize 继续拒绝。
func (s *DeliveryService) resolveMaterializeConflict(ctx context.Context, _ DeliveryRequest, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget, materializeErr error) (artifactmodel.DeliveryResult, error) {
	if !isDeliveryTargetConflict(materializeErr) {
		return artifactmodel.DeliveryResult{}, materializeErr
	}
	return s.materializer.ReplaceMaterialize(ctx, artifact, target)
}

func deliveryResultFromRecord(record artifactmodel.DeliveryRecord, artifact artifactmodel.ArtifactRef) artifactmodel.DeliveryResult {
	return artifactmodel.DeliveryResult{Artifact: artifact, Target: artifactmodel.DeliveryTarget{Kind: record.TargetKind, Resource: record.Target, Name: record.TargetName}, Resource: record.ResultResource, Display: record.Display}
}
func deliveryKey(req DeliveryRequest, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) string {
	return strings.Join([]string{req.TenantID, req.RunID, req.DeliverableID, artifact.ID, target.Resource.Authority, target.Resource.Scheme, target.Resource.ID, target.Resource.Version, string(target.Kind), target.Name}, "\x00")
}
func deliveryShortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
func (s *DeliveryService) deliveryLock(id string) *sync.Mutex {
	value, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}
