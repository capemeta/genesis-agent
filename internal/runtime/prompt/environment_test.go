package prompt

import (
	"strings"
	"testing"
)

func TestRenderEnvironmentContextEscapesAndDeduplicates(t *testing.T) {
	got := renderEnvironmentContext(EnvironmentContext{
		OS:               "windows",
		Cwd:              `D:\work&space`,
		DefaultShell:     "powershell",
		DefaultShellPath: `C:\Program Files\PowerShell\pwsh.exe`,
		SupportedShells:  []string{"powershell", "PowerShell", "cmd"},
		SandboxMode:      "disabled",
		ExternalApproval: true,
	})
	checks := []string{
		`<cwd>D:\work&amp;space</cwd>`,
		`<default_shell name="powershell" path="C:\Program Files\PowerShell\pwsh.exe" />`,
		`<supported_shells>powershell,cmd</supported_shells>`,
		`<filesystem external_access_requires_approval="true" />`,
	}
	for _, check := range checks {
		if !strings.Contains(got, check) {
			t.Fatalf("renderEnvironmentContext() = %q, missing %q", got, check)
		}
	}
}

func TestRenderEnvironmentContextDoesNotGuessUnknownRemoteShell(t *testing.T) {
	got := renderEnvironmentContext(EnvironmentContext{Cwd: "/workspace", SandboxProvider: "genesis-sandbox"})
	if strings.Contains(got, "default_shell") || strings.Contains(got, "supported_shells") {
		t.Fatalf("renderEnvironmentContext() = %q", got)
	}
}
