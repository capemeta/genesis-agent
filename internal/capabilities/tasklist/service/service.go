package service

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"genesis-agent/internal/capabilities/tasklist/contract"
	"genesis-agent/internal/capabilities/tasklist/model"
	tasklistprompt "genesis-agent/internal/capabilities/tasklist/prompt"
)

// TaskListService 实现 contract.Service 接口
type TaskListService struct {
	repo                contract.Repository
	broadcaster         contract.EventBroadcaster
	remindIntervalSteps int
}

// NewTaskListService 创建 TaskListService 实例
func NewTaskListService(repo contract.Repository, broadcaster contract.EventBroadcaster, remindIntervalSteps int) *TaskListService {
	if remindIntervalSteps <= 0 {
		remindIntervalSteps = 3
	}
	return &TaskListService{
		repo:                repo,
		broadcaster:         broadcaster,
		remindIntervalSteps: remindIntervalSteps,
	}
}

// GetPlan 获取指定会话的当前计划
func (s *TaskListService) GetTaskList(ctx context.Context, sessionID string) (*model.TaskList, error) {
	plan, err := s.repo.GetTaskList(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("read plan from repository failed: %w", err)
	}
	return plan, nil
}

// levenshtein 计算两个字符串的编辑距离
func levenshtein(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)
	column := make([]int, len1+1)
	for y := 1; y <= len1; y++ {
		column[y] = y
	}
	for x := 1; x <= len2; x++ {
		column[0] = x
		lastkey := x - 1
		for y := 1; y <= len1; y++ {
			oldkey := column[y]
			incr := 0
			if r1[y-1] != r2[x-1] {
				incr = 1
			}
			column[y] = int(math.Min(math.Min(float64(column[y]+1), float64(column[y-1]+1)), float64(lastkey+incr)))
			lastkey = oldkey
		}
	}
	return column[len1]
}

// isSimilar 判定语义是否相似（相似度大于等于 80%）
func isSimilar(title1, title2 string) bool {
	t1, t2 := strings.TrimSpace(title1), strings.TrimSpace(title2)
	if t1 == t2 {
		return true
	}
	r1, r2 := []rune(t1), []rune(t2)
	maxLen := math.Max(float64(len(r1)), float64(len(r2)))
	if maxLen == 0 {
		return true
	}
	dist := levenshtein(t1, t2)
	similarity := 1.0 - (float64(dist) / maxLen)
	return similarity >= 0.8
}

// generateStableID 根据 Title 文本生成序号无关的唯一稳定 ID
func generateStableID(title string) string {
	cleaned := strings.TrimSpace(strings.ToLower(title))
	hash := md5.Sum([]byte(cleaned))
	return fmt.Sprintf("task_%x", hash[:4])
}

// MergeCollaborativePlan 人机协同智能三方合并
func MergeCollaborativePlan(oldPlan *model.TaskList, incomingSteps []model.Step, operator string) []model.Step {
	if oldPlan == nil {
		return incomingSteps
	}

	mergedMap := make(map[string]model.Step)
	for _, step := range incomingSteps {
		mergedMap[step.ID] = step
	}

	for _, oldStep := range oldPlan.Steps {
		incomingStep, exists := mergedMap[oldStep.ID]
		if !exists {
			continue
		}
		// 若 AI 提交，而用户已经标记完成，保留用户高权威标记
		if operator == "agent" && oldStep.Status == model.StepStatusCompleted && incomingStep.Status == model.StepStatusPending {
			incomingStep.Status = model.StepStatusCompleted
		}
		mergedMap[oldStep.ID] = incomingStep
	}

	result := make([]model.Step, 0, len(incomingSteps))
	for _, step := range incomingSteps {
		result = append(result, mergedMap[step.ID])
	}
	return result
}

