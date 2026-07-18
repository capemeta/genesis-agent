package memory

import (
	"context"
	"sort"
	"sync"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

// Store 是控制面端口的并发安全内存实现，主要用于契约测试和本地装配准备。
type Store struct {
	mu               sync.RWMutex
	deliverables     map[string]artifactmodel.DeliverableSpec
	selections       map[string]artifactmodel.DeliverableSelection
	reservations     map[string]artifactmodel.OutputReservation
	reservationSlots map[string]string
	evidence         map[string]artifactmodel.QAEvidenceRecord
	publications     map[string]artifactmodel.ArtifactPublicationRecord
	publicationKeys  map[string]string
	deliveries       map[string]artifactmodel.DeliveryRecord
	deliveryKeys     map[string]string
}

func NewStore() *Store {
	return &Store{
		deliverables: map[string]artifactmodel.DeliverableSpec{}, selections: map[string]artifactmodel.DeliverableSelection{},
		reservations: map[string]artifactmodel.OutputReservation{}, reservationSlots: map[string]string{}, evidence: map[string]artifactmodel.QAEvidenceRecord{},
		publications: map[string]artifactmodel.ArtifactPublicationRecord{}, publicationKeys: map[string]string{}, deliveries: map[string]artifactmodel.DeliveryRecord{}, deliveryKeys: map[string]string{},
	}
}

func (s *Store) CreateDeliverable(ctx context.Context, value artifactmodel.DeliverableSpec) error {
	if err := contextAndValidate(ctx, value.Validate); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(value.TenantID, value.RunID, value.ID)
	if _, ok := s.deliverables[key]; ok {
		return artifactcontract.ErrAlreadyExists
	}
	s.deliverables[key] = cloneDeliverable(value)
	return nil
}

func (s *Store) ListDeliverables(ctx context.Context, tenantID, runID string) ([]artifactmodel.DeliverableSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []artifactmodel.DeliverableSpec{}
	for _, value := range s.deliverables {
		if value.TenantID == tenantID && value.RunID == runID {
			out = append(out, cloneDeliverable(value))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) CreateSelection(ctx context.Context, tenantID, runID string, value artifactmodel.DeliverableSelection) error {
	if err := contextAndValidate(ctx, value.Validate); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(tenantID, runID, value.DeliverableID)
	if _, ok := s.deliverables[key]; !ok {
		return artifactcontract.ErrNotFound
	}
	if _, ok := s.selections[key]; ok {
		return artifactcontract.ErrAlreadyExists
	}
	s.selections[key] = value
	return nil
}

func (s *Store) ReplaceSelection(ctx context.Context, tenantID, runID string, value artifactmodel.DeliverableSelection) error {
	if err := contextAndValidate(ctx, value.Validate); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(tenantID, runID, value.DeliverableID)
	if _, ok := s.deliverables[key]; !ok {
		return artifactcontract.ErrNotFound
	}
	if _, ok := s.selections[key]; !ok {
		return artifactcontract.ErrNotFound
	}
	s.selections[key] = value
	return nil
}

func (s *Store) GetSelection(ctx context.Context, tenantID, runID, deliverableID string) (artifactmodel.DeliverableSelection, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.DeliverableSelection{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.selections[scoped(tenantID, runID, deliverableID)]
	if !ok {
		return value, artifactcontract.ErrNotFound
	}
	return value, nil
}

func (s *Store) CreateReservation(ctx context.Context, value artifactmodel.OutputReservation) error {
	if err := contextAndValidate(ctx, value.Validate); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(value.TenantID, value.RunID, value.ID)
	slot := scoped(value.TenantID, value.RunID, value.DeliverableID, value.AttemptID)
	if _, ok := s.reservations[key]; ok {
		return artifactcontract.ErrAlreadyExists
	}
	if _, ok := s.reservationSlots[slot]; ok {
		return artifactcontract.ErrAlreadyExists
	}
	s.reservations[key] = cloneReservation(value)
	s.reservationSlots[slot] = key
	return nil
}

func (s *Store) ListReservations(ctx context.Context, tenantID, runID, deliverableID string) ([]artifactmodel.OutputReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []artifactmodel.OutputReservation{}
	for _, value := range s.reservations {
		if value.TenantID == tenantID && value.RunID == runID && (deliverableID == "" || value.DeliverableID == deliverableID) {
			out = append(out, cloneReservation(value))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) CreateQAEvidence(ctx context.Context, value artifactmodel.QAEvidenceRecord) error {
	if err := contextAndValidate(ctx, value.Validate); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(value.TenantID, value.RunID, value.ID)
	if _, ok := s.evidence[key]; ok {
		return artifactcontract.ErrAlreadyExists
	}
	s.evidence[key] = cloneEvidence(value)
	return nil
}

func (s *Store) ListQAEvidence(ctx context.Context, tenantID, runID, deliverableID string) ([]artifactmodel.QAEvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []artifactmodel.QAEvidenceRecord{}
	for _, v := range s.evidence {
		if v.TenantID == tenantID && v.RunID == runID && (deliverableID == "" || v.DeliverableID == deliverableID) {
			out = append(out, cloneEvidence(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) CreatePublication(ctx context.Context, value artifactmodel.ArtifactPublicationRecord) (artifactmodel.ArtifactPublicationRecord, bool, error) {
	if err := contextAndValidate(ctx, value.Validate); err != nil {
		return value, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := scoped(value.TenantID, value.ID)
	ik := scoped(value.TenantID, value.IdempotencyKey)
	if existingID, ok := s.publicationKeys[ik]; ok {
		existing := s.publications[existingID]
		if samePublicationRequest(existing, value) {
			return existing, false, nil
		}
		return artifactmodel.ArtifactPublicationRecord{}, false, artifactcontract.ErrIdempotencyConflict
	}
	if _, ok := s.publications[id]; ok {
		return artifactmodel.ArtifactPublicationRecord{}, false, artifactcontract.ErrAlreadyExists
	}
	s.publications[id] = value
	s.publicationKeys[ik] = id
	return value, true, nil
}

func (s *Store) GetPublication(ctx context.Context, tenantID, id string) (artifactmodel.ArtifactPublicationRecord, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.ArtifactPublicationRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.publications[scoped(tenantID, id)]
	if !ok {
		return v, artifactcontract.ErrNotFound
	}
	return v, nil
}
func (s *Store) GetPublicationByIdempotencyKey(ctx context.Context, tenantID, key string) (artifactmodel.ArtifactPublicationRecord, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.ArtifactPublicationRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.publicationKeys[scoped(tenantID, key)]
	if !ok {
		return artifactmodel.ArtifactPublicationRecord{}, artifactcontract.ErrNotFound
	}
	return s.publications[id], nil
}

func (s *Store) UpdatePublication(ctx context.Context, expected uint64, value artifactmodel.ArtifactPublicationRecord) (artifactmodel.ArtifactPublicationRecord, error) {
	if err := ctx.Err(); err != nil {
		return value, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(value.TenantID, value.ID)
	old, ok := s.publications[key]
	if !ok {
		return value, artifactcontract.ErrNotFound
	}
	if old.Revision != expected {
		return value, artifactcontract.ErrRevisionConflict
	}
	if old.IdempotencyKey != value.IdempotencyKey || old.RunID != value.RunID || old.DeliverableID != value.DeliverableID || old.ProducedResourceID != value.ProducedResourceID {
		return value, artifactcontract.ErrIdempotencyConflict
	}
	if !publicationTransition(old.Status, value.Status) {
		return value, artifactcontract.ErrRevisionConflict
	}
	value.Revision = expected + 1
	if err := value.Validate(); err != nil {
		return value, err
	}
	s.publications[key] = value
	return value, nil
}

func (s *Store) ListPublications(ctx context.Context, tenantID, runID, deliverableID string) ([]artifactmodel.ArtifactPublicationRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []artifactmodel.ArtifactPublicationRecord{}
	for _, v := range s.publications {
		if v.TenantID == tenantID && v.RunID == runID && (deliverableID == "" || v.DeliverableID == deliverableID) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) CreateDelivery(ctx context.Context, value artifactmodel.DeliveryRecord) (artifactmodel.DeliveryRecord, bool, error) {
	if err := contextAndValidate(ctx, value.Validate); err != nil {
		return value, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := scoped(value.TenantID, value.ID)
	ik := scoped(value.TenantID, value.IdempotencyKey)
	if existingID, ok := s.deliveryKeys[ik]; ok {
		existing := s.deliveries[existingID]
		if sameDeliveryRequest(existing, value) {
			return existing, false, nil
		}
		return artifactmodel.DeliveryRecord{}, false, artifactcontract.ErrIdempotencyConflict
	}
	if _, ok := s.deliveries[id]; ok {
		return value, false, artifactcontract.ErrAlreadyExists
	}
	s.deliveries[id] = value
	s.deliveryKeys[ik] = id
	return value, true, nil
}
func (s *Store) GetDelivery(ctx context.Context, tenantID, id string) (artifactmodel.DeliveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.DeliveryRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.deliveries[scoped(tenantID, id)]
	if !ok {
		return v, artifactcontract.ErrNotFound
	}
	return v, nil
}
func (s *Store) GetDeliveryByIdempotencyKey(ctx context.Context, tenantID, key string) (artifactmodel.DeliveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return artifactmodel.DeliveryRecord{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.deliveryKeys[scoped(tenantID, key)]
	if !ok {
		return artifactmodel.DeliveryRecord{}, artifactcontract.ErrNotFound
	}
	return s.deliveries[id], nil
}
func (s *Store) UpdateDelivery(ctx context.Context, expected uint64, value artifactmodel.DeliveryRecord) (artifactmodel.DeliveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return value, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(value.TenantID, value.ID)
	old, ok := s.deliveries[key]
	if !ok {
		return value, artifactcontract.ErrNotFound
	}
	if old.Revision != expected {
		return value, artifactcontract.ErrRevisionConflict
	}
	if old.IdempotencyKey != value.IdempotencyKey || old.ArtifactID != value.ArtifactID || old.PublicationID != value.PublicationID {
		return value, artifactcontract.ErrIdempotencyConflict
	}
	if !deliveryTransition(old.Status, value.Status) {
		return value, artifactcontract.ErrRevisionConflict
	}
	value.Revision = expected + 1
	if err := value.Validate(); err != nil {
		return value, err
	}
	s.deliveries[key] = value
	return value, nil
}
func (s *Store) ListDeliveries(ctx context.Context, tenantID, runID, deliverableID string) ([]artifactmodel.DeliveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []artifactmodel.DeliveryRecord{}
	for _, v := range s.deliveries {
		if v.TenantID == tenantID && v.RunID == runID && (deliverableID == "" || v.DeliverableID == deliverableID) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func publicationTransition(a, b artifactmodel.PublicationStatus) bool {
	if a == b {
		return true
	}
	switch a {
	case artifactmodel.PublicationPending:
		return b == artifactmodel.PublicationStaging || b == artifactmodel.PublicationFailed
	case artifactmodel.PublicationStaging:
		return b == artifactmodel.PublicationGated || b == artifactmodel.PublicationFailed
	case artifactmodel.PublicationGated:
		return b == artifactmodel.PublicationCommitted || b == artifactmodel.PublicationFailed
	case artifactmodel.PublicationFailed:
		return b == artifactmodel.PublicationStaging
	}
	return false
}
func deliveryTransition(a, b artifactmodel.DeliveryStatus) bool {
	if a == b {
		return true
	}
	switch a {
	case artifactmodel.DeliveryPending:
		return b == artifactmodel.DeliveryDelivering || b == artifactmodel.DeliveryFailed
	case artifactmodel.DeliveryDelivering:
		return b == artifactmodel.DeliverySucceeded || b == artifactmodel.DeliveryFailed
	case artifactmodel.DeliveryFailed:
		return b == artifactmodel.DeliveryDelivering
	}
	return false
}
func contextAndValidate(ctx context.Context, validate func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return validate()
}
func scoped(values ...string) string {
	out := ""
	for _, v := range values {
		out += v + "\x00"
	}
	return out
}
func cloneDeliverable(v artifactmodel.DeliverableSpec) artifactmodel.DeliverableSpec {
	v.AcceptedMIMEs = append([]string(nil), v.AcceptedMIMEs...)
	v.AcceptedSuffix = append([]string(nil), v.AcceptedSuffix...)
	return v
}
func cloneReservation(v artifactmodel.OutputReservation) artifactmodel.OutputReservation {
	if v.ExpiresAt != nil {
		x := *v.ExpiresAt
		v.ExpiresAt = &x
	}
	return v
}
func cloneEvidence(v artifactmodel.QAEvidenceRecord) artifactmodel.QAEvidenceRecord {
	v.EvidenceResourceIDs = append([]string(nil), v.EvidenceResourceIDs...)
	return v
}

func samePublicationRequest(a, b artifactmodel.ArtifactPublicationRecord) bool {
	return a.ID == b.ID && a.TenantID == b.TenantID && a.RunID == b.RunID &&
		a.ProducedResourceID == b.ProducedResourceID && a.DeliverableID == b.DeliverableID &&
		a.DesiredName == b.DesiredName && a.GateVersion == b.GateVersion && a.IdempotencyKey == b.IdempotencyKey
}

func sameDeliveryRequest(a, b artifactmodel.DeliveryRecord) bool {
	return a.ID == b.ID && a.TenantID == b.TenantID && a.RunID == b.RunID &&
		a.DeliverableID == b.DeliverableID && a.PublicationID == b.PublicationID &&
		a.ArtifactID == b.ArtifactID && a.Target == b.Target && a.TargetKind == b.TargetKind &&
		a.TargetName == b.TargetName && a.IdempotencyKey == b.IdempotencyKey
}

var _ artifactcontract.DeliverableSpecStore = (*Store)(nil)
var _ artifactcontract.DeliverableSelectionStore = (*Store)(nil)
var _ artifactcontract.OutputReservationStore = (*Store)(nil)
var _ artifactcontract.QAEvidenceStore = (*Store)(nil)
var _ artifactcontract.ArtifactPublicationStore = (*Store)(nil)
var _ artifactcontract.DeliveryRecordStore = (*Store)(nil)
