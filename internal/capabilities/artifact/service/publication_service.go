package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type PublicationRequest struct {
	TenantID           string
	RunID              string
	DeliverableID      string
	ProducedResourceID string
}

// LeaseKeeper 在打开 leased ProducedResource 前尽力续租；失败时返回 PRODUCED_RESOURCE_EXPIRED。
type LeaseKeeper interface {
	EnsureLeasedReadable(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error
}

type ArtifactPublicationService struct {
	deliverables artifactcontract.DeliverableSpecStore
	selections   artifactcontract.DeliverableSelectionStore
	publications artifactcontract.ArtifactPublicationStore
	produced     workcontract.ProducedResourceStore
	manifests    workcontract.RunManifestStore
	readers      workcontract.ResourceReaderRouter
	store        artifactcontract.TransactionalStore
	gate         artifactcontract.Gate
	leases       LeaseKeeper
	now          func() time.Time
}

func NewArtifactPublicationService(deliverables artifactcontract.DeliverableSpecStore, selections artifactcontract.DeliverableSelectionStore, publications artifactcontract.ArtifactPublicationStore, produced workcontract.ProducedResourceStore, manifests workcontract.RunManifestStore, readers workcontract.ResourceReaderRouter, store artifactcontract.TransactionalStore, gate artifactcontract.Gate) (*ArtifactPublicationService, error) {
	if deliverables == nil || selections == nil || publications == nil || produced == nil || manifests == nil || readers == nil || store == nil || gate == nil {
		return nil, fmt.Errorf("artifact publication service 依赖不完整")
	}
	return &ArtifactPublicationService{deliverables: deliverables, selections: selections, publications: publications, produced: produced, manifests: manifests, readers: readers, store: store, gate: gate, now: time.Now}, nil
}

// WithLeaseKeeper 注入发布前 leased 资源续租端口；可为 nil。
func (s *ArtifactPublicationService) WithLeaseKeeper(leases LeaseKeeper) *ArtifactPublicationService {
	if s != nil {
		s.leases = leases
	}
	return s
}

func (s *ArtifactPublicationService) Publish(ctx context.Context, req PublicationRequest) (artifactmodel.ArtifactRef, error) {
	if missingPublicationInput(req) {
		return artifactmodel.ArtifactRef{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("publication request 信息不完整"))
	}
	spec, descriptor, execution, name, err := s.loadAndValidate(ctx, req)
	if err != nil {
		return artifactmodel.ArtifactRef{}, err
	}
	key := publicationKey(req, name, s.gate.Version())
	publicationID := "publication-" + shortHash(key)
	artifactID := "artifact-" + publicationID
	now := s.now().UTC()
	initial := artifactmodel.ArtifactPublicationRecord{ID: publicationID, TenantID: req.TenantID, RunID: req.RunID, ProducedResourceID: req.ProducedResourceID, DeliverableID: req.DeliverableID, DesiredName: name, GateVersion: s.gate.Version(), IdempotencyKey: key, Status: artifactmodel.PublicationPending, Revision: 1, CreatedAt: now, UpdatedAt: now}
	record, _, err := s.publications.CreatePublication(ctx, initial)
	if err != nil {
		return artifactmodel.ArtifactRef{}, err
	}
	for attempts := 0; attempts < 6; attempts++ {
		statusBefore := record.Status
		switch record.Status {
		case artifactmodel.PublicationCommitted:
			return s.committedArtifact(ctx, record, artifactID)
		case artifactmodel.PublicationPending, artifactmodel.PublicationFailed:
			record, err = s.stage(ctx, record, descriptor, execution, spec)
		case artifactmodel.PublicationStaging:
			record, err = s.gateStaged(ctx, record, descriptor, spec, artifactID)
		case artifactmodel.PublicationGated:
			return s.commit(ctx, record, descriptor, artifactID)
		default:
			err = artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("publication status 无效: %s", record.Status))
		}
		if err != nil {
			current, getErr := s.publications.GetPublication(ctx, req.TenantID, publicationID)
			if getErr == nil && current.Revision != record.Revision {
				record = current
				continue
			}
			if errors.Is(err, artifactcontract.ErrRevisionConflict) {
				record, err = s.publications.GetPublication(ctx, req.TenantID, publicationID)
				if err == nil {
					continue
				}
			}
			if statusBefore == artifactmodel.PublicationPending || statusBefore == artifactmodel.PublicationStaging {
				s.markFailed(ctx, record, err)
			}
			return artifactmodel.ArtifactRef{}, err
		}
	}
	return artifactmodel.ArtifactRef{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("publication 状态推进超过限制"))
}