// UpdatePlan 全量更新/重构计划大纲
func (s *TaskListService) UpdateTaskList(ctx context.Context, sessionID string, steps []model.Step, explanation string, operator string) (*model.TaskList, error) {
	if strings.TrimSpace(explanation) == "" {
		explanation = "Agent 自动规划/改写待办事项列表"
	}

	// 1. 唯一进行中校验
	inProgressCount := 0
	for _, step := range steps {
		if step.Status == model.StepStatusInProgress {
			inProgressCount++
		}
	}
	if inProgressCount > 1 {
		return nil, errors.New("仅允许同时将 1 个待办事项标记为 'in_progress'")
	}

	// 2. 读取现有 plan
	oldPlan, err := s.repo.GetTaskList(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("读取旧计划失败: %w", err)
	}

	var nextVersion int64 = 1
	if oldPlan != nil {
		nextVersion = oldPlan.Version + 1
	}

	// 3. 防重排的 ID 稳定性对齐（结合 Levenshtein 模糊对齐）
	now := time.Now()
	for i, step := range steps {
		stableID := generateStableID(step.Title)
		steps[i].ID = stableID

		if oldPlan != nil {
			matched := false
			for _, oldStep := range oldPlan.Steps {
				if oldStep.ID == stableID {
					steps[i].CreatedAt = oldStep.CreatedAt
					matched = true
					break
				}
			}
			if !matched {
				for _, oldStep := range oldPlan.Steps {
					if isSimilar(oldStep.Title, step.Title) {
						steps[i].ID = oldStep.ID
						steps[i].CreatedAt = oldStep.CreatedAt
						matched = true
						break
					}
				}
			}
		}

		if steps[i].CreatedAt.IsZero() {
			steps[i].CreatedAt = now
		}
		steps[i].UpdatedAt = now
	}

	// 3.5 校验 ParentID 合法性与循环依赖检测
	stepIDs := make(map[string]bool)
	for _, s := range steps {
		stepIDs[s.ID] = true
	}

	for _, s := range steps {
		if s.ParentID != "" {
			if s.ParentID == s.ID {
				return nil, fmt.Errorf("步骤 [%s] 不能将其自身设为父节点", s.Title)
			}
			if !stepIDs[s.ParentID] {
				return nil, fmt.Errorf("步骤 [%s] 的父节点 ID [%s] 不存在", s.Title, s.ParentID)
			}

			// 循环依赖检查（A -> B -> C -> A）
			parentTracker := make(map[string]bool)
			curr := s
			for curr.ParentID != "" {
				if parentTracker[curr.ParentID] {
					return nil, fmt.Errorf("步骤 [%s] 存在父子循环依赖引用", s.Title)
				}
				parentTracker[curr.ParentID] = true

				// 寻找父节点
				found := false
				for _, p := range steps {
					if p.ID == curr.ParentID {
						curr = p
						found = true
						break
					}
				}
				if !found {
					break
				}
			}
		}
	}

	// 4. 重大结构重构判定 (Strategic Re-planning Detection)
	hasStructureChange := false
	if oldPlan != nil && len(oldPlan.Steps) > 0 {
		if len(oldPlan.Steps) != len(steps) {
			hasStructureChange = true
		} else {
			// 检测是否有全新的未知步骤
			for _, step := range steps {
				matched := false
				for _, oldStep := range oldPlan.Steps {
					if oldStep.ID == step.ID {
						matched = true
						break
					}
				}
				if !matched {
					hasStructureChange = true
					break
				}
			}
		}
	}

	// 5. 三方合并
	finalSteps := MergeCollaborativePlan(oldPlan, steps, operator)

	// 如果判定为重大重构且来自 AI，执行阻断拦截，强迫前台拉起审批流
	if operator == "agent" && hasStructureChange {
		for i := range finalSteps {
			// 将新重构且未完成的步骤均标记为待审批挂起
			if finalSteps[i].Status != model.StepStatusCompleted {
				finalSteps[i].Status = model.StepStatusBlockedByApproval
			}
		}
		explanation = "[重大任务清单重构申请] " + explanation
	}

	// 6. 生成只写追加变更日志
	revision := &model.RevisionLog{
		Version:     nextVersion,
		Explanation: explanation,
		Operator:    operator,
		Timestamp:   now,
	}

	// 7. 存储主快照并追加审计日志
	plan := &model.TaskList{
		SessionID:         sessionID,
		Steps:             finalSteps,
		LatestExplanation: explanation,
		Version:           nextVersion,
		UpdatedAt:         now,
	}
	if err := s.repo.SaveTaskList(ctx, plan, revision); err != nil {
		return nil, fmt.Errorf("保存计划失败: %w", err)
	}

	// 8. 广播
	s.broadcastUpdate(ctx, sessionID, plan, explanation, oldPlan == nil)
	return plan, nil
}

