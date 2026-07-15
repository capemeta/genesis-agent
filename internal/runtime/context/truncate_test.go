package context

import (
	"context"
	"strings"
	"testing"

	"genesis-agent/internal/domain"
)

// mockEstimator 简单的 mock 估算器，便于测试确切截断的字符/token 数
type mockEstimator struct{}

func (m *mockEstimator) Estimate(_ context.Context, text string, _ string) int {
	// 直接返回 rune 长度作为 token 估算，简单直观
	return len([]rune(text))
}

func TestTruncate_TailOnly(t *testing.T) {
	estimator := &mockEstimator{}
	ctx := context.Background()

	msg := domain.NewUserMessage("abcdefghij") // 10 runes
	cfg := TruncateConfig{Strategy: TruncateTailOnly}

	// 限制为 5 个 token (即 5 个字符)
	truncated := Truncate(ctx, msg, 5, "model", estimator, cfg)

	// 截尾保留尾部，格式为：[... 已就地截去头部 5 字符 ...]\nfghij
	expectedTail := "fghij"
	if !strings.HasSuffix(truncated.Content, expectedTail) {
		t.Errorf("expected suffix %q, got %q", expectedTail, truncated.Content)
	}
}

func TestTruncate_HeadTail(t *testing.T) {
	estimator := &mockEstimator{}
	ctx := context.Background()

	msg := domain.NewUserMessage("12345abcde67890") // 15 runes
	cfg := TruncateConfig{Strategy: TruncateHeadTail, HeadRatio: 0.4}

	// 预算 10 token (headRatio=0.4 表示 4个，尾部 6个)
	truncated := Truncate(ctx, msg, 10, "model", estimator, cfg)

	// 截中留两端：1234 ... 67890
	if !strings.HasPrefix(truncated.Content, "1234") {
		t.Errorf("expected prefix 1234, got %q", truncated.Content)
	}
	if !strings.HasSuffix(truncated.Content, "67890") {
		t.Errorf("expected suffix 67890, got %q", truncated.Content)
	}
}

func TestTruncate_SectionAware(t *testing.T) {
	estimator := &mockEstimator{}
	ctx := context.Background()

	text := "## Intro\nThis is a long introduction section for the agent run context.\n" +
		"## Examples\nHere are some complex few-shot examples showing how tools interact.\n" +
		"## Instructions\nAlways execute instructions step by step and register fonts."
	msg := domain.NewUserMessage(text)

	// 给段落配置优先级：## Examples 为 10（最先被删），## Instructions 为 90（最不容易删）
	priorities := map[string]int{
		"Examples":     10,
		"Instructions": 90,
	}

	cfg := TruncateConfig{
		Strategy:        TruncateSectionAware,
		SectionPriority: priorities,
	}

	// 文本总共约 200，我们要压缩到 120 以下
	truncated := Truncate(ctx, msg, 120, "model", estimator, cfg)

	// Examples段落优先级最低，应该被优先折叠成占位符
	if !strings.Contains(truncated.Content, "段落截断: ## Examples") {
		t.Errorf("expected Examples to be truncated, got %q", truncated.Content)
	}
	// Instructions段落优先级最高，应该被完整保留
	if !strings.Contains(truncated.Content, "## Instructions\nAlways execute instructions step by step") {
		t.Errorf("expected Instructions to be preserved, got %q", truncated.Content)
	}
}

func TestTruncate_DefaultStrategy(t *testing.T) {
	estimator := &mockEstimator{}
	ctx := context.Background()

	// 1. 测试 tool_result，超限时应自动路由至 TruncateTailOnly
	toolMsg := domain.NewToolResultMessage("call-1", "log-line-1\nlog-line-2\nlog-line-3\nerror_stack_trace_here") // 48 runes
	emptyCfg := TruncateConfig{}

	truncatedTool := Truncate(ctx, toolMsg, 22, "model", estimator, emptyCfg)
	if !strings.HasSuffix(truncatedTool.Content, "error_stack_trace_here") {
		t.Errorf("expected tool_result default to be tail-only preserving stack trace, got %q", truncatedTool.Content)
	}

	// 2. 测试 user_turn，超限时应自动路由至 TruncateHeadTail
	userMsg := domain.NewUserMessage("12345abcdef67890") // 16 runes
	truncatedUser := Truncate(ctx, userMsg, 10, "model", estimator, emptyCfg)
	if !strings.HasPrefix(truncatedUser.Content, "12345") || !strings.HasSuffix(truncatedUser.Content, "67890") {
		t.Errorf("expected user_turn default to be head-tail preserving prefix/suffix, got %q", truncatedUser.Content)
	}
}

