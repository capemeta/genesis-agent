package contract_test

import (
	"context"
	"os"
	"testing"
	"time"

	"genesis-agent/internal/capabilities/tasklist/adapter/memory"
	"genesis-agent/internal/capabilities/tasklist/contract"
	"genesis-agent/internal/capabilities/tasklist/model"
	localtasklist "genesis-agent/shared/local/tasklist"
)

// runRepositoryContractSuite 规范所有 Repository 实现需遵循的契约测试集
func runRepositoryContractSuite(t *testing.T, repo contract.Repository) {
	ctx := context.Background()
	sessionID := "test_session_contract_1"

	// 1. 读取不存在的 session 返回 nil
	got, err := repo.GetTaskList(ctx, sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for non-existent session, got: %+v", got)
	}

	// 2. 保存 Version 1 快照
	now := time.Now().Truncate(time.Millisecond)
	plan1 := &model.TaskList{
		SessionID: sessionID,
		Version:   1,
		Steps: []model.Step{
			{ID: "task_1", Title: "步骤 1", Status: model.StepStatusPending},
		},
		UpdatedAt: now,
	}
	rev1 := &model.RevisionLog{
		Version:     1,
		Explanation: "初始化步骤",
		Operator:    "user",
		Timestamp:   now,
	}
	if err := repo.SaveTaskList(ctx, plan1, rev1); err != nil {
		t.Fatalf("SaveTaskList v1 failed: %v", err)
	}

	// 3. 校验读取结果
	saved, err := repo.GetTaskList(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetTaskList failed: %v", err)
	}
	if saved == nil || saved.Version != 1 || len(saved.Steps) != 1 {
		t.Fatalf("unexpected saved plan: %+v", saved)
	}

	// 4. 乐观锁校验：尝试使用同版本号 (Version=1) 覆盖写，必须报并发错误
	conflictPlan := &model.TaskList{
		SessionID: sessionID,
		Version:   1,
		Steps:     []model.Step{},
	}
	if err := repo.SaveTaskList(ctx, conflictPlan, nil); err == nil {
		t.Fatalf("expected concurrency conflict error, got nil")
	}

	// 5. 保存 Version 2 快照
	plan2 := &model.TaskList{
		SessionID: sessionID,
		Version:   2,
		Steps: []model.Step{
			{ID: "task_1", Title: "步骤 1", Status: model.StepStatusCompleted},
			{ID: "task_2", Title: "步骤 2", Status: model.StepStatusInProgress},
		},
		UpdatedAt: now.Add(time.Second),
	}
	rev2 := &model.RevisionLog{
		Version:     2,
		Explanation: "标记完成步骤1",
		Operator:    "agent",
		Timestamp:   now.Add(time.Second),
	}
	if err := repo.SaveTaskList(ctx, plan2, rev2); err != nil {
		t.Fatalf("SaveTaskList v2 failed: %v", err)
	}

	// 6. 校验 History 追加记录
	history, err := repo.GetHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history logs, got %d", len(history))
	}
}

func TestMemoryRepository_Contract(t *testing.T) {
	repo := memory.New()
	runRepositoryContractSuite(t, repo)
}

func TestFileRepository_Contract(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tasklist_contract_test_*")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	fileRepo, err := localtasklist.NewFileRepository(tempDir)
	if err != nil {
		t.Fatalf("NewFileRepository failed: %v", err)
	}
	runRepositoryContractSuite(t, fileRepo)
}
