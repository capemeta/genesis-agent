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

// WorkspaceMode 描述一次执行的工作路径语义。
type WorkspaceMode string

const (
	WorkspaceModeLocalCoding WorkspaceMode = "local_coding_workspace"
	WorkspaceModeLocalTask   WorkspaceMode = "local_task_workspace"
	WorkspaceModeSandboxSess WorkspaceMode = "sandbox_session_workspace"
)

// PathPolicy 描述执行前路径契约强度。
type PathPolicy string

const (
	PathPolicyStrictWorkspace   PathPolicy = "strict_workspace_contract"
	PathPolicyAdvisoryWorkspace PathPolicy = "advisory_workspace_contract"
	PathPolicyPermissionOnly    PathPolicy = "permission_only"
)

// ExecutionWorkspace 描述代码执行时注入给进程的逻辑工作目录。
type ExecutionWorkspace struct {
	Mode       WorkspaceMode     `json:"mode,omitempty"`
	PathPolicy PathPolicy        `json:"path_policy,omitempty"`
	WorkDir    string            `json:"work_dir,omitempty"`
	InputDir   string            `json:"input_dir,omitempty"`
	OutputDir  string            `json:"output_dir,omitempty"`
	TmpDir     string            `json:"tmp_dir,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// InputArtifactRef 描述已 staging 到执行环境的输入文件或资源。
type InputArtifactRef struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name,omitempty"`
	LocalPath   string            `json:"local_path,omitempty"`
	RemotePath  string            `json:"remote_path,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	JobID       string            `json:"job_id,omitempty"`
	Size        int64             `json:"size,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	MIME        string            `json:"mime,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ArtifactCollectionPolicy 描述成果物收集策略。
type ArtifactCollectionPolicy string

const (
	ArtifactCollectionOutputOnly ArtifactCollectionPolicy = "output_only"
	ArtifactCollectionDisabled   ArtifactCollectionPolicy = "disabled"
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
	Command    string            `json:"command"`
	Cwd        string            `json:"cwd,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Shell      ShellKind         `json:"shell,omitempty"`
	Background bool              `json:"background,omitempty"`
	UsePTY     bool              `json:"use_pty,omitempty"`
}

// Result 描述命令执行结果。
type Result struct {
	Command         string               `json:"command"`
	Cwd             string               `json:"cwd"`
	Shell           ShellKind            `json:"shell,omitempty"`
	Environment     ExecutionEnvironment `json:"environment,omitempty"`
	SandboxProvider string               `json:"sandbox_provider,omitempty"`
	Warnings        []string             `json:"warnings,omitempty"`
	Artifacts       []Artifact           `json:"artifacts,omitempty"`
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

// Artifact 描述命令执行产生的远程或已落盘产物。
type Artifact struct {
	ID          string            `json:"id,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	JobID       string            `json:"job_id,omitempty"`
	Name        string            `json:"name,omitempty"`
	Size        int64             `json:"size,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	MIME        string            `json:"mime,omitempty"`
	RemoteURL   string            `json:"remote_url,omitempty"`
	LocalPath   string            `json:"local_path,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}
