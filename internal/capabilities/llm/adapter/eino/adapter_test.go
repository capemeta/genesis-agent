package eino

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestIsIncompleteFinishReason(t *testing.T) {
	for _, reason := range []string{"length", "MAX_TOKENS", "max_output_tokens"} {
		if !isIncompleteFinishReason(reason) {
			t.Fatalf("reason %q should be incomplete", reason)
		}
	}
	for _, reason := range []string{"", "stop", "tool_calls"} {
		if isIncompleteFinishReason(reason) {
			t.Fatalf("reason %q should be complete", reason)
		}
	}
}

func TestFinishReason(t *testing.T) {
	message := &schema.Message{ResponseMeta: &schema.ResponseMeta{FinishReason: " length "}}
	if got := finishReason(message); got != "length" {
		t.Fatalf("finishReason = %q", got)
	}
}

func TestRawLLMDebugDisabledByDefault(t *testing.T) {
	t.Setenv("GENESIS_LLM_RAW_DEBUG", "")
	if rawLLMDebugEnabled() {
		t.Fatal("raw debug must be opt-in")
	}
	t.Setenv("GENESIS_LLM_RAW_DEBUG", "true")
	if !rawLLMDebugEnabled() {
		t.Fatal("raw debug should accept explicit opt-in")
	}
}
