package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/plan/model"
)

// MockRepository 简易内存 Repository
type MockRepository struct {
	plan    *model.Plan
	history []model.RevisionLog
}

func (m *MockRepository) GetPlan(ctx context.Context, sessionID string) (*model.Plan, error) {
	return m.plan, nil
}

func (m *MockRepository) SavePlan(ctx context.Context, plan *model.Plan, revision *model.RevisionLog) error {
	m.plan = plan
	m.history = append(m.history, *revision)
	return nil
}

func (m *MockRepository) GetHistory(ctx context.Context, sessionID string) ([]model.RevisionLog, error) {
	return m.history, nil
}

// MockBroadcaster 简易广播器
type MockBroadcaster struct {
	events []interface{}
}

func (m *MockBroadcaster) Broadcast(ctx context.Context, event interface{}) error {
	m.events = append(m.events, event)
	return nil
}

func TestPlanService_SingleInProgressConstraint(t *testing.T) {
	repo := &MockRepository{}
	svc := NewPlanService(repo, nil, 3)

	steps := []model.Step{
		{Title: "Step 1", Status: model.StepStatusInProgress},
		{Title: "Step 2", Status: model.StepStatusInProgress},
	}

	_, err := svc.UpdatePlan(context.Background(), "session_1", steps, "Test reason", "agent")
	if err == nil {
		t.Fatal("Expected error for multiple in_progress steps, got nil")
	}
	if !strings.Contains(err.Error(), "仅允许同时将 1 个待办事项标记为 'in_progress'") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestPlanService_LevenshteinAndIDAlignment(t *testing.T) {
	repo := &MockRepository{}
	svc := NewPlanService(repo, nil, 3)

	steps1 := []model.Step{
		{Title: "初始化代码仓库"},
	}

	plan1, err := svc.UpdatePlan(context.Background(), "session_1", steps1, "Init plan", "agent")
	if err != nil {
		t.Fatalf("Init update failed: %v", plan1)
	}
	initialID := plan1.Steps[0].ID

	// 模拟 AI 进行重构，微改文字（相似度 > 80%）
	steps2 := []model.Step{
		{Title: "初始化代码库"},
	}

	plan2, err := svc.UpdatePlan(context.Background(), "session_1", steps2, "Tweak title", "agent")
	if err != nil {
		t.Fatalf("Tweak update failed: %v", err)
	}

	if plan2.Steps[0].ID != initialID {
		t.Errorf("Expected step ID to align and remain %s, but got %s", initialID, plan2.Steps[0].ID)
	}
}

func TestPlanService_MergeCollaborativePlan(t *testing.T) {
	repo := &MockRepository{}
	svc := NewPlanService(repo, nil, 3)

	// 1. 建立初始计划
	steps := []model.Step{
		{Title: "打补丁", Status: model.StepStatusPending},
	}
	plan1, _ := svc.UpdatePlan(context.Background(), "session_1", steps, "Initial", "agent")

	// 2. 模拟用户在前台手动标记为 Completed
	plan1.Steps[0].Status = model.StepStatusCompleted
	repo.plan = plan1

	// 3. 模拟 AI 拿着旧的 Pending 计划发起全量更新
	stepsAI := []model.Step{
		{Title: "打补丁", Status: model.StepStatusPending},
	}

	plan2, err := svc.UpdatePlan(context.Background(), "session_1", stepsAI, "AI update", "agent")
	if err != nil {
		t.Fatalf("AI Update failed: %v", err)
	}

	if plan2.Steps[0].Status != model.StepStatusCompleted {
		t.Errorf("Expected step status to merge and remain Completed, got %s", plan2.Steps[0].Status)
	}
}

func TestPlanService_StructuralRePlanningInterception(t *testing.T) {
	repo := &MockRepository{}
	svc := NewPlanService(repo, nil, 3)

	// 1. 初始化计划（2个步骤）
	steps1 := []model.Step{
		{Title: "Step A", Status: model.StepStatusPending},
		{Title: "Step B", Status: model.StepStatusPending},
	}
	_, _ = svc.UpdatePlan(context.Background(), "session_1", steps1, "Init", "agent")

	// 2. 模拟 AI 全量覆写，插入第 3 个新步骤 (Strategic Re-planning)
	steps2 := []model.Step{
		{Title: "Step A", Status: model.StepStatusPending},
		{Title: "Step B", Status: model.StepStatusPending},
		{Title: "Step C", Status: model.StepStatusPending},
	}

	plan2, err := svc.UpdatePlan(context.Background(), "session_1", steps2, "AI replanning", "agent")
	if err != nil {
		t.Fatalf("Re-planning failed: %v", err)
	}

	// 检查未完成项是否全部被自动标记为待审批拦截状态
	for _, step := range plan2.Steps {
		if step.Status != model.StepStatusBlockedByApproval {
			t.Errorf("Expected step [%s] status to be blocked_by_approval, got %s", step.Title, step.Status)
		}
	}
	if !strings.HasPrefix(plan2.LatestExplanation, "[重大计划重构申请]") {
		t.Errorf("Unexpected explanation prefix: %s", plan2.LatestExplanation)
	}
}

func TestPlanService_GeneratePromptReminder(t *testing.T) {
	repo := &MockRepository{}
	// remindIntervalSteps = 2
	svc := NewPlanService(repo, nil, 2)

	// 1. 无计划情况
	reminder1, needed1, _ := svc.GeneratePromptReminder(context.Background(), "session_1", 0)
	if !needed1 {
		t.Error("Expected needed to be true when no plan exists")
	}
	if !strings.Contains(reminder1, "当前任务尚未规划待办事项列表") {
		t.Errorf("Unexpected reminder text: %s", reminder1)
	}

	// 2. 有计划，执行步数未到阈值
	steps := []model.Step{
		{Title: "Step 1", Status: model.StepStatusInProgress},
		{Title: "Step 2", Status: model.StepStatusPending},
	}
	_, _ = svc.UpdatePlan(context.Background(), "session_1", steps, "Init", "user") // user 绕过审批阻断

	_, needed2, _ := svc.GeneratePromptReminder(context.Background(), "session_1", 1)
	if needed2 {
		t.Error("Expected needed to be false at step 1")
	}

	// 3. 有计划，步数到达阈值（2）
	reminder3, needed3, _ := svc.GeneratePromptReminder(context.Background(), "session_1", 2)
	if !needed3 {
		t.Error("Expected needed to be true at step 2")
	}
	if !strings.Contains(reminder3, "当前任务进度跟进") {
		t.Errorf("Unexpected reminder text: %s", reminder3)
	}
}

func TestPlanService_ParentIDValidationAndCycles(t *testing.T) {
	repo := &MockRepository{}
	svc := NewPlanService(repo, nil, 3)

	// 1. 测试自我循环依赖引用
	stableIDA := generateStableID("Step A")
	selfRefSteps := []model.Step{
		{Title: "Step A", ParentID: stableIDA},
	}
	_, err := svc.UpdatePlan(context.Background(), "session_1", selfRefSteps, "Self ref", "user")
	if err == nil || !strings.Contains(err.Error(), "不能将其自身设为父节点") {
		t.Errorf("Expected self-reference check error, got: %v", err)
	}

	// 2. 测试悬空 Parent
	danglingSteps := []model.Step{
		{Title: "Step A", ParentID: "non_existent"},
	}
	_, err = svc.UpdatePlan(context.Background(), "session_1", danglingSteps, "Dangling", "user")
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("Expected dangling parent check error, got: %v", err)
	}

	// 3. 测试循环链依赖 (A -> B -> C -> A)
	stableIDB := generateStableID("Step B")
	stableIDC := generateStableID("Step C")

	cycleSteps := []model.Step{
		{Title: "Step A", ParentID: stableIDB},
		{Title: "Step B", ParentID: stableIDC},
		{Title: "Step C", ParentID: stableIDA},
	}
	_, err = svc.UpdatePlan(context.Background(), "session_1", cycleSteps, "Cycle chain", "user")
	if err == nil || !strings.Contains(err.Error(), "存在父子循环依赖引用") {
		t.Errorf("Expected circular dependency error, got: %v", err)
	}
}

