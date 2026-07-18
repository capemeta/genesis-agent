package service

import (
	"context"
	"testing"
	"time"

	artifactmemory "genesis-agent/internal/capabilities/artifact/adapter/memory"
	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

type fixedReservationIDs struct{ values []string }

func (g *fixedReservationIDs) Generate() string {
	if len(g.values) == 0 {
		return "id"
	}
	v := g.values[0]
	g.values = g.values[1:]
	return v
}

func TestOutputReservationServiceCreatesExclusiveSlotsAndHarnessEnv(t *testing.T) {
	ctx := context.Background()
	store := artifactmemory.NewStore()
	now := time.Now().UTC()
	spec := artifactmodel.DeliverableSpec{
		ID: "deliverable-primary", TenantID: "tenant", RunID: "run", Required: true,
		Role: artifactmodel.DeliverableRolePrimary, DesiredName: "deck.pptx",
		AcceptedSuffix: []string{".pptx"}, DeliveryPolicy: "inbox", CreatedAt: now,
	}
	if err := store.CreateDeliverable(ctx, spec); err != nil {
		t.Fatal(err)
	}
	_ = store.CreateDeliverable(ctx, artifactmodel.DeliverableSpec{
		ID: "thumb", TenantID: "tenant", RunID: "run", Required: false,
		Role: artifactmodel.DeliverableRoleSupporting, DeliveryPolicy: "none", CreatedAt: now,
	})

	svc, err := NewOutputReservationService(store, &fixedReservationIDs{values: []string{"reservation-1"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Reserve(ctx, artifactcontract.ReserveRequest{
		TenantID: "tenant", RunID: "run", BindingID: "binding-1", AttemptID: "attempt-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reservations) != 1 {
		t.Fatalf("reservations=%d", len(result.Reservations))
	}
	got := result.Reservations[0]
	if got.LogicalTarget != "reserved/deliverable-primary/deck.pptx" {
		t.Fatalf("logical_target=%q", got.LogicalTarget)
	}
	if got.DesiredName != "deck.pptx" || result.EnvBindings["GENESIS_PRIMARY_OUTPUT"] != "reserved/deliverable-primary/deck.pptx" {
		t.Fatalf("reservation=%+v env=%v", got, result.EnvBindings)
	}
	if _, ok := result.EnvBindings["OUTPUT_DIR"]; ok {
		t.Fatal("reservation service 不得改写 OUTPUT_DIR；由 Harness 映射物理根")
	}

	_, err = svc.Reserve(ctx, artifactcontract.ReserveRequest{
		TenantID: "tenant", RunID: "run", BindingID: "binding-1", AttemptID: "attempt-1",
	})
	if err == nil {
		t.Fatal("同一 attempt 重复 Reserve 必须失败")
	}
}

func TestOutputReservationServiceRequiresAttemptIdentity(t *testing.T) {
	svc, err := NewOutputReservationService(artifactmemory.NewStore(), &fixedReservationIDs{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Reserve(context.Background(), artifactcontract.ReserveRequest{TenantID: "t", RunID: "r", BindingID: "b"})
	if err == nil {
		t.Fatal("缺少 attempt_id 必须失败")
	}
}
