// Package model 定义命令执行能力的数据模型。
package model

import "time"

// ShellKind 描述命令希望使用的 shell。auto 由产品侧 runner 按平台选择。
type ShellKind string

const (
	ShellAuto       ShellKind = "auto"
	ShellSystem     ShellKind = "system"
	ShellBash       ShellKind = "bash"
	ShellSh         ShellKind = "sh"
	ShellZsh        ShellKind = "zsh"
	ShellPowerShell ShellKind = "powershell"
	ShellCmd        ShellKind = "cmd"
)

// ShellInfo 描述一个已经由产品执行环境验证可用的 Shell。
type ShellInfo struct {
	Kind ShellKind `json:"kind"`
	Path string    `json:"path,omitempty"`
}

// ShellCapabilities 描述当前执行环境真实可用的 Shell 能力。
// Default 必须出现在 Supported 中；未知远程环境可以返回零值，由调用方保守使用 auto。
type ShellCapabilities struct {
	Default   ShellInfo   `json:"default"`
	Supported []ShellInfo `json:"supported,omitempty"`
}

// ExecutionEnvironment 描述命令实际执行环境。
type ExecutionEnvironment string

const (
	EnvironmentLocal   ExecutionEnvironment = "local"
	EnvironmentSandbox ExecutionEnvironment = "sandbox"
)

// SandboxMode 描述沙箱使用策略。
type SandboxMode string

const (
	SandboxDisabled SandboxMode = "disabled"
	SandboxOptional SandboxMode = "optional"
	SandboxRequired SandboxMode = "required"
)

// SandboxTaskType 描述提交给 genesis-sandbox 的任务类型。
type SandboxTaskType string

const (
	SandboxTaskCode    SandboxTaskType = "code"
	SandboxTaskShell   SandboxTaskType = "shell"
	SandboxTaskTool    SandboxTaskType = "tool"
	SandboxTaskSkill   SandboxTaskType = "skill"
	SandboxTaskBuild   SandboxTaskType = "build"
	SandboxTaskOffice  SandboxTaskType = "office"
	SandboxTaskBrowser SandboxTaskType = "browser"
	SandboxTaskDesktop SandboxTaskType = "desktop"
)

// SandboxOperation 描述提交给 genesis-sandbox 的能力操作。
type SandboxOperation string

const (
	SandboxOperationRunCode           SandboxOperation = "run_code"
	SandboxOperationRunShell          SandboxOperation = "run_shell"
	SandboxOperationRunTool           SandboxOperation = "run_tool"
	SandboxOperationRunSkill          SandboxOperation = "run_skill"
	SandboxOperationBuildDependencies SandboxOperation = "build_dependencies"
	SandboxOperationConvertToPDF      SandboxOperation = "convert_to_pdf"
	SandboxOperationExtractText       SandboxOperation = "extract_text"
	SandboxOperationPreview           SandboxOperation = "preview"
	SandboxOperationInspect           SandboxOperation = "inspect"
	SandboxOperationGenerateDocx      SandboxOperation = "generate_docx"
	SandboxOperationGeneratePptx      SandboxOperation = "generate_pptx"
	SandboxOperationProcessXlsx       SandboxOperation = "process_xlsx"
	SandboxOperationOCRPDF            SandboxOperation = "ocr_pdf"
	SandboxOperationOCRImage          SandboxOperation = "ocr_image"
	SandboxOperationBrowserRun        SandboxOperation = "browser_run"
	SandboxOperationScreenshot        SandboxOperation = "screenshot"
	SandboxOperationExtractPage       SandboxOperation = "extract_page"
	SandboxOperationFillForm          SandboxOperation = "fill_form"
	SandboxOperationBrowserGUI        SandboxOperation = "browser_gui"
	SandboxOperationVNCSession        SandboxOperation = "vnc_session"
	SandboxOperationDesktopSession    SandboxOperation = "desktop_session"
	SandboxOperationNoVNCSession      SandboxOperation = "novnc_session"
)

// SandboxRuntimeProfile 是 genesis-sandbox 当前稳定暴露的 runtime profile 名称。
type SandboxRuntimeProfile string

