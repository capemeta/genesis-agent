package transport

import (
	"context"
	"os"
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/mcp/model"
)

func TestSelectedEnvDoesNotInheritSecrets(t *testing.T) {
	t.Setenv("GENESIS_TEST_ALLOWED", "allowed")
	t.Setenv("GENESIS_TEST_SECRET", "secret")
	env := selectedEnv([]string{"GENESIS_TEST_ALLOWED"}, map[string]string{"EXPLICIT": "value"})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GENESIS_TEST_ALLOWED=allowed") || !strings.Contains(joined, "EXPLICIT=value") {
		t.Fatalf("env = %v", env)
	}
	if strings.Contains(joined, "GENESIS_TEST_SECRET") {
		t.Fatalf("secret inherited: %v", env)
	}
}

func TestFactoryRejectsSandboxPlacementWithoutExecutionRunner(t *testing.T) {
	_, err := NewFactory(nil, nil).Build(context.Background(), model.McpServerConfig{Name: "sandboxed", Type: model.McpTransportStdio, Command: os.Args[0], Placement: model.McpPlacementLocalSandboxStdio})
	if err == nil {
		t.Fatal("Build() error = nil")
	}
}
