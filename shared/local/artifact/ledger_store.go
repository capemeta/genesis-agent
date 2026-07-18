package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

const ledgerSchemaVersion = 1

// LedgerStore 是本地控制面事实的持久化实现。它与二进制 Artifact Store 使用独立目录。
type LedgerStore struct {
	path string
	mu   *sync.RWMutex
}

type selectionEntry struct {
	TenantID string                             `json:"tenant_id"`
	RunID    string                             `json:"run_id"`
	Value    artifactmodel.DeliverableSelection `json:"value"`
}

type ledgerState struct {
	SchemaVersion int                                       `json:"schema_version"`
	Deliverables  []artifactmodel.DeliverableSpec           `json:"deliverables,omitempty"`
	Selections    []selectionEntry                          `json:"selections,omitempty"`
	Reservations  []artifactmodel.OutputReservation         `json:"reservations,omitempty"`
	QAEvidence    []artifactmodel.QAEvidenceRecord          `json:"qa_evidence,omitempty"`
	Publications  []artifactmodel.ArtifactPublicationRecord `json:"publications,omitempty"`
	Deliveries    []artifactmodel.DeliveryRecord            `json:"deliveries,omitempty"`
}

var ledgerLocks sync.Map

// NewLedgerStore 打开 state root 下的 artifact-control/ledger.json；已有损坏状态会直接报错。
func NewLedgerStore(stateRoot string) (*LedgerStore, error) {
	if strings.TrimSpace(stateRoot) == "" {
		return nil, fmt.Errorf("artifact ledger 缺少 state root")
	}
	path, err := filepath.Abs(filepath.Join(stateRoot, "artifacts", "specs", "ledger.json"))
	if err != nil {
		return nil, fmt.Errorf("解析 artifact ledger 路径: %w", err)
	}
	lock, _ := ledgerLocks.LoadOrStore(strings.ToLower(filepath.Clean(path)), &sync.RWMutex{})
	s := &LedgerStore{path: path, mu: lock.(*sync.RWMutex)}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.read(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *LedgerStore) CreateDeliverable(ctx context.Context, value artifactmodel.DeliverableSpec) error {
	if err := check(ctx, value.Validate); err != nil {
		return err
	}
	return s.change(ctx, func(state *ledgerState) error {
		for _, v := range state.Deliverables {
			if v.TenantID == value.TenantID && v.RunID == value.RunID && v.ID == value.ID {
				return artifactcontract.ErrAlreadyExists
			}
		}
		state.Deliverables = append(state.Deliverables, value)
		return nil
	})
}

func (s *LedgerStore) ListDeliverables(ctx context.Context, tenantID, runID string) ([]artifactmodel.DeliverableSpec, error) {
	state, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]artifactmodel.DeliverableSpec, 0)
	for _, v := range state.Deliverables {
		if v.TenantID == tenantID && v.RunID == runID {
			out = append(out, cloneDeliverable(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *LedgerStore) CreateSelection(ctx context.Context, tenantID, runID string, value artifactmodel.DeliverableSelection) error {
	if err := check(ctx, value.Validate); err != nil {
		return err
	}
	return s.change(ctx, func(state *ledgerState) error {
		found := false
		for _, v := range state.Deliverables {
			if v.TenantID == tenantID && v.RunID == runID && v.ID == value.DeliverableID {
				found = true
				break
			}
		}
		if !found {
			return artifactcontract.ErrNotFound
		}
		for _, v := range state.Selections {
			if v.TenantID == tenantID && v.RunID == runID && v.Value.DeliverableID == value.DeliverableID {
				return artifactcontract.ErrAlreadyExists
			}
		}
		state.Selections = append(state.Selections, selectionEntry{TenantID: tenantID, RunID: runID, Value: value})
		return nil
	})
}

func (s *LedgerStore) ReplaceSelection(ctx context.Context, tenantID, runID string, value artifactmodel.DeliverableSelection) error {
	if err := check(ctx, value.Validate); err != nil {
		return err
	}
	return s.change(ctx, func(state *ledgerState) error {
		foundDeliverable := false
		for _, v := range state.Deliverables {
			if v.TenantID == tenantID && v.RunID == runID && v.ID == value.DeliverableID {
				foundDeliverable = true
				break
			}
		}
		if !foundDeliverable {
			return artifactcontract.ErrNotFound
		}
		for i, v := range state.Selections {
			if v.TenantID == tenantID && v.RunID == runID && v.Value.DeliverableID == value.DeliverableID {
				state.Selections[i].Value = value
				return nil
			}
		}
		return artifactcontract.ErrNotFound
	})
}

func (s *LedgerStore) GetSelection(ctx context.Context, tenantID, runID, deliverableID string) (artifactmodel.DeliverableSelection, error) {
	state, err := s.load(ctx)
	if err != nil {
		return artifactmodel.DeliverableSelection{}, err
	}
	for _, v := range state.Selections {
		if v.TenantID == tenantID && v.RunID == runID && v.Value.DeliverableID == deliverableID {
			return v.Value, nil
		}
	}
	return artifactmodel.DeliverableSelection{}, artifactcontract.ErrNotFound
}

func (s *LedgerStore) CreateReservation(ctx context.Context, value artifactmodel.OutputReservation) error {
	if err := check(ctx, value.Validate); err != nil {
		return err
	}
	return s.change(ctx, func(state *ledgerState) error {
		for _, v := range state.Reservations {
			if v.TenantID == value.TenantID && v.RunID == value.RunID && (v.ID == value.ID || (v.DeliverableID == value.DeliverableID && v.AttemptID == value.AttemptID)) {
				return artifactcontract.ErrAlreadyExists
			}
		}
		state.Reservations = append(state.Reservations, value)
		return nil
	})
}

func (s *LedgerStore) ListReservations(ctx context.Context, tenantID, runID, deliverableID string) ([]artifactmodel.OutputReservation, error) {
	state, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]artifactmodel.OutputReservation, 0)
	for _, v := range state.Reservations {
		if v.TenantID == tenantID && v.RunID == runID && (deliverableID == "" || v.DeliverableID == deliverableID) {
			out = append(out, cloneReservation(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *LedgerStore) CreateQAEvidence(ctx context.Context, value artifactmodel.QAEvidenceRecord) error {
	if err := check(ctx, value.Validate); err != nil {
		return err
	}
	return s.change(ctx, func(state *ledgerState) error {
		for _, v := range state.QAEvidence {
			if v.TenantID == value.TenantID && v.RunID == value.RunID && v.ID == value.ID {
				return artifactcontract.ErrAlreadyExists
			}
		}
		state.QAEvidence = append(state.QAEvidence, value)
		return nil
	})
}

func (s *LedgerStore) ListQAEvidence(ctx context.Context, tenantID, runID, deliverableID string) ([]artifactmodel.QAEvidenceRecord, error) {
	state, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]artifactmodel.QAEvidenceRecord, 0)
	for _, v := range state.QAEvidence {
		if v.TenantID == tenantID && v.RunID == runID && (deliverableID == "" || v.DeliverableID == deliverableID) {
			out = append(out, cloneEvidence(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *LedgerStore) CreatePublication(ctx context.Context, value artifactmodel.ArtifactPublicationRecord) (artifactmodel.ArtifactPublicationRecord, bool, error) {
	if err := check(ctx, value.Validate); err != nil {
		return value, false, err
	}
	var result artifactmodel.ArtifactPublicationRecord
	created := false
	err := s.change(ctx, func(state *ledgerState) error {
		for _, v := range state.Publications {
			if v.TenantID == value.TenantID && v.IdempotencyKey == value.IdempotencyKey {
				if samePublicationRequest(v, value) {
					result = v
					return nil
				}
				return artifactcontract.ErrIdempotencyConflict
			}
			if v.TenantID == value.TenantID && v.ID == value.ID {
				return artifactcontract.ErrAlreadyExists
			}
		}
		state.Publications = append(state.Publications, value)
		result, created = value, true
		return nil
	})
	return result, created, err
}

func (s *LedgerStore) GetPublication(ctx context.Context, tenantID, id string) (artifactmodel.ArtifactPublicationRecord, error) {
	state, err := s.load(ctx)
	if err != nil {
		return artifactmodel.ArtifactPublicationRecord{}, err
	}
	for _, v := range state.Publications {
		if v.TenantID == tenantID && v.ID == id {
			return v, nil
		}
	}
	return artifactmodel.ArtifactPublicationRecord{}, artifactcontract.ErrNotFound
}

func (s *LedgerStore) GetPublicationByIdempotencyKey(ctx context.Context, tenantID, key string) (artifactmodel.ArtifactPublicationRecord, error) {
	state, err := s.load(ctx)
	if err != nil {
		return artifactmodel.ArtifactPublicationRecord{}, err
	}
	for _, v := range state.Publications {
		if v.TenantID == tenantID && v.IdempotencyKey == key {
			return v, nil
		}
	}
	return artifactmodel.ArtifactPublicationRecord{}, artifactcontract.ErrNotFound
}

func (s *LedgerStore) UpdatePublication(ctx context.Context, expected uint64, value artifactmodel.ArtifactPublicationRecord) (artifactmodel.ArtifactPublicationRecord, error) {
	var result artifactmodel.ArtifactPublicationRecord
	err := s.change(ctx, func(state *ledgerState) error {
		for i, old := range state.Publications {
			if old.TenantID != value.TenantID || old.ID != value.ID {
				continue
			}
			if old.Revision != expected {
				return artifactcontract.ErrRevisionConflict
			}
			if old.IdempotencyKey != value.IdempotencyKey || old.RunID != value.RunID || old.DeliverableID != value.DeliverableID || old.ProducedResourceID != value.ProducedResourceID {
				return artifactcontract.ErrIdempotencyConflict
			}
			if !publicationTransition(old.Status, value.Status) {
				return artifactcontract.ErrRevisionConflict
			}
			value.Revision = expected + 1
			if err := value.Validate(); err != nil {
				return err
			}
			state.Publications[i], result = value, value
			return nil
		}
		return artifactcontract.ErrNotFound
	})
	return result, err
}

func (s *LedgerStore) ListPublications(ctx context.Context, tenantID, runID, deliverableID string) ([]artifactmodel.ArtifactPublicationRecord, error) {
	state, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]artifactmodel.ArtifactPublicationRecord, 0)
	for _, v := range state.Publications {
		if v.TenantID == tenantID && v.RunID == runID && (deliverableID == "" || v.DeliverableID == deliverableID) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *LedgerStore) CreateDelivery(ctx context.Context, value artifactmodel.DeliveryRecord) (artifactmodel.DeliveryRecord, bool, error) {
	if err := check(ctx, value.Validate); err != nil {
		return value, false, err
	}
	var result artifactmodel.DeliveryRecord
	created := false
	err := s.change(ctx, func(state *ledgerState) error {
		for _, v := range state.Deliveries {
			if v.TenantID == value.TenantID && v.IdempotencyKey == value.IdempotencyKey {
				if sameDeliveryRequest(v, value) {
					result = v
					return nil
				}
				return artifactcontract.ErrIdempotencyConflict
			}
			if v.TenantID == value.TenantID && v.ID == value.ID {
				return artifactcontract.ErrAlreadyExists
			}
		}
		state.Deliveries = append(state.Deliveries, value)
		result, created = value, true
		return nil
	})
	return result, created, err
}

func (s *LedgerStore) GetDelivery(ctx context.Context, tenantID, id string) (artifactmodel.DeliveryRecord, error) {
	state, err := s.load(ctx)
	if err != nil {
		return artifactmodel.DeliveryRecord{}, err
	}
	for _, v := range state.Deliveries {
		if v.TenantID == tenantID && v.ID == id {
			return v, nil
		}
	}
	return artifactmodel.DeliveryRecord{}, artifactcontract.ErrNotFound
}

func (s *LedgerStore) GetDeliveryByIdempotencyKey(ctx context.Context, tenantID, key string) (artifactmodel.DeliveryRecord, error) {
	state, err := s.load(ctx)
	if err != nil {
		return artifactmodel.DeliveryRecord{}, err
	}
	for _, v := range state.Deliveries {
		if v.TenantID == tenantID && v.IdempotencyKey == key {
			return v, nil
		}
	}
	return artifactmodel.DeliveryRecord{}, artifactcontract.ErrNotFound
}

func (s *LedgerStore) UpdateDelivery(ctx context.Context, expected uint64, value artifactmodel.DeliveryRecord) (artifactmodel.DeliveryRecord, error) {
	var result artifactmodel.DeliveryRecord
	err := s.change(ctx, func(state *ledgerState) error {
		for i, old := range state.Deliveries {
			if old.TenantID != value.TenantID || old.ID != value.ID {
				continue
			}
			if old.Revision != expected {
				return artifactcontract.ErrRevisionConflict
			}
			if old.IdempotencyKey != value.IdempotencyKey || old.ArtifactID != value.ArtifactID || old.PublicationID != value.PublicationID {
				return artifactcontract.ErrIdempotencyConflict
			}
			if !deliveryTransition(old.Status, value.Status) {
				return artifactcontract.ErrRevisionConflict
			}
			value.Revision = expected + 1
			if err := value.Validate(); err != nil {
				return err
			}
			state.Deliveries[i], result = value, value
			return nil
		}
		return artifactcontract.ErrNotFound
	})
	return result, err
}

func (s *LedgerStore) ListDeliveries(ctx context.Context, tenantID, runID, deliverableID string) ([]artifactmodel.DeliveryRecord, error) {
	state, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]artifactmodel.DeliveryRecord, 0)
	for _, v := range state.Deliveries {
		if v.TenantID == tenantID && v.RunID == runID && (deliverableID == "" || v.DeliverableID == deliverableID) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *LedgerStore) load(ctx context.Context) (ledgerState, error) {
	if err := ctx.Err(); err != nil {
		return ledgerState{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.read()
}

func (s *LedgerStore) change(ctx context.Context, mutate func(*ledgerState) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.read()
	if err != nil {
		return err
	}
	if err := mutate(&state); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.write(state)
}

func (s *LedgerStore) read() (ledgerState, error) {
	state := ledgerState{SchemaVersion: ledgerSchemaVersion}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("读取 artifact ledger: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return ledgerState{}, fmt.Errorf("解析 artifact ledger: %w", err)
	}
	if err := validateLedger(state); err != nil {
		return ledgerState{}, fmt.Errorf("校验 artifact ledger: %w", err)
	}
	return state, nil
}

func (s *LedgerStore) write(state ledgerState) error {
	if err := validateLedger(state); err != nil {
		return fmt.Errorf("写入前校验 artifact ledger: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("创建 artifact ledger 目录: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 artifact ledger: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".ledger-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceLedgerFile(name, s.path); err != nil {
		return fmt.Errorf("提交 artifact ledger: %w", err)
	}
	committed = true
	return nil
}

func validateLedger(state ledgerState) error {
	if state.SchemaVersion != ledgerSchemaVersion {
		return fmt.Errorf("不支持 schema_version %d", state.SchemaVersion)
	}
	deliverables := map[string]struct{}{}
	selections := map[string]struct{}{}
	reservations := map[string]struct{}{}
	slots := map[string]struct{}{}
	evidence := map[string]struct{}{}
	publications := map[string]struct{}{}
	publicationKeys := map[string]struct{}{}
	deliveries := map[string]struct{}{}
	deliveryKeys := map[string]struct{}{}
	for _, v := range state.Deliverables {
		if err := v.Validate(); err != nil {
			return err
		}
		if !unique(deliverables, scoped(v.TenantID, v.RunID, v.ID)) {
			return fmt.Errorf("deliverable 重复")
		}
	}
	for _, v := range state.Selections {
		if strings.TrimSpace(v.TenantID) == "" || strings.TrimSpace(v.RunID) == "" {
			return fmt.Errorf("selection 缺少 tenant/run")
		}
		if err := v.Value.Validate(); err != nil {
			return err
		}
		key := scoped(v.TenantID, v.RunID, v.Value.DeliverableID)
		if _, ok := deliverables[key]; !ok {
			return fmt.Errorf("selection 引用不存在 deliverable")
		}
		if !unique(selections, key) {
			return fmt.Errorf("selection 重复")
		}
	}
	for _, v := range state.Reservations {
		if err := v.Validate(); err != nil {
			return err
		}
		if !unique(reservations, scoped(v.TenantID, v.RunID, v.ID)) || !unique(slots, scoped(v.TenantID, v.RunID, v.DeliverableID, v.AttemptID)) {
			return fmt.Errorf("reservation id/slot 重复")
		}
	}
	for _, v := range state.QAEvidence {
		if err := v.Validate(); err != nil {
			return err
		}
		if !unique(evidence, scoped(v.TenantID, v.RunID, v.ID)) {
			return fmt.Errorf("qa evidence 重复")
		}
	}
	for _, v := range state.Publications {
		if err := v.Validate(); err != nil {
			return err
		}
		if !unique(publications, scoped(v.TenantID, v.ID)) || !unique(publicationKeys, scoped(v.TenantID, v.IdempotencyKey)) {
			return fmt.Errorf("publication id/idempotency key 重复")
		}
	}
	for _, v := range state.Deliveries {
		if err := v.Validate(); err != nil {
			return err
		}
		if !unique(deliveries, scoped(v.TenantID, v.ID)) || !unique(deliveryKeys, scoped(v.TenantID, v.IdempotencyKey)) {
			return fmt.Errorf("delivery id/idempotency key 重复")
		}
	}
	return nil
}

func unique(set map[string]struct{}, key string) bool {
	if _, ok := set[key]; ok {
		return false
	}
	set[key] = struct{}{}
	return true
}
func scoped(values ...string) string { return strings.Join(values, "\x00") }
func check(ctx context.Context, validate func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return validate()
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
	return a.ID == b.ID && a.TenantID == b.TenantID && a.RunID == b.RunID && a.ProducedResourceID == b.ProducedResourceID && a.DeliverableID == b.DeliverableID && a.DesiredName == b.DesiredName && a.GateVersion == b.GateVersion && a.IdempotencyKey == b.IdempotencyKey
}
func sameDeliveryRequest(a, b artifactmodel.DeliveryRecord) bool {
	return a.ID == b.ID && a.TenantID == b.TenantID && a.RunID == b.RunID && a.DeliverableID == b.DeliverableID && a.PublicationID == b.PublicationID && a.ArtifactID == b.ArtifactID && a.Target == b.Target && a.TargetKind == b.TargetKind && a.TargetName == b.TargetName && a.IdempotencyKey == b.IdempotencyKey
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

var _ artifactcontract.DeliverableSpecStore = (*LedgerStore)(nil)
var _ artifactcontract.DeliverableSelectionStore = (*LedgerStore)(nil)
var _ artifactcontract.OutputReservationStore = (*LedgerStore)(nil)
var _ artifactcontract.QAEvidenceStore = (*LedgerStore)(nil)
var _ artifactcontract.ArtifactPublicationStore = (*LedgerStore)(nil)
var _ artifactcontract.DeliveryRecordStore = (*LedgerStore)(nil)