const (
	RuntimeProfileCodePolyglotBasic  SandboxRuntimeProfile = "code-polyglot-basic"
	RuntimeProfileCodePythonIsolated SandboxRuntimeProfile = "code-python-isolated"
	RuntimeProfileToolBasic          SandboxRuntimeProfile = "tool-basic"
	RuntimeProfileSkillPolyglotBasic SandboxRuntimeProfile = "skill-polyglot-basic"
	RuntimeProfileSkillBuildPolyglot SandboxRuntimeProfile = "skill-build-polyglot"
	RuntimeProfileOfficeBasic        SandboxRuntimeProfile = "office-basic"
	RuntimeProfileOfficeOCR          SandboxRuntimeProfile = "office-ocr"
	RuntimeProfileBrowserPlaywright  SandboxRuntimeProfile = "browser-playwright"
	RuntimeProfileBrowserChrome      SandboxRuntimeProfile = "browser-chrome"
	RuntimeProfileBrowserDesktop     SandboxRuntimeProfile = "browser-desktop"
)

// SandboxRiskLevel 描述一次执行的风险等级，供产品策略和 sandbox 服务裁决降级。
type SandboxRiskLevel string

const (
	SandboxRiskLow    SandboxRiskLevel = "low"
	SandboxRiskMedium SandboxRiskLevel = "medium"
	SandboxRiskHigh   SandboxRiskLevel = "high"
)

// WorkspaceMode 描述一次执行绑定的资源生命周期语义。
// 它与产品端、物理 backend 和 sandbox provider 正交。
type WorkspaceMode string

const (
	WorkspaceModeProject WorkspaceMode = "project_workspace"
	WorkspaceModeTask    WorkspaceMode = "task_job"
	WorkspaceModeSession WorkspaceMode = "session_workspace"
)

// WorkspaceAccess 描述执行主体对绑定工作空间的最大访问姿态。
// 具体文件操作仍须经过 PathResolver、PermissionEngine 和锁/Freshness 校验。
type WorkspaceAccess string

const (
	WorkspaceAccessReadOnly  WorkspaceAccess = "read_only"
	WorkspaceAccessReadWrite WorkspaceAccess = "read_write"
)

// PathPolicy 描述执行前路径契约强度。
type PathPolicy string

const (
	PathPolicyStrictWorkspace   PathPolicy = "strict_workspace_contract"
	PathPolicyAdvisoryWorkspace PathPolicy = "advisory_workspace_contract"
	PathPolicyPermissionOnly    PathPolicy = "permission_only"
)

// ExecutionOwnerRef 描述一次执行绑定的可信所有者与关联拓扑。
// 这些字段由控制面注入，不能从 LLM 工具参数构造。
type ExecutionOwnerRef struct {
	TenantID             string `json:"tenant_id,omitempty"`
	ProjectID            string `json:"project_id,omitempty"`
	UserID               string `json:"user_id,omitempty"`
	SessionID            string `json:"session_id,omitempty"`
	AgentAppID           string `json:"agent_app_id,omitempty"`
	AgentAppVersion      string `json:"agent_app_version,omitempty"`
	TaskID               string `json:"task_id,omitempty"`
	RunID                string `json:"run_id"`
	ParentRunID          string `json:"parent_run_id,omitempty"`
	SubAgentInstanceID   string `json:"subagent_instance_id,omitempty"`
	WorkflowStepID       string `json:"workflow_step_id,omitempty"`
	CollaborationSpaceID string `json:"collaboration_space_id,omitempty"`
	MemberID             string `json:"member_id,omitempty"`
}

// ExecutionSubjectRef 只描述 Run 内或编排拓扑中的执行主体。
// tenant/run/App/session 等授权身份必须由控制面从 Run manifest 补全。
type ExecutionSubjectRef struct {
	TaskID               string `json:"task_id,omitempty"`
	SubAgentInstanceID   string `json:"subagent_instance_id,omitempty"`
	WorkflowStepID       string `json:"workflow_step_id,omitempty"`
	CollaborationSpaceID string `json:"collaboration_space_id,omitempty"`
	MemberID             string `json:"member_id,omitempty"`
}

