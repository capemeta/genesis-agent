package contract

import (
	"context"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	skillcontract "genesis-agent/internal/capabilities/skill/contract"
	skillmodel "genesis-agent/internal/capabilities/skill/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// Runner 执行已 materialize 的 Skill 命令（产品无关端口）。
type Runner interface {
	Run(ctx context.Context, req RunRequest) (*RunResult, error)
}

type AutoScriptPayload struct {
	Name    string
	Content []byte
}

// RunRequest 描述一次 Skill 命令执行。
type RunRequest struct {
	Catalog        skillcontract.CatalogRequest
	Skill          string
	Invocation     skillmodel.InvocationBinding
	Command        string
	AutoScriptFile *AutoScriptPayload
	Inputs         workmodel.InputManifest // 控制面完成权限、版本、hash 与 staging 后生成的不可变输入清单
	Binding        execmodel.ExecutionBinding
	Backend        execmodel.ExecutionBackendRef
	StateRoot      workmodel.StateRoot
	ProjectDir     string
	TimeoutMS      int64
	Sandbox        execmodel.SandboxProfile
}

// RunResult 是 Skill 命令执行结果。
type RunResult struct {
	OK           bool                `json:"ok"`
	Skill        string              `json:"skill"`
	Script       string              `json:"script,omitempty"`
	Command      string              `json:"command"`
	ExitCode     int                 `json:"exit_code"`
	Stdout       string              `json:"stdout,omitempty"`
	Stderr       string              `json:"stderr,omitempty"`
	SkillDir     string              `json:"-"`
	WorkDir      string              `json:"-"`
	Produced     []ProducedCandidate `json:"produced,omitempty"`
	StagedInputs []string            `json:"staged_inputs,omitempty"`
	Error        string              `json:"error,omitempty"`
	Warnings     []string            `json:"warnings,omitempty"`
	Metadata     map[string]string   `json:"metadata,omitempty"`
	// DurationMS 是 run_skill_command harness 总耗时，不等同于子进程执行耗时。
	DurationMS          int64 `json:"duration_ms"`
	ApprovalDurationMS  int64 `json:"approval_duration_ms"`
	StagingDurationMS   int64 `json:"staging_duration_ms"`
	ExecutionDurationMS int64 `json:"execution_duration_ms"`
	// 其余字段描述失败分类与恢复建议。
	FailureKind      string              `json:"failure_kind,omitempty"`
	Missing          []MissingDep        `json:"missing,omitempty"`
	SuggestedInstall *SuggestedInstall   `json:"suggested_install,omitempty"`
	SuggestedAction  string              `json:"suggested_action,omitempty"`
	Retryable        bool                `json:"retryable,omitempty"`
	RequiredInputs   []string            `json:"required_inputs,omitempty"`
	ExactCall        *ToolCallSuggestion `json:"exact_call,omitempty"`
}

// ProducedCandidate 是普通模型可见的最小候选投影，不含路径、locator 或 version token。
type ProducedCandidate struct {
	CandidateID   string `json:"candidate_id"`
	Name          string `json:"name"`
	MediaType     string `json:"media_type,omitempty"`
	DeliverableID string `json:"deliverable_id,omitempty"`
	// Role 可选：空/省略表示交付相关候选；qa_asset 表示视觉 QA 预览图（不触发 Delivery）。
	Role string `json:"role,omitempty"`
}

// ToolCallSuggestion 是 Harness 返回的可直接执行纠错调用。
type ToolCallSuggestion struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
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
