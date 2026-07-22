package service

import (
	"testing"
)

func TestAdoptionStoreResolveAndVersionLock(t *testing.T) {
	store := NewAdoptionStore()
	store.SetStateRoot(t.TempDir())

	consumerTenant := "tenant-default"
	consumerRun := "run-parent-001"
	producedID := "produced-test-123456"
	ownerRun := "run-subagent-999"

	// 1. 未接纳前不可跨 Run 解析（不再有「假跨 Run 可读」）。
	if _, ok := store.Resolve(consumerTenant, consumerRun, producedID, ""); ok {
		t.Fatalf("expected Resolve to return false before adoption")
	}

	// 2. 父子边界显式接纳。
	rec, err := store.Adopt(AdoptionRecord{
		ConsumerTenantID: consumerTenant,
		ConsumerRunID:    consumerRun,
		ProducedID:       producedID,
		OwnerTenantID:    consumerTenant,
		OwnerRunID:       ownerRun,
		AgentID:          "agent-worker-001",
		ContentHash:      "sha256:aaa",
	})
	if err != nil {
		t.Fatalf("adopt failed: %v", err)
	}
	if rec.OwnerRunID != ownerRun {
		t.Fatalf("unexpected adopted record: %+v", rec)
	}

	// 3. 接纳后可在消费者作用域解析到真实所属子 Run。
	got, ok := store.Resolve(consumerTenant, consumerRun, producedID, "")
	if !ok || got.OwnerRunID != ownerRun || got.OwnerTenantID != consumerTenant {
		t.Fatalf("unexpected Resolve result ok=%v rec=%+v", ok, got)
	}

	// 4. 幂等：同内容再次接纳返回既有记录且不报错。
	if _, err := store.Adopt(AdoptionRecord{ConsumerTenantID: consumerTenant, ConsumerRunID: consumerRun, ProducedID: producedID, OwnerTenantID: consumerTenant, OwnerRunID: ownerRun, ContentHash: "sha256:aaa"}); err != nil {
		t.Fatalf("idempotent adopt should not fail: %v", err)
	}

	// 5. 版本锁：同槽位接纳到不同内容哈希 → 冲突。
	if _, err := store.Adopt(AdoptionRecord{ConsumerTenantID: consumerTenant, ConsumerRunID: consumerRun, ProducedID: producedID, OwnerTenantID: consumerTenant, OwnerRunID: ownerRun, ContentHash: "sha256:bbb"}); err == nil {
		t.Fatalf("expected version conflict on different content hash")
	}

	// 6. 隔离：另一个消费者 Run 未接纳则解析不到（每消费者独立接纳）。
	if _, ok := store.Resolve(consumerTenant, "run-other-parent", producedID, ""); ok {
		t.Fatalf("adoption must be per-consumer; unrelated run should not resolve")
	}
}

func TestAdoptionStoreDiskPersistence(t *testing.T) {
	stateRoot := t.TempDir()
	writer := NewAdoptionStore()
	writer.SetStateRoot(stateRoot)
	if _, err := writer.Adopt(AdoptionRecord{
		ConsumerTenantID: "t", ConsumerRunID: "run-parent", ProducedID: "produced-x",
		OwnerTenantID: "t", OwnerRunID: "run-child",
	}); err != nil {
		t.Fatalf("adopt failed: %v", err)
	}

	// 新实例（无内存缓存）应能通过传入 stateRoot 从磁盘恢复接纳记录。
	reader := NewAdoptionStore()
	got, ok := reader.Resolve("t", "run-parent", "produced-x", stateRoot)
	if !ok || got.OwnerRunID != "run-child" {
		t.Fatalf("expected disk-persisted adoption to resolve, ok=%v rec=%+v", ok, got)
	}
}
