package context

import (
	"context"
	"testing"

	"genesis-agent/internal/domain"
)

func TestHeuristicEstimator_Estimate(t *testing.T) {
	estimator := NewHeuristicEstimator()
	ctx := context.Background()

	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"Empty text", "", 0},
		{"English text only", "Hello world this is test", 6}, // 5 words * 1.3 = 6.5 -> 6
		{"Chinese text only", "你好世界", 6},                  // 4 chars * 1.5 = 6
		{"Mixed text", "Hello你好世界", 7},                     // 1 word (1.3) + 4 chars (6) = 7.3 -> 7
		{"Mixed with spacing", "Hello 你好 世界", 7},            // 2 words (2.6) + 4 chars (6) = 8.6 -> 7 (or 8 depending on rounding/spacing)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimator.Estimate(ctx, tt.text, "test-model")
			// 偏差在合理范围即可
			if got < tt.expected-1 || got > tt.expected+1 {
				t.Errorf("Estimate() = %v, expected near %v", got, tt.expected)
			}
		})
	}
}

func TestHeuristicEstimator_EstimateMessages(t *testing.T) {
	estimator := NewHeuristicEstimator()
	ctx := context.Background()

	msgs := []*domain.Message{
		domain.NewSystemMessage("You are agent"), // 3 words * 1.3 = 3.9 -> 3 tokens
		domain.NewUserMessage("你好"),            // 2 chars * 1.5 = 3 tokens
	}

	// 预计 Token: (3 + 4 overhead) + (3 + 4 overhead) + 3 final assistant overhead = 17
	got := estimator.EstimateMessages(ctx, msgs, "test-model")
	if got < 15 || got > 20 {
		t.Errorf("EstimateMessages() = %v, expected in range [15, 20]", got)
	}
}
