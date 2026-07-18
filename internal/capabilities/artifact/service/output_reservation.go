package service

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// reservationIDGenerator 仅用于生成排他 reservation ID。
type reservationIDGenerator interface {
	Generate() string
}

// OutputReservationService 为 required Deliverable 分配逻辑写入槽位。
type OutputReservationService struct {
	store artifactcontract.OutputReservationStore
	specs artifactcontract.DeliverableSpecStore
	ids   reservationIDGenerator
	now   func() time.Time
}

func NewOutputReservationService(store artifactcontract.OutputReservationStore, ids reservationIDGenerator) (*OutputReservationService, error) {
	specs, ok := store.(artifactcontract.DeliverableSpecStore)
	if !ok || store == nil || ids == nil {
		return nil, fmt.Errorf("output reservation service 缺少 store/deliverable/id generator")
	}
	return &OutputReservationService{store: store, specs: specs, ids: ids, now: time.Now}, nil
}

func NewOutputReservationServiceWithSpecs(store artifactcontract.OutputReservationStore, specs artifactcontract.DeliverableSpecStore, ids reservationIDGenerator) (*OutputReservationService, error) {
	if store == nil || specs == nil || ids == nil {
		return nil, fmt.Errorf("output reservation service 缺少 store/deliverable/id generator")
	}
	return &OutputReservationService{store: store, specs: specs, ids: ids, now: time.Now}, nil
}

func (s *OutputReservationService) Reserve(ctx context.Context, req artifactcontract.ReserveRequest) (artifactcontract.ReserveResult, error) {
	if missingReservationIdentity(req) {
		return artifactcontract.ReserveResult{}, fmt.Errorf("output reservation 缺少 tenant/run/binding/attempt")
	}
	specs, err := s.specs.ListDeliverables(ctx, req.TenantID, req.RunID)
	if err != nil {
		return artifactcontract.ReserveResult{}, err
	}
	now := s.now().UTC()
	result := artifactcontract.ReserveResult{EnvBindings: map[string]string{}}
	for _, spec := range specs {
		if !spec.Required {
			continue
		}
		name := reservationFileName(spec)
		logical := workmodel.WorkspacePath(path.Join("reserved", spec.ID, name))
		if err := logical.Validate(); err != nil {
			return artifactcontract.ReserveResult{}, fmt.Errorf("deliverable %s logical target 无效: %w", spec.ID, err)
		}
		reservation := artifactmodel.OutputReservation{
			ID: "reservation-" + s.ids.Generate(), TenantID: req.TenantID, RunID: req.RunID,
			BindingID: req.BindingID, DeliverableID: spec.ID, AttemptID: req.AttemptID,
			LogicalTarget: logical, DesiredName: name, MediaType: firstAcceptedMIME(spec.AcceptedMIMEs),
			CreatedAt: now,
		}
		if err := s.store.CreateReservation(ctx, reservation); err != nil {
			return artifactcontract.ReserveResult{}, err
		}
		result.Reservations = append(result.Reservations, reservation)
		if spec.Role == artifactmodel.DeliverableRolePrimary || result.EnvBindings["GENESIS_PRIMARY_OUTPUT"] == "" {
			result.EnvBindings["GENESIS_PRIMARY_OUTPUT"] = string(logical)
		}
		result.EnvBindings["GENESIS_OUTPUT_"+sanitizeEnvKey(spec.ID)] = string(logical)
	}
	return result, nil
}

func missingReservationIdentity(req artifactcontract.ReserveRequest) bool {
	return strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.RunID) == "" ||
		strings.TrimSpace(req.BindingID) == "" || strings.TrimSpace(req.AttemptID) == ""
}

func reservationFileName(spec artifactmodel.DeliverableSpec) string {
	name := strings.TrimSpace(spec.DesiredName)
	if name == "" {
		suffix := ".bin"
		if len(spec.AcceptedSuffix) > 0 {
			suffix = spec.AcceptedSuffix[0]
		}
		name = "deliverable" + suffix
	}
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" || name == "" {
		return "deliverable.bin"
	}
	return name
}

func sanitizeEnvKey(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(value)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "DELIVERABLE"
	}
	return out
}

func firstAcceptedMIME(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var _ artifactcontract.OutputReservationAllocator = (*OutputReservationService)(nil)