func TestPlanService_DefensiveFallbacksAndValidations(t *testing.T) {
	repo := &MockRepository{}
	svc := NewPlanService(repo, nil, 3)

	// 1. 空 explanation 覆写测试
	steps := []model.Step{
		{Title: "Step 1", Status: model.StepStatusPending},
	}
	plan, err := svc.UpdatePlan(context.Background(), "session_1", steps, "", "agent")
	if err != nil {
		t.Fatalf("UpdatePlan failed: %v", err)
	}
	if plan.LatestExplanation != "Agent 自动规划/改写待办事项列表" {
		t.Errorf("Expected explanation fallback, got: %s", plan.LatestExplanation)
	}

	// 2. 非法 status 拦截测试
	_, err = svc.UpdateStepStatus(context.Background(), "session_1", plan.Steps[0].ID, model.StepStatus("invalid_doing"), "", "agent")
	if err == nil || !strings.Contains(err.Error(), "invalid step status") {
		t.Errorf("Expected invalid step status error, got: %v", err)
	}

	// 3. 正常局部更新的空 explanation 拦截测试
	plan2, err := svc.UpdateStepStatus(context.Background(), "session_1", plan.Steps[0].ID, model.StepStatusCompleted, "", "agent")
	if err != nil {
		t.Fatalf("UpdateStepStatus failed: %v", err)
	}

	history, _ := repo.GetHistory(context.Background(), "session_1")
	if len(history) < 2 {
		t.Fatalf("Expected history logs, got len %d", len(history))
	}
	latestRev := history[len(history)-1]
	expectedPrefix := fmt.Sprintf("步骤 [%s] 状态更新为", plan2.Steps[0].ID)
	if !strings.HasPrefix(latestRev.Explanation, expectedPrefix) {
		t.Errorf("Expected history explanation prefix %s, got: %s", expectedPrefix, latestRev.Explanation)
	}
}
