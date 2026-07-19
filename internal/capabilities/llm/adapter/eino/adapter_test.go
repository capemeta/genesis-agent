package eino

import (
	"testing"

	"github.com/cloudwego/eino/schema"

	tool "genesis-agent/internal/capabilities/tool/contract"
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

func TestToolInfosToSchemaNilOrEmpty(t *testing.T) {
	if got := toolInfosToSchema(nil); len(got) != 0 {
		t.Fatalf("nil tools => empty schema, got %d", len(got))
	}
	if got := toolInfosToSchema([]*tool.Info{}); len(got) != 0 {
		t.Fatalf("empty tools => empty schema, got %d", len(got))
	}
}