// ExecutionBinding 固化一次执行的模式、所有者和能力上限。
// binding 在 Run 内不可变；切换模式或 owner 必须创建新 binding。
type ExecutionBinding struct {
	ID         string            `json:"id"`
	Mode       WorkspaceMode     `json:"mode"`
	Access     WorkspaceAccess   `json:"access"`
	PathPolicy PathPolicy        `json:"path_policy,omitempty"`
	Owner      ExecutionOwnerRef `json:"owner"`
}

// ExecutionWorkspace 描述执行 backend 实际注入给进程的逻辑目录映射。
// 它不携带业务模式或权限；这些语义只存在于 ExecutionBinding。
type ExecutionWorkspace struct {
	WorkDir   string            `json:"work_dir,omitempty"`
	InputDir  string            `json:"input_dir,omitempty"`
	OutputDir string            `json:"output_dir,omitempty"`
	TmpDir    string            `json:"tmp_dir,omitempty"`
	SkillDir  string            `json:"skill_dir,omitempty"` // 本次可执行的 skill 包根（含 scripts/）
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// StagedInputRef 描述已由控制面 staging 到执行环境的输入资源。
type StagedInputRef struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	JobID       string            `json:"job_id,omitempty"`
	Size        int64             `json:"size,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	MIME        string            `json:"mime,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// OutputDiscoveryPolicy 描述 executor 对输出对象的发现策略；发现结果不是正式 Artifact。
type OutputDiscoveryPolicy string

const (
	OutputDiscoveryDeclared OutputDiscoveryPolicy = "declared_outputs"
	OutputDiscoveryDisabled OutputDiscoveryPolicy = "disabled"
)

// SandboxProfile 描述 Docker、genesis-sandbox 或平台沙箱需要的执行上下文。
type SandboxProfile struct {
	Mode           SandboxMode           `json:"mode,omitempty"`
	Provider       string                `json:"provider,omitempty"`
	WorkspaceID    string                `json:"workspace_id,omitempty"`
	RuntimeProfile SandboxRuntimeProfile `json:"runtime_profile,omitempty"`
	TaskType       SandboxTaskType       `json:"task_type,omitempty"`
	Operation      SandboxOperation      `json:"operation,omitempty"`
	Language       string                `json:"language,omitempty"`
	RiskLevel      SandboxRiskLevel      `json:"risk_level,omitempty"`
	Metadata       map[string]string     `json:"metadata,omitempty"`
}

// Command 描述一次命令执行请求。
type Command struct {
	Command string            `json:"command"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// Stdin 是传递给命令标准输入的原始字节。远程 sandbox transport 按 JSON base64 编码。
	Stdin      []byte    `json:"stdin,omitempty"`
	Shell      ShellKind `json:"shell,omitempty"`
	Background bool      `json:"background,omitempty"`
	UsePTY     bool      `json:"use_pty,omitempty"`
}

// Result 描述命令执行结果。
type Result struct {
	Command         string                 `json:"command"`
	Cwd             string                 `json:"cwd"`
	Shell           ShellKind              `json:"shell,omitempty"`
	Environment     ExecutionEnvironment   `json:"environment,omitempty"`
	SandboxProvider string                 `json:"sandbox_provider,omitempty"`
	Warnings        []string               `json:"warnings,omitempty"`
	OutputObjects   []ExecutorOutputObject `json:"output_objects,omitempty"`
	// SandboxViolations 携带 OS sandbox 拒绝的结构化事件（如 macOS Seatbelt denial）。
	// 每条记录格式为 "operation:path" 或 "operation"（无路径时省略 ":"）。
	SandboxViolations []string      `json:"sandbox_violations,omitempty"`
	ExitCode          int           `json:"exit_code"`
	Stdout            string        `json:"stdout,omitempty"`
	Stderr            string        `json:"stderr,omitempty"`
	Duration          time.Duration `json:"-"`
	DurationMS        int64         `json:"duration_ms"`
	TimedOut          bool          `json:"timed_out"`
	OutputTruncated   bool          `json:"output_truncated"`
	Error             string        `json:"error,omitempty"`
}
