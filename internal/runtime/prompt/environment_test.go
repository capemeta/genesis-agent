package prompt

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultPromptUsesReadFileForKnownExactPath(t *testing.T) {
	prompt, err := New().BuildSystem(context.Background(), BuildRequest{AvailableTools: []string{
		"read_file", "glob", "list_dir", "walk_dir", "grep", "run_command",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "裸文件名") || !strings.Contains(prompt, "直接 read_file") || !strings.Contains(prompt, "禁止擅自改写为通配路径") {
		t.Fatalf("缺少精确路径工具选择约束: %s", prompt)
	}
}

func TestDefaultPromptDoesNotReferenceUnavailableTools(t *testing.T) {
	prompt, err := New().BuildSystem(context.Background(), BuildRequest{AvailableTools: []string{"current_time"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, unavailable := range []string{"read_file", "glob", "list_dir", "walk_dir", "grep", "run_command"} {
		if strings.Contains(prompt, unavailable) {
			t.Fatalf("提示词引用了不可用工具 %q: %s", unavailable, prompt)
		}
	}
}

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
