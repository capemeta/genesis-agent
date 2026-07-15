package builtin

import (
	"context"
	"testing"
)

func TestDefaultGuardsBlockDangerousInputs(t *testing.T) {
	r := NewDefaultRegistry()
	git := r.handlers["git_branch_guard"](context.Background(), []byte(`{"tool_name":"run_command","tool_input":{"command":"git switch main"}}`))
	if git.Continue {
		t.Fatalf("git branch switch should be blocked: %#v", git)
	}
	secret := r.handlers["secret_path_guard"](context.Background(), []byte(`{"tool_name":"read_file","tool_input":{"path":".env"}}`))
	if secret.Continue {
		t.Fatalf("secret path should be blocked: %#v", secret)
	}
}
