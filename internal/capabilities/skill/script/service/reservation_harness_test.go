package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	artifactmemory "genesis-agent/internal/capabilities/artifact/adapter/memory"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

func TestMergeReservedEnvOverridesSameKeys(t *testing.T) {
	base := map[string]string{"OUTPUT_DIR": "work", "GENESIS_PRIMARY_OUTPUT": "old"}
	got := mergeReservedEnv(base, map[string]string{"GENESIS_PRIMARY_OUTPUT": "reserved/a/deck.pptx"})
	if got["GENESIS_PRIMARY_OUTPUT"] != "reserved/a/deck.pptx" || got["OUTPUT_DIR"] != "work" {
		t.Fatalf("got=%v", got)
	}
}

func TestCollectReservedHitsLocalSession(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := ensureLocalReservationDirs(skillDir, []artifactmodel.OutputReservation{{
		LogicalTarget: "reserved/d1/deck.pptx",
	}}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(skillDir, "reserved", "d1", "deck.pptx")
	if err := os.WriteFile(target, []byte("pptx"), 0o644); err != nil {
		t.Fatal(err)
	}
	observed, err := snapshotLocalFiles(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	hits := collectReservedHitsLocal(skillDir, skillDir, []artifactmodel.OutputReservation{{
		LogicalTarget: "reserved/d1/deck.pptx",
	}}, observed)
	if len(hits) != 1 || hits[0] != "reserved/d1/deck.pptx" {
		t.Fatalf("hits=%v", hits)
	}
}

func TestCollectReservedHitsLocalTaskJobSeparatedOutput(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "work", "skill")
	outputDir := filepath.Join(root, "output", "binding")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalReservationDirs(outputDir, []artifactmodel.OutputReservation{{
		LogicalTarget: "reserved/d1/deck.pptx",
	}}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outputDir, "reserved", "d1", "deck.pptx")
	if err := os.WriteFile(target, []byte("pptx"), 0o644); err != nil {
		t.Fatal(err)
	}
	observed := map[string]fileFingerprint{}
	hits := collectReservedHitsLocal(outputDir, skillDir, []artifactmodel.OutputReservation{{
		LogicalTarget: "reserved/d1/deck.pptx",
	}}, observed)
	if len(hits) != 1 || hits[0] != "reserved/d1/deck.pptx" {
		t.Fatalf("hits=%v observed=%v", hits, observed)
	}
	if observed["reserved/d1/deck.pptx"].Size != 4 {
		t.Fatalf("observed fingerprint=%+v", observed["reserved/d1/deck.pptx"])
	}
}

func TestFilterProducedByDeliverablesKeepsReservationAndMatchingDiff(t *testing.T) {
	store := artifactmemory.NewStore()
	now := time.Now().UTC()
	if err := store.CreateDeliverable(context.Background(), artifactmodel.DeliverableSpec{
		ID: "d1", TenantID: "t", RunID: "r", Required: true, Role: artifactmodel.DeliverableRolePrimary,
		AcceptedSuffix: []string{".pptx"}, DeliveryPolicy: "out", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{deliverables: store}
	got, err := svc.filterProducedByDeliverables(context.Background(), "t", "r",
		[]string{"reserved/d1/deck.pptx"},
		[]string{"reserved/d1/deck.pptx", "notes.txt", "qa.png", "slide.pptx"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "reserved/d1/deck.pptx" || got[1] != "slide.pptx" {
		t.Fatalf("got=%v", got)
	}
}
