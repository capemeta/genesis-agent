package prompt

import (
	"context"
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
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
	got := renderEnvironmentContext(context.Background(), EnvironmentContext{
		OS:               "windows",
		DefaultShell:     "powershell",
		DefaultShellPath: `C:\Program Files\PowerShell\pwsh.exe`,
		SupportedShells:  []string{"powershell", "PowerShell", "cmd"},
		SandboxMode:      "disabled",
		ExternalApproval: true,
	})
	checks := []string{
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
	got := renderEnvironmentContext(context.Background(), EnvironmentContext{SandboxProvider: "genesis-sandbox"})
	if strings.Contains(got, "default_shell") || strings.Contains(got, "supported_shells") {
		t.Fatalf("renderEnvironmentContext() = %q", got)
	}
}

func TestRenderEnvironmentContextOnlyDescribesInstalledEnvironmentTools(t *testing.T) {
	mixed := renderEnvironmentContext(context.Background(), EnvironmentContext{
		OS: "windows", HostCommandTool: "run_command", SandboxMode: "required",
		SandboxProvider: "genesis-sandbox", SandboxCommandTool: "sandbox_exec",
	})
	for _, want := range []string{`tool="run_command"`, `tool="sandbox_exec"`, "两个环境的文件不自动共享"} {
		if !strings.Contains(mixed, want) {
			t.Fatalf("mixed context=%q missing %q", mixed, want)
		}
	}
	sandboxOnly := renderEnvironmentContext(context.Background(), EnvironmentContext{SandboxMode: "required", SandboxProvider: "genesis-sandbox"})
	if strings.Contains(sandboxOnly, "run_command 始终") || strings.Contains(sandboxOnly, `tool="sandbox_exec"`) {
		t.Fatalf("context must not advertise unavailable tools: %q", sandboxOnly)
	}
}

func TestRenderEnvironmentContextUsesLogicalRunWorkspace(t *testing.T) {
	binding := execmodel.ExecutionBinding{ID: "binding", Mode: execmodel.WorkspaceModeTask, Access: execmodel.WorkspaceAccessReadWrite, Owner: execmodel.ExecutionOwnerRef{RunID: "run"}}
	view := workmodel.WorkspaceViewManifest{BindingID: "binding", Root: ".", Entries: []workmodel.WorkspaceViewEntry{{Path: "source.pptx", Access: workmodel.WorkspaceViewAccessReadWrite}}}
	ctx := workcontract.WithPreparedRun(context.Background(), workmodel.PreparedRun{Manifest: workmodel.RunManifest{View: view}, Execution: workmodel.PreparedExecutionSnapshot{Binding: binding, Workspace: execmodel.ExecutionWorkspace{WorkDir: `D:\secret\run`}}})
	got := renderEnvironmentContext(ctx, EnvironmentContext{OS: "windows"})
	for _, want := range []string{`mode="task_job"`, `root="."`, `project_changes_persist="false"`, `path="source.pptx"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("workspace context=%q missing %q", got, want)
		}
	}
	if strings.Contains(got, `D:\secret`) {
		t.Fatalf("workspace context 泄漏物理路径: %s", got)
	}
}
