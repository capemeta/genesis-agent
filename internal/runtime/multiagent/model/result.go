package model

// ResultStatus 表示父 Agent 可见的子任务终态。
type ResultStatus string

const (
	ResultStatusCompleted ResultStatus = "completed"
	ResultStatusPartial   ResultStatus = "partial"
	ResultStatusFailed    ResultStatus = "failed"
	ResultStatusCancelled ResultStatus = "cancelled"
)

// ResultError 是经过清洗的稳定错误投影。
type ResultError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// Usage 汇总子 Run 可安全暴露的使用量。
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	ToolCalls    int   `json:"tool_calls"`
	BudgetHit    bool  `json:"budget_exhausted"`
}

// Finding 仅承载已校验的结构化结论。当前 Phase 1 不自动从自由文本提取。
type Finding struct {
	Claim    string   `json:"claim"`
	Evidence []string `json:"evidence"`
}

// Artifact 是已登记产物的最小模型投影。
type Artifact struct {
	ResourceID  string `json:"resource_id,omitempty"`
	Path        string `json:"path,omitempty"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// ArtifactManifest 是子 Run 显式登记的产物集合。
// 它只表示候选交付物，不能直接回传给父 Agent，必须先经过 EvidenceValidator。
type ArtifactManifest struct {
	Artifacts []Artifact
}

// TaskResult 是父 Agent 可见的终态结果协议。
type TaskResult struct {
	SchemaVersion   int          `json:"schema_version"`
	ResultID        string       `json:"result_id"`
	Status          ResultStatus `json:"status"`
	AgentID         string       `json:"agent_id"`
	ChildRunID      string       `json:"child_run_id,omitempty"`
	SubagentType    string       `json:"subagent_type"`
	Summary         string       `json:"summary,omitempty"`
	Findings        []Finding    `json:"findings,omitempty"`
	Artifacts       []Artifact   `json:"artifacts,omitempty"`
	Error           *ResultError `json:"error,omitempty"`
	Usage           Usage        `json:"usage"`
	Truncated       bool         `json:"truncated,omitempty"`
	OmittedSections []string     `json:"omitted_sections,omitempty"`
	NextAction      string       `json:"next_action,omitempty"`
}

// TaskLaunch 是异步委派受理结果，不能与终态结果混用。
type TaskLaunch struct {
	Status     string `json:"status"`
	AgentID    string `json:"agent_id"`
	ChildRunID string `json:"child_run_id,omitempty"`
}

// Instance 保存控制平面运行态和已归约的终态结果。
// Result 仅在实例到达终态后可用。
//
// Instance 原有字段暂时保留给 Progress 和内部控制面，模型工具结果必须优先使用 Result。
