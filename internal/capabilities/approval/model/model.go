// Package model 定义通用审批能力的数据模型。
package model

// Action 描述需要审批的动作。
type Action string

const (
	ActionFileRead          Action = "file.read"
	ActionFileWrite         Action = "file.write"
	ActionFileEdit          Action = "file.edit"
	ActionFileList          Action = "file.list"
	ActionFileWalk          Action = "file.walk"
	ActionCommandExec       Action = "command.exec"
	ActionHTTPRequest       Action = "http.request"
	ActionMCPCall           Action = "mcp.call"
	ActionSkillLoad         Action = "skill.load"
	ActionSkillResourceRead Action = "skill.resource.read"
	ActionSkillInstall      Action = "skill.install"
	ActionSubAgentDelegate  Action = "subagent.delegate"
	// ActionPlanExitApprove 方案期退出规划模式批准（与任务清单 blocked_by_approval 语义分离）。
	ActionPlanExitApprove Action = "plan.exit_approve"
)

// Resource 描述动作作用的资源。
type Resource struct {
	Type     string            `json:"type"`
	URI      string            `json:"uri"`
	Display  string            `json:"display,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RiskLevel 描述风险等级。
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// GrantScope 描述授权记忆的时间/归属范围。
type GrantScope string

const (
	GrantScopeOnce    GrantScope = "once"
	GrantScopeTurn    GrantScope = "turn"
	GrantScopeSession GrantScope = "session"
	GrantScopeProject GrantScope = "project"
	GrantScopeTenant  GrantScope = "tenant"
	GrantScopeGlobal  GrantScope = "global"
)

// PathGrantMode 描述文件授权的路径广度（与 GrantScope 正交）。
type PathGrantMode string

const (
	// PathGrantExact 仅授权请求路径本身（文件或目录）。
	PathGrantExact PathGrantMode = "exact"
	// PathGrantDirectory 授权文件的直接父目录（含子树）；对目录请求等价于该目录本身。
	PathGrantDirectory PathGrantMode = "directory"
)

// DecisionType 描述审批结果。
type DecisionType string

const (
	DecisionApproved         DecisionType = "approved"
	DecisionApprovedForScope DecisionType = "approved_for_scope"
	DecisionDenied           DecisionType = "denied"
	DecisionAbort            DecisionType = "abort"
	DecisionTimedOut         DecisionType = "timed_out"
)

// PolicyType 描述策略评估结果。
type PolicyType string

const (
	PolicyAllow PolicyType = "allow"
	PolicyAsk   PolicyType = "ask"
	PolicyDeny  PolicyType = "deny"
)

// Request 是一次通用审批请求。
type Request struct {
	ID              string            `json:"id,omitempty"`
	ToolName        string            `json:"tool_name,omitempty"`
	Action          Action            `json:"action"`
	Resource        Resource          `json:"resource"`
	Reason          string            `json:"reason,omitempty"`
	Risk            RiskLevel         `json:"risk,omitempty"`
	SuggestedScopes []GrantScope      `json:"suggested_scopes,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Decision 是最终审批决策。
type Decision struct {
	Type     DecisionType  `json:"type"`
	Scope    GrantScope    `json:"scope,omitempty"`
	PathMode PathGrantMode `json:"path_mode,omitempty"`
	Reason   string        `json:"reason,omitempty"`
}

// PolicyResult 是策略引擎输出。
type PolicyResult struct {
	Type            PolicyType   `json:"type"`
	Reason          string       `json:"reason,omitempty"`
	Risk            RiskLevel    `json:"risk,omitempty"`
	SuggestedScopes []GrantScope `json:"suggested_scopes,omitempty"`
}

// GrantKey 是授权缓存 key。
type GrantKey struct {
	Action      Action     `json:"action"`
	ResourceURI string     `json:"resource_uri"`
	Scope       GrantScope `json:"scope"`
}

// KeyFor 为请求生成授权缓存 key。
func KeyFor(req Request, scope GrantScope) GrantKey {
	return GrantKey{Action: req.Action, ResourceURI: req.Resource.URI, Scope: scope}
}