// UpdateStepStatus 差量局部更改单个步骤状态
func (s *TaskListService) UpdateStepStatus(ctx context.Context, sessionID string, stepID string, status model.StepStatus, explanation string, operator string) (*model.TaskList, error) {
	// 强类型值域安全防线
	if status != model.StepStatusPending &&
		status != model.StepStatusInProgress &&
		status != model.StepStatusCompleted &&
		status != model.StepStatusBlockedByApproval {
		return nil, fmt.Errorf("invalid step status: %s", status)
	}

	if strings.TrimSpace(explanation) == "" {
		explanation = fmt.Sprintf("步骤 [%s] 状态更新为 %s", stepID, status)
	}

	oldPlan, err := s.repo.GetTaskList(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("获取现有计划失败: %w", err)
	}
	if oldPlan == nil {
		return nil, errors.New("计划尚未创建，无法进行局部状态更新")
	}

	// 检查 status
	if status == model.StepStatusInProgress {
		for _, step := range oldPlan.Steps {
			if step.ID != stepID && step.Status == model.StepStatusInProgress {
				return nil, errors.New("仅允许同时将 1 个待办事项标记为 'in_progress'")
			}
		}
	}

	now := time.Now()
	stepFound := false
	for i, step := range oldPlan.Steps {
		if step.ID == stepID {
			oldPlan.Steps[i].Status = status
			oldPlan.Steps[i].UpdatedAt = now
			stepFound = true
			break
		}
	}
	if !stepFound {
		return nil, fmt.Errorf("未找到 ID 为 [%s] 的步骤", stepID)
	}

	nextVersion := oldPlan.Version + 1
	revision := &model.RevisionLog{
		Version:     nextVersion,
		Explanation: explanation,
		Operator:    operator,
		Timestamp:   now,
	}

	plan := &model.TaskList{
		SessionID:         sessionID,
		Steps:             oldPlan.Steps,
		LatestExplanation: explanation,
		Version:           nextVersion,
		UpdatedAt:         now,
	}
	if err := s.repo.SaveTaskList(ctx, plan, revision); err != nil {
		return nil, fmt.Errorf("保存局部更改计划失败: %w", err)
	}

	s.broadcastUpdate(ctx, sessionID, plan, explanation, false)
	return plan, nil
}

// GeneratePromptReminder 生成供 runtime 注入的未完成步骤提醒
func (s *TaskListService) GeneratePromptReminder(ctx context.Context, sessionID string, currentStep int) (string, bool, error) {
	plan, err := s.repo.GetTaskList(ctx, sessionID)
	if err != nil {
		return "", false, fmt.Errorf("获取计划失败: %w", err)
	}

	// 无清单时不每轮催促：琐事依赖 system 纪律自觉跳过；多步建立清单由 task_management 约束。
	if plan == nil || len(plan.Steps) == 0 {
		return "", false, nil
	}

	var activeSteps []model.Step
	for _, step := range plan.Steps {
		if step.Status != model.StepStatusCompleted {
			activeSteps = append(activeSteps, step)
		}
	}

	if len(activeSteps) == 0 {
		return "", false, nil
	}

	if currentStep > 0 && currentStep%s.remindIntervalSteps == 0 {
		var sb strings.Builder
		sb.WriteString("当前任务清单进度跟进（未完成项）：\n")
		for i, step := range activeSteps {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("... 还有 %d 项未完成\n", len(activeSteps)-5))
				break
			}

			statusLabel := string(step.Status)
			if step.Status == model.StepStatusInProgress {
				statusLabel = "★ IN_PROGRESS"
				if step.Notes != "" {
					sb.WriteString(fmt.Sprintf("%d. [%s] %s (备注: %s)\n", i+1, statusLabel, step.Title, step.Notes))
					continue
				}
			}
			sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, statusLabel, step.Title))
		}
		sb.WriteString("\n若当前步骤已完成，请务必使用 `todo_update_step` 将其标记为 completed 并启动下一步。避免在同一子任务内陷入死循环。")
		return tasklistprompt.WrapSystemReminder(sb.String()), true, nil
	}

	return "", false, nil
}

func (s *TaskListService) broadcastUpdate(ctx context.Context, sessionID string, plan *model.TaskList, explanation string, isNew bool) {
	eventType := contract.EventTaskListUpdated
	if isNew {
		eventType = contract.EventTaskListCreated
	}
	if s.broadcaster != nil {
		_ = s.broadcaster.Broadcast(ctx, contract.TaskListEvent{
			Type:        eventType,
			SessionID:   sessionID,
			Plan:        plan,
			Explanation: explanation,
			Timestamp:   time.Now(),
		})
	}
}
