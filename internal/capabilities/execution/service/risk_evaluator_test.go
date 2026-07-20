package service

import (
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
)

func TestCommandRiskEvaluator(t *testing.T) {
	evaluator := NewCommandRiskEvaluator()

	tests := []struct {
		name         string
		command      string
		taskElevated bool
		expected     RiskLevel
	}{
		{
			name:         "local safe go test",
			command:      "go test ./...",
			taskElevated: false,
			expected:     RiskLevelLocalSafe,
		},
		{
			name:         "grep keyword should not trigger false positive",
			command:      "grep \"pip install\" README.md",
			taskElevated: false,
			expected:     RiskLevelLocalSafe,
		},
		{
			name:         "untrusted pip install",
			command:      "pip install requests",
			taskElevated: false,
			expected:     RiskLevelUntrustedRemote,
		},
		{
			name:         "ordinary ls escalated due to active subtask remote affinity",
			command:      "ls -la",
			taskElevated: true,
			expected:     RiskLevelUntrustedRemote,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := execmodel.Command{Command: tt.command}
			got := evaluator.EvaluateRisk(cmd, tt.taskElevated)
			if got != tt.expected {
				t.Errorf("EvaluateRisk(%q, %v) = %v, want %v", tt.command, tt.taskElevated, got, tt.expected)
			}
		})
	}
}
