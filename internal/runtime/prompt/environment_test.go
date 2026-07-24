package prompt

import (
	"context"
	"strings"
	"testing"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	"genesis-agent/internal/domain"
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

func TestDefaultPromptTeachesEmptyMatchesSemantics(t *testing.T) {
	prompt, err := New().BuildSystem(context.Background(), BuildRequest{AvailableTools: []string{
		"glob", "grep", "list_dir", "run_command",
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"matches=[]",
		"禁止用 run_command 做",
		"显式定义业务退出码",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in system prompt:\n%s", want, prompt)
		}
	}
}

func TestDefaultPromptRunCommandRuleOnlyNamesAvailableFileTools(t *testing.T) {
	prompt, err := New().BuildSystem(context.Background(), BuildRequest{AvailableTools: []string{
		"list_dir", "run_command",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "ls/dir/Get-ChildItem 路径枚举") || !strings.Contains(prompt, "应改用 list_dir") {
		t.Fatalf("expected list_dir-only steer:\n%s", prompt)
	}
	if strings.Contains(prompt, "grep/rg") || strings.Contains(prompt, "应改用 list_dir/grep") {
		t.Fatalf("must not mention unavailable grep:\n%s", prompt)
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

func TestBuildSystemInjectsTaskManagementBlock(t *testing.T) {
	withTodo, err := New().BuildSystem(context.Background(), BuildRequest{AvailableTools: []string{
		"todo_write", "todo_update_step", "todo_read",
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<task_management>", "</task_management>", "任务清单纪律", "硬规则", "todo_update_step"} {
		if !strings.Contains(withTodo, want) {
			t.Fatalf("missing %q in system prompt:\n%s", want, withTodo)
		}
	}
	if strings.Contains(withTodo, "## 工具分工") {
		t.Fatalf("system 不应再堆工具分工（应在 Description）: %s", withTodo)
	}
	withoutTodo, err := New().BuildSystem(context.Background(), BuildRequest{AvailableTools: []string{"current_time"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutTodo, "<task_management>") || strings.Contains(withoutTodo, "任务清单纪律") {
		t.Fatalf("task_management must not inject without todo tools: %s", withoutTodo)
	}
}

func TestBuildSystemPlanModeExcludesTaskManagement(t *testing.T) {
	got, err := New().BuildSystem(context.Background(), BuildRequest{
		AvailableTools:    []string{"todo_write", "todo_update_step", "write_implementation_plan", "exit_plan_mode"},
		CollaborationMode: "plan_mode",
		PlanDocumentPath:  ".genesis/plans/test-sess.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<plan_mode_rules>") || !strings.Contains(got, "规划模式") {
		t.Fatalf("missing plan_mode_rules: %s", got)
	}
	if !strings.Contains(got, ".genesis/plans/test-sess.md") {
		t.Fatalf("plan_mode_rules missing plan path: %s", got)
	}
	if strings.Contains(got, "<task_management>") {
		t.Fatalf("plan mode must not inject task_management: %s", got)
	}
}

func TestBuildSystemInjectsDelegationBlock(t *testing.T) {
	withTask, err := New().BuildSystem(context.Background(), BuildRequest{
		AvailableTools:    []string{"Task", "read_file", "grep"},
		DelegationPosture: "proactive",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<delegation>", "</delegation>", "子智能体委派纪律", "Task(subagent_type=explore)"} {
		if !strings.Contains(withTask, want) {
			t.Fatalf("missing %q in system prompt:\n%s", want, withTask)
		}
	}
	if !strings.Contains(withTask, "非 needle") && !strings.Contains(withTask, "广搜") && !strings.Contains(withTask, "优先 Task") {
		// 文件查找规则应与 Task 消歧，避免只鼓励直接 grep
		if strings.Contains(withTask, "文件查找工具选择：") && !strings.Contains(withTask, "Task") {
			t.Fatalf("with Task available, discovery rules must mention Task:\n%s", withTask)
		}
	}
	explicit, err := New().BuildSystem(context.Background(), BuildRequest{
		AvailableTools:    []string{"Task"},
		DelegationPosture: "explicit_request_only",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(explicit, "不算授权") {
		t.Fatalf("explicit posture missing gate:\n%s", explicit)
	}
	withoutTask, err := New().BuildSystem(context.Background(), BuildRequest{AvailableTools: []string{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutTask, "<delegation>") || strings.Contains(withoutTask, "子智能体委派纪律") {
		t.Fatalf("delegation must not inject without Task: %s", withoutTask)
	}
}

func TestBuildSystemSubAgentAudienceSkipsDelegation(t *testing.T) {
	got, err := New().BuildSystem(context.Background(), BuildRequest{
		Agent: &domain.Agent{SystemPrompt: "子智能体契约"},
		AvailableTools: []string{"Task", "read_file"},
		Audience:       AudienceSubAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "<delegation>") || strings.Contains(got, "子智能体委派纪律") {
		t.Fatalf("subagent audience must skip root delegation block:\n%s", got)
	}
	if strings.Contains(got, "思考时请清晰说明你的推理过程") {
		t.Fatalf("subagent audience must skip root tone:\n%s", got)
	}
	if !strings.Contains(got, "子智能体契约") {
		t.Fatalf("subagent system persona missing:\n%s", got)
	}
	if !strings.Contains(got, "failure_kind=repeated_failure") {
		t.Fatalf("subagent must keep failure fuse rules:\n%s", got)
	}
}

func TestBuildSystemVisionDegradedRules(t *testing.T) {
	got, err := New().BuildSystem(context.Background(), BuildRequest{
		AvailableTools: []string{"view_image", "sandbox_exec"},
		VisionMode:     "degraded_text",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"view_image", "Pillow", "vision_unavailable", "degraded_text"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in system prompt:\n%s", want, got)
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
	for _, want := range []string{`tool="run_command"`, `tool="sandbox_exec"`, "环境差异与命令执行规则"} {
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

func TestBuildSystemSubAgentAudienceSkipsEnvironmentContext(t *testing.T) {
	envInjector := NewEnvironmentContextInjector(EnvironmentContext{
		OS: "windows", HostCommandTool: "run_command", SandboxMode: "required",
		SandboxProvider: "genesis-sandbox", SandboxCommandTool: "sandbox_exec",
	})
	builder := New(envInjector)

	subAgentPrompt, err := builder.BuildSystem(context.Background(), BuildRequest{
		Agent:    &domain.Agent{SystemPrompt: "SubAgent Contract"},
		Audience: AudienceSubAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(subAgentPrompt, "<environment_context>") {
		t.Fatalf("AudienceSubAgent must skip EnvironmentContextInjector:\n%s", subAgentPrompt)
	}

	rootPrompt, err := builder.BuildSystem(context.Background(), BuildRequest{
		Agent:    &domain.Agent{SystemPrompt: "Root Contract"},
		Audience: AudienceRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootPrompt, "<environment_context>") {
		t.Fatalf("AudienceRoot must include EnvironmentContextInjector:\n%s", rootPrompt)
	}
}

func TestBuildSystemSkillForkAudienceSkipsAllInjectors(t *testing.T) {
	dummyInjector := ContextInjectorFunc(func(ctx context.Context, req BuildRequest) (Fragment, error) {
		return Fragment{Name: "dummy_fragment", Contents: "dummy content"}, nil
	})
	envInjector := NewEnvironmentContextInjector(EnvironmentContext{OS: "windows"})
	builder := New(dummyInjector, envInjector)

	got, err := builder.BuildSystem(context.Background(), BuildRequest{
		Agent:    &domain.Agent{SystemPrompt: "Skill Fork Persona"},
		Audience: AudienceSkillFork,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "dummy_fragment") || strings.Contains(got, "<environment_context>") {
		t.Fatalf("AudienceSkillFork must skip all injectors:\n%s", got)
	}
	if !strings.Contains(got, "Skill Fork Persona") {
		t.Fatalf("AudienceSkillFork must keep agent persona:\n%s", got)
	}
}