func (s *ArtifactPublicationService) loadAndValidate(ctx context.Context, req PublicationRequest) (artifactmodel.DeliverableSpec, workmodel.ProducedResourceDescriptor, workmodel.PreparedExecutionSnapshot, string, error) {
	specs, err := s.deliverables.ListDeliverables(ctx, req.TenantID, req.RunID)
	if err != nil {
		return artifactmodel.DeliverableSpec{}, workmodel.ProducedResourceDescriptor{}, workmodel.PreparedExecutionSnapshot{}, "", err
	}
	var spec artifactmodel.DeliverableSpec
	for _, candidate := range specs {
		if candidate.ID == req.DeliverableID {
			spec = candidate
			break
		}
	}
	if spec.ID == "" {
		return spec, workmodel.ProducedResourceDescriptor{}, workmodel.PreparedExecutionSnapshot{}, "", artifactcontract.ErrNotFound
	}
	selection, err := s.selections.GetSelection(ctx, req.TenantID, req.RunID, req.DeliverableID)
	if err != nil {
		return spec, workmodel.ProducedResourceDescriptor{}, workmodel.PreparedExecutionSnapshot{}, "", err
	}
	if selection.ProducedResourceID != req.ProducedResourceID {
		return spec, workmodel.ProducedResourceDescriptor{}, workmodel.PreparedExecutionSnapshot{}, "", workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("produced resource 不匹配 deliverable selection"))
	}
	descriptor, err := s.produced.Get(ctx, req.TenantID, req.RunID, req.ProducedResourceID)
	if err != nil {
		return spec, descriptor, workmodel.PreparedExecutionSnapshot{}, "", err
	}
	if err := descriptor.Validate(); err != nil {
		return spec, descriptor, workmodel.PreparedExecutionSnapshot{}, "", workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	if descriptor.TenantID != req.TenantID || descriptor.RunID != req.RunID {
		return spec, descriptor, workmodel.PreparedExecutionSnapshot{}, "", workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("produced resource scope 不一致"))
	}
	if err := s.ensureLeasedReadable(ctx, descriptor); err != nil {
		return spec, descriptor, workmodel.PreparedExecutionSnapshot{}, "", err
	}
	manifest, err := s.manifests.Get(ctx, req.TenantID, req.RunID)
	if err != nil {
		return spec, descriptor, workmodel.PreparedExecutionSnapshot{}, "", err
	}
	if err := manifest.Validate(); err != nil {
		return spec, descriptor, workmodel.PreparedExecutionSnapshot{}, "", err
	}
	if manifest.Scope.TenantID != req.TenantID || descriptor.Source.Scope != manifest.Scope {
		return spec, descriptor, workmodel.PreparedExecutionSnapshot{}, "", workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("resource 与 manifest scope 不一致"))
	}
	var execution workmodel.PreparedExecutionSnapshot
	for _, candidate := range manifest.Executions {
		if candidate.Binding.ID == descriptor.BindingID {
			execution = candidate
			break
		}
	}
	if execution.Binding.ID == "" {
		return spec, descriptor, execution, "", workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("binding 不在 Run manifest"))
	}
	if execution.Backend.Authority != descriptor.Source.Authority {
		return spec, descriptor, execution, "", workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, fmt.Errorf("backend/source authority 不一致"))
	}
	name := strings.TrimSpace(spec.DesiredName)
	if name == "" {
		name = descriptor.ObservedName
	}
	if path.Base(strings.ReplaceAll(name, `\`, "/")) != name || strings.ContainsAny(name, "/\\\x00") {
		return spec, descriptor, execution, "", artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("deliverable name 无效"))
	}
	if !matchesDeliverable(spec, descriptor, name) {
		return spec, descriptor, execution, "", artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("produced resource 不满足 deliverable MIME/suffix"))
	}
	return spec, descriptor, execution, name, nil
}

func (s *ArtifactPublicationService) stage(ctx context.Context, record artifactmodel.ArtifactPublicationRecord, descriptor workmodel.ProducedResourceDescriptor, execution workmodel.PreparedExecutionSnapshot, _ artifactmodel.DeliverableSpec) (artifactmodel.ArtifactPublicationRecord, error) {
	// 重试/长时间等待后再次确保 lease；descriptor.ExpiresAt 仍是登记快照。
	if err := s.ensureLeasedReadable(ctx, descriptor); err != nil {
		return record, err
	}
	handle, err := s.readers.Open(ctx, execution.Backend, descriptor.Source)
	if err != nil {
		return record, err
	}
	defer handle.Reader.Close()
	if handle.Version != descriptor.Source.Version || handle.Size != descriptor.Size {
		return record, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("resource handle version/size 不一致"))
	}
	if handle.MediaType != "" && descriptor.MediaType != "" && !strings.EqualFold(strings.TrimSpace(handle.MediaType), strings.TrimSpace(descriptor.MediaType)) {
		return record, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("resource handle media type 不一致"))
	}
	hash := sha256.New()
	limited := &io.LimitedReader{R: handle.Reader, N: descriptor.Size + 1}
	object, err := s.store.Stage(ctx, "artifact-"+record.ID, record.DesiredName, io.TeeReader(limited, hash))
	if err != nil {
		return record, err
	}
	size := descriptor.Size + 1 - limited.N
	if size != descriptor.Size {
		_ = s.store.Abort(context.Background(), object)
		return record, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("resource 实际大小不一致"))
	}
	next := record
	next.Status = artifactmodel.PublicationStaging
	next.StagedObjectID = object.Name
	next.FailureCode = ""
	next.FailureValidator = ""
	next.FailureReason = ""
	next.SubjectVersion = handle.Version
	next.SubjectSHA256 = hex.EncodeToString(hash.Sum(nil))
	next.UpdatedAt = s.now().UTC()
	updated, err := s.publications.UpdatePublication(ctx, record.Revision, next)
	if err != nil {
		_ = s.store.Abort(context.Background(), object)
	}
	return updated, err
}

func (s *ArtifactPublicationService) gateStaged(ctx context.Context, record artifactmodel.ArtifactPublicationRecord, descriptor workmodel.ProducedResourceDescriptor, _ artifactmodel.DeliverableSpec, artifactID string) (artifactmodel.ArtifactPublicationRecord, error) {
	object := artifactcontract.StagedObject{ID: artifactID, Name: record.StagedObjectID}
	reader, err := s.store.OpenStaged(ctx, object)
	if err != nil {
		return record, err
	}
	kind, detected, gateErr := s.gate.Validate(ctx, record.DesiredName, descriptor.MediaType, descriptor.Size, reader)
	closeErr := reader.Close()
	if gateErr != nil {
		_ = s.store.Abort(context.Background(), object)
		var classified *artifactcontract.Error
		if errors.As(gateErr, &classified) && classified.Code == artifactcontract.ErrCodeArtifactInvalid {
			return record, gateErr
		}
		return record, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, gateErr)
	}
	if closeErr != nil {
		_ = s.store.Abort(context.Background(), object)
		return record, closeErr
	}
	next := record
	next.Status = artifactmodel.PublicationGated
	next.ArtifactKind = kind
	next.DetectedMIME = detected
	next.UpdatedAt = s.now().UTC()
	return s.publications.UpdatePublication(ctx, record.Revision, next)
}

func (s *ArtifactPublicationService) commit(ctx context.Context, record artifactmodel.ArtifactPublicationRecord, descriptor workmodel.ProducedResourceDescriptor, artifactID string) (artifactmodel.ArtifactRef, error) {
	if existing, ok, err := s.store.GetCommitted(ctx, artifactID); err != nil {
		return artifactmodel.ArtifactRef{}, err
	} else if ok {
		return s.finishCommitted(ctx, record, existing)
	}
	object := artifactcontract.StagedObject{ID: artifactID, Name: record.StagedObjectID}
	manifest := artifactmodel.Manifest{ArtifactRef: artifactmodel.ArtifactRef{ID: artifactID, Name: record.DesiredName, Kind: record.ArtifactKind, Size: descriptor.Size, SHA256: record.SubjectSHA256, MIME: record.DetectedMIME, Producer: "artifact-publication-service", RunID: record.RunID, Scope: descriptor.Source.Scope}, GateVersion: record.GateVersion, CreatedAt: s.now().UTC()}
	artifact, err := s.store.Commit(ctx, object, manifest)
	if err != nil {
		if recovered, ok, getErr := s.store.GetCommitted(ctx, artifactID); getErr == nil && ok {
			return s.finishCommitted(ctx, record, recovered)
		}
		return artifactmodel.ArtifactRef{}, err
	}
	return s.finishCommitted(ctx, record, artifact)
}

func (s *ArtifactPublicationService) finishCommitted(ctx context.Context, record artifactmodel.ArtifactPublicationRecord, artifact artifactmodel.ArtifactRef) (artifactmodel.ArtifactRef, error) {
	if artifact.SHA256 != record.SubjectSHA256 {
		return artifactmodel.ArtifactRef{}, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("committed artifact hash 与 publication 不一致"))
	}
	next := record
	next.Status = artifactmodel.PublicationCommitted
	next.ArtifactID = artifact.ID
	next.UpdatedAt = s.now().UTC()
	if _, err := s.publications.UpdatePublication(ctx, record.Revision, next); err != nil {
		if errors.Is(err, artifactcontract.ErrRevisionConflict) {
			current, getErr := s.publications.GetPublication(ctx, record.TenantID, record.ID)
			if getErr == nil && current.Status == artifactmodel.PublicationCommitted {
				return s.committedArtifact(ctx, current, artifact.ID)
			}
		}
		return artifactmodel.ArtifactRef{}, err
	}
	return artifact, nil
}
func (s *ArtifactPublicationService) committedArtifact(ctx context.Context, record artifactmodel.ArtifactPublicationRecord, artifactID string) (artifactmodel.ArtifactRef, error) {
	artifact, ok, err := s.store.GetCommitted(ctx, artifactID)
	if err != nil {
		return artifact, err
	}
	if !ok {
		return artifact, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("committed publication 缺少 Artifact"))
	}
	if artifact.ID != record.ArtifactID || artifact.SHA256 != record.SubjectSHA256 {
		return artifact, artifactcontract.NewError(artifactcontract.ErrCodeArtifactInvalid, fmt.Errorf("publication/artifact 身份不一致"))
	}
	return artifact, nil
}

func (s *ArtifactPublicationService) markFailed(ctx context.Context, record artifactmodel.ArtifactPublicationRecord, cause error) {
	next := record
	next.Status = artifactmodel.PublicationFailed
	next.FailureCode = publicationFailureCode(cause)
	next.FailureValidator, next.FailureReason = publicationGateFailureDetail(cause)
	next.UpdatedAt = s.now().UTC()
	_, _ = s.publications.UpdatePublication(ctx, record.Revision, next)
}

func publicationFailureCode(err error) string {
	var artifactErr *artifactcontract.Error
	if errors.As(err, &artifactErr) {
		return string(artifactErr.Code)
	}
	var workspaceErr *workcontract.Error
	if errors.As(err, &workspaceErr) {
		return string(workspaceErr.Code)
	}
	return "PUBLICATION_FAILED"
}

// publicationGateFailureDetail 仅提取 Gate 结构化字段；非 Gate 失败返回空。
func publicationGateFailureDetail(err error) (validator, reason string) {
	var artifactErr *artifactcontract.Error
	if !errors.As(err, &artifactErr) || artifactErr.Code != artifactcontract.ErrCodeArtifactInvalid {
		return "", ""
	}
	return strings.TrimSpace(artifactErr.Validator), strings.TrimSpace(artifactErr.Reason)
}

func publicationKey(req PublicationRequest, name, gate string) string {
	return strings.Join([]string{req.TenantID, req.RunID, req.DeliverableID, req.ProducedResourceID, name, gate}, "\x00")
}
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
func missingPublicationInput(req PublicationRequest) bool {
	return strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.DeliverableID) == "" || strings.TrimSpace(req.ProducedResourceID) == ""
}
func matchesDeliverable(spec artifactmodel.DeliverableSpec, d workmodel.ProducedResourceDescriptor, name string) bool {
	if strings.EqualFold(strings.TrimSpace(d.Role), "qa_asset") || strings.EqualFold(strings.TrimSpace(d.Role), "intermediate_asset") {
		return false
	}
	return spec.MatchesObserved(name, d.MediaType)
}

const leaseRenewSkew = 30 * time.Second

func (s *ArtifactPublicationService) ensureLeasedReadable(ctx context.Context, descriptor workmodel.ProducedResourceDescriptor) error {
	if descriptor.Availability != workmodel.ResourceAvailabilityLeased {
		return nil
	}
	if descriptor.ExpiresAt == nil {
		return workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("produced resource lease 缺少 expires_at"))
	}
	remaining := descriptor.ExpiresAt.Sub(s.now())
	if remaining > leaseRenewSkew {
		return nil
	}
	if s.leases != nil {
		if err := s.leases.EnsureLeasedReadable(ctx, descriptor); err != nil {
			return err
		}
		return nil
	}
	if remaining > 0 {
		return nil
	}
	return workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("produced resource lease 已过期"))
}
