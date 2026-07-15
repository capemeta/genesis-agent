package context

import (
	"context"
	"testing"
)

func TestContextBudgetPlanner_Plan(t *testing.T) {
	planner := NewContextBudgetPlanner(nil)
	ctx := context.Background()

	t.Run("Default Profile normal allocation", func(t *testing.T) {
		opt := PlanOptions{
			ContextWindow:         32768, // 32k window
			EffectiveContextRatio: 0.90,  // 90%
			MaxTokens:             4096,
			Strategy:              "default",
			StableSystemTokens:    2000,
			ToolsSchemaTokens:     3000,
			CurrentUserTokens:     1000,
			ActualSummaryTokens:   500,
			ActualLTMTokens:       400,
		}

		budget, inputBudget, err := planner.Plan(ctx, opt)
		if err != nil {
			t.Fatalf("Plan failed: %v", err)
		}

		// 可用：usable = 32768 * 0.9 = 29491
		// 输入预算: inputBudget = 29491 - 4096 = 25395
		// 刚性占：rigid = 2000 + 3000 + 1000 = 6000
		// 剩余：remaining = 25395 - 6000 = 19395
		// 安全缓冲：buffer = 19395 * 0.05 = 969
		// 可分配：distributable = 19395 - 969 = 18426
		if inputBudget != 25395 {
			t.Errorf("expected inputBudget 25395, got %d", inputBudget)
		}

		// 验证弹性分配
		// LTM 实际仅有 400，Summary 仅有 500；它们应该饱和分配为 400 和 500
		// 结余应该完全回流给 history
		if budget.LTM != 400 {
			t.Errorf("expected LTM budget 400 (actual size), got %d", budget.LTM)
		}
		if budget.Summary != 500 {
			t.Errorf("expected Summary budget 500 (actual size), got %d", budget.Summary)
		}
		// 剩余的全部补偿给 history，但受到 90% 的硬性上限限制 (0.90 * 18426 = 16583)
		expectedHistory := 16583
		if budget.History != expectedHistory {
			t.Errorf("expected History budget %d, got %d", expectedHistory, budget.History)
		}
	})

	t.Run("Rigid input exceeds window error", func(t *testing.T) {
		opt := PlanOptions{
			ContextWindow:         8192, // 很小的窗口
			EffectiveContextRatio: 0.90,
			MaxTokens:             4096,
			Strategy:              "default",
			StableSystemTokens:    4000, // 刚性超限
			ToolsSchemaTokens:     3000,
			CurrentUserTokens:     1000,
		}

		_, _, err := planner.Plan(ctx, opt)
		if err == nil {
			t.Fatal("expected error when rigid input exceeds budget, got nil")
		}
	})
}
