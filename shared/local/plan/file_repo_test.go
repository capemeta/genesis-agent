package plan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"genesis-agent/internal/capabilities/plan/model"
)

func TestFileRepository_SaveAndGetPlan(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "genesis_plan_test")
	if err != nil {
		t.Fatalf("Create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repo, err := NewFileRepository(tempDir)
	if err != nil {
		t.Fatalf("Init repo failed: %v", err)
	}

	sessionID := "test_session_abc"

	steps := []model.Step{
		{ID: "task_1", Title: "Initialize project", Status: model.StepStatusPending},
	}

	plan := &model.Plan{
		SessionID:         sessionID,
		Steps:             steps,
		LatestExplanation: "Test save",
		Version:           1,
		UpdatedAt:         time.Now(),
	}

	revision := &model.RevisionLog{
		Version:     1,
		Explanation: "Test save",
		Operator:    "agent",
		Timestamp:   time.Now(),
	}

	// 1. 保存 Plan
	err = repo.SavePlan(context.Background(), plan, revision)
	if err != nil {
		t.Fatalf("Save plan failed: %v", err)
	}

	// 2. 读取 Plan
	retrieved, err := repo.GetPlan(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("Get plan failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("Expected plan to exist, got nil")
	}

	if retrieved.SessionID != sessionID || len(retrieved.Steps) != 1 || retrieved.Version != 1 {
		t.Errorf("Retrieved plan properties mismatch: %+v", retrieved)
	}

	// 3. 并发乐观锁冲突测试
	// 尝试以相同或更小版本号 (<=1) 写入
	err = repo.SavePlan(context.Background(), plan, revision)
	if err == nil {
		t.Fatal("Expected error due to concurrent lock version conflict, got nil")
	}

	// 4. 正确更新版本
	plan.Version = 2
	revision.Version = 2
	err = repo.SavePlan(context.Background(), plan, revision)
	if err != nil {
		t.Fatalf("Save plan with next version failed: %v", err)
	}

	// 5. 校验变更历史记录是否成功追加
	history, err := repo.GetHistory(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("Get history failed: %v", err)
	}

	if len(history) != 2 {
		t.Errorf("Expected 2 history revisions, got %d", len(history))
	}
	if history[0].Version != 1 || history[1].Version != 2 {
		t.Errorf("History versions incorrect: %+v", history)
	}
}

func TestFileRepository_CleanPathSafety(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "genesis_plan_test_safety")
	if err != nil {
		t.Fatalf("Create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repo, err := NewFileRepository(tempDir)
	if err != nil {
		t.Fatalf("Init repo failed: %v", err)
	}

	// 模拟黑客注入相对路径进行路径穿越
	hackSessionID := "../../../hack_session"
	safePath := repo.getPlanPath(hackSessionID)

	// 确认 filepath.Clean 处理了穿越字符
	if filepath.Base(safePath) != "hack_session_plan.json" {
		t.Errorf("Expected filename to be cleaned to hack_session_plan.json, got path: %s", safePath)
	}
}
