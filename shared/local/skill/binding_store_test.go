package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
)

func TestBindingStorePersistsAndRestoresImmutableBinding(t *testing.T) {
	root := t.TempDir()
	writer, err := NewBindingStore(root)
	if err != nil {
		t.Fatal(err)
	}
	binding := testInvocationBinding("binding-1", "idem-1")
	saved, err := writer.SaveBinding(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	// 返回值必须是副本，调用方修改不能污染 store 内的不可变事实。
	saved.Task = "mutated"
	saved.RuntimeProfile.Dependencies.Runtime.Python[0].Name = "mutated-package"
	saved.Result.Deliverables[0].AcceptedSuffixes[0] = ".mutated"

	reader, err := NewBindingStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.GetBinding(context.Background(), binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Task != binding.Task || got.Package.Digest != binding.Package.Digest || got.RuntimeProfile.Dependencies.Runtime.Python[0].Name != "markitdown" || got.Result.Deliverables[0].AcceptedSuffixes[0] != ".pptx" {
		t.Fatalf("restored binding mismatch: %+v", got)
	}
	byKey, err := reader.GetBindingByIdempotencyKey(context.Background(), binding.IdempotencyKey)
	if err != nil || byKey.ID != binding.ID {
		t.Fatalf("idempotency lookup=%+v err=%v", byKey, err)
	}
	listed, err := reader.ListBindingsByRun(context.Background(), binding.TenantID, binding.RunID)
	if err != nil || len(listed) != 1 || listed[0].ID != binding.ID {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
}

func TestBindingStoreUsesOpaqueFileNameAndKeepsIdempotency(t *testing.T) {
	root := t.TempDir()
	store, err := NewBindingStore(root)
	if err != nil {
		t.Fatal(err)
	}
	binding := testInvocationBinding("..\\outside/binding", "same-work")
	if _, err := store.SaveBinding(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	replayed := testInvocationBinding(binding.ID, binding.IdempotencyKey)
	got, err := store.SaveBinding(context.Background(), replayed)
	if err != nil || got.ID != binding.ID {
		t.Fatalf("idempotent replay=%+v err=%v", got, err)
	}

	entries, err := os.ReadDir(filepath.Join(root, "runtime", "skill-bindings"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != bindingFileName(binding.ID) {
		t.Fatalf("unexpected binding files: %+v", entries)
	}
	if _, err := os.Stat(filepath.Join(root, "outside", "binding.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("binding ID escaped state root: %v", err)
	}
	if _, err := store.SaveBinding(context.Background(), testInvocationBinding("another-id", binding.IdempotencyKey)); err == nil {
		t.Fatal("same idempotency key with a different deterministic ID must conflict")
	}
}

func TestBindingStoreRejectsEmptyRootAndUnknownBinding(t *testing.T) {
	if _, err := NewBindingStore("  "); err == nil {
		t.Fatal("expected empty state root rejection")
	}
	store, err := NewBindingStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetBinding(context.Background(), "missing"); !errors.Is(err, skillcontract.ErrInvocationBindingNotFound) {
		t.Fatalf("expected binding not found, got %v", err)
	}
}

func testInvocationBinding(id, idempotencyKey string) skillmodel.InvocationBinding {
	return skillmodel.InvocationBinding{
		ID: id, TenantID: "tenant", RunID: "run", Handle: "office-ppt", PhysicalSkill: "office-ppt",
		Package:      skillmodel.SkillPackageSnapshot{PackageID: "office-ppt", Digest: "sha256:package"},
		InvocationID: "work", RuntimeProfileID: "work", Task: "生成汇报", IdempotencyKey: idempotencyKey,
		RuntimeProfile:        skillmodel.RuntimeProfile{Dependencies: skillmodel.Dependencies{Runtime: skillmodel.RuntimeDeps{Python: []skillmodel.RuntimePackage{{Name: "markitdown"}}}}},
		Result:                skillmodel.ResultContract{Kind: skillmodel.ResultKindDeliverables, Deliverables: []skillmodel.DeliverableDeclaration{{ID: "deck", AcceptedSuffixes: []string{".pptx"}}}},
		PolicySnapshotVersion: "policy/v1", CreatedAt: time.Now().UTC(),
	}
}
