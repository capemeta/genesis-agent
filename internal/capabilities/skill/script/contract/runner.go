package contract

import (
	"context"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	"genesis-agent/internal/capabilities/skill/model"
)

// Runner 执行已 materialize 的 Skill 脚本（产品无关端口）。
type Runner interface {
	Run(ctx context.Context, req RunRequest) (*RunResult, error)
}

// RunRequest 描述一次 Skill 脚本执行。
type RunRequest struct {
	Catalog       skillcontract.CatalogRequest
	Skill         string
	Script        model.ResourceID // 如 office-ppt/scripts/inspect_pptx.py
	Args          []string
	Inputs        []string // 工作区相对路径或文件名；将 stage 到 INPUT_DIR
	RunID         string
	TimeoutMS     int64
	PythonBin     string // 默认 python
	Sandbox       execmodel.SandboxProfile
	WorkspaceRoot string // 可选；默认当前进程工作区
}

// RunResult 是脚本执行结果。
type RunResult struct {
	OK               bool              `json:"ok"`
	Skill            string            `json:"skill"`
	Script           string            `json:"script"`
	Command          string            `json:"command"`
	ExitCode         int               `json:"exit_code"`
	Stdout           string            `json:"stdout,omitempty"`
	Stderr           string            `json:"stderr,omitempty"`
	SkillDir         string            `json:"skill_dir,omitempty"`
	InputDir         string            `json:"input_dir,omitempty"`
	OutputDir        string            `json:"output_dir,omitempty"`
	StagedInputs     []string          `json:"staged_inputs,omitempty"`
	Artifacts        []Artifact        `json:"artifacts,omitempty"`
	Error            string            `json:"error,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	DurationMS       int64             `json:"duration_ms,omitempty"`
	FailureKind      string            `json:"failure_kind,omitempty"`
	Missing          []MissingDep      `json:"missing,omitempty"`
	SuggestedInstall *SuggestedInstall `json:"suggested_install,omitempty"`
	SuggestedAction  string            `json:"suggested_action,omitempty"`
	Retryable        bool              `json:"retryable,omitempty"`
}

// MissingDep 描述缺失的 runtime 依赖。
type MissingDep struct {
	Manager string `json:"manager,omitempty"`
	Name    string `json:"name"`
	Require string `json:"require,omitempty"`
}

// SuggestedInstall 指引 Agent 走显式安装通道（Gate B 工具落地后对齐）。
type SuggestedInstall struct {
	Tool          string         `json:"tool"`
	Args          map[string]any `json:"args,omitempty"`
	ShellFallback string         `json:"shell_fallback,omitempty"`
}

// Artifact 描述 OUTPUT_DIR 中的交付物。
type Artifact struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Kind   string `json:"kind,omitempty"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}
