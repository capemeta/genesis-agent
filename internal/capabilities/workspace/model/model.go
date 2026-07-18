// Package model 定义产品无关的工作空间资源模型。
package model

import (
	"fmt"
	"path"
	"strings"
	"time"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

const RunManifestSchemaVersion = "2"

// ResourceScope 描述资源的授权域。
type ResourceScope struct {
	TenantID  string `json:"tenant_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

// ResourceRef 是跨能力域传播的稳定资源引用。
type ResourceRef struct {
	Authority string        `json:"authority"`
	Scheme    string        `json:"scheme"`
	ID        string        `json:"id"`
	Path      string        `json:"path,omitempty"`
	Version   string        `json:"version,omitempty"`
	MediaType string        `json:"media_type,omitempty"`
	Scope     ResourceScope `json:"scope"`
}

// WorkspacePath 是某个 workspace namespace 内的相对路径。
type WorkspacePath string

// Validate 校验路径不携带物理根或越界语义。
func (p WorkspacePath) Validate() error {
	raw := strings.TrimSpace(string(p))
	if raw == "" {
		return fmt.Errorf("workspace path 不能为空")
	}
	if raw != string(p) || strings.ContainsRune(raw, '\x00') {
		return fmt.Errorf("workspace path 包含非法字符")
	}
	if strings.Contains(raw, `\`) {
		return fmt.Errorf("workspace path 必须使用正斜杠: %q", raw)
	}
	normalized := strings.ReplaceAll(raw, `\`, "/")
	if strings.HasPrefix(normalized, "/") || hasWindowsVolume(normalized) {
		return fmt.Errorf("workspace path 必须是相对路径: %q", raw)
	}
	clean := path.Clean(normalized)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("workspace path 越界: %q", raw)
	}
	if clean != normalized {
		return fmt.Errorf("workspace path 必须规范化: %q", raw)
	}
	return nil
}

func hasWindowsVolume(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':'
}

// StateRoot 是 Run 创建时固化的状态根引用。Path 只对相应 adapter 有意义。
type StateRoot struct {
	ID        string        `json:"id"`
	Authority string        `json:"authority"`
	Path      string        `json:"path,omitempty"`
	Scope     ResourceScope `json:"scope"`
}

// InputRef 描述已经完成校验与快照的不可变输入。
type InputRef struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Size       int64         `json:"size"`
	SHA256     string        `json:"sha256"`
	MIME       string        `json:"mime,omitempty"`
	Source     ResourceRef   `json:"source"`
	StagedPath WorkspacePath `json:"staged_path"`
}

// InputManifest 固化一次 execution 的输入映射。
type InputManifest struct {
	RunID     string     `json:"run_id"`
	BindingID string     `json:"binding_id"`
	Inputs    []InputRef `json:"inputs"`
	CreatedAt time.Time  `json:"created_at"`
}

// PreparedExecutionSnapshot 固化一个执行主体的 binding 与 backend 映射。
type PreparedExecutionSnapshot struct {
	Binding   execmodel.ExecutionBinding    `json:"binding"`
	Backend   execmodel.ExecutionBackendRef `json:"backend"`
	Workspace execmodel.ExecutionWorkspace  `json:"workspace"`
}

// RunManifest 是 Run 创建时原子写入的工作空间控制面快照。
// 物理路径只允许由对应产品的受信 adapter 持久化和读取，不进入跨产品业务协议。
type RunManifest struct {
	SchemaVersion    string                         `json:"schema_version"`
	Revision         uint64                         `json:"revision"`
	RunID            string                         `json:"run_id"`
	ParentRunID      string                         `json:"parent_run_id,omitempty"`
	Scope            ResourceScope                  `json:"scope"`
	AgentApp         agentappmodel.EffectiveProfile `json:"agent_app"`
	ArtifactRequired bool                           `json:"artifact_required,omitempty"`
	StateRoot        StateRoot                      `json:"state_root"`
	ProjectRoot      *ResourceRef                   `json:"project_root,omitempty"`
	ProjectDir       string                         `json:"project_dir,omitempty"`
	Limits           WorkspaceLimits                `json:"limits"`
	Executions       []PreparedExecutionSnapshot    `json:"executions"`
	CreatedAt        time.Time                      `json:"created_at"`
}

// WorkspaceLimits 是 Run 创建时固化的产品、策略、backend 与访问上界交集输入。
type WorkspaceLimits struct {
	ProductModes  []execmodel.WorkspaceMode `json:"product_modes,omitempty"`
	PolicyModes   []execmodel.WorkspaceMode `json:"policy_modes,omitempty"`
	BackendModes  []execmodel.WorkspaceMode `json:"backend_modes,omitempty"`
	MaximumAccess execmodel.WorkspaceAccess `json:"maximum_access"`
}

// PreparedRun 是控制面交给 Run Engine 的只读准备结果。
type PreparedRun struct {
	Manifest  RunManifest               `json:"manifest"`
	Execution PreparedExecutionSnapshot `json:"execution"`
}

// Validate 校验 manifest 的身份、引用和 execution 快照一致性。
func (m RunManifest) Validate() error {
	if m.SchemaVersion != RunManifestSchemaVersion || m.Revision == 0 || strings.TrimSpace(m.RunID) == "" {
		return fmt.Errorf("Run manifest schema/revision/run_id 无效（仅接受 schema %s）", RunManifestSchemaVersion)
	}
	if err := m.AgentApp.Validate(); err != nil {
		return fmt.Errorf("Run manifest agent app 无效: %w", err)
	}
	if strings.TrimSpace(m.StateRoot.ID) == "" || strings.TrimSpace(m.StateRoot.Authority) == "" {
		return fmt.Errorf("Run manifest state root 无效")
	}
	if m.StateRoot.Scope != m.Scope {
		return fmt.Errorf("Run manifest state root scope 不一致")
	}
	if m.ProjectRoot != nil && m.ProjectRoot.Scope != m.Scope {
		return fmt.Errorf("Run manifest project root scope 不一致")
	}
	if m.Limits.MaximumAccess != execmodel.WorkspaceAccessReadOnly && m.Limits.MaximumAccess != execmodel.WorkspaceAccessReadWrite {
		return fmt.Errorf("Run manifest maximum access 无效")
	}
	if len(m.Executions) == 0 {
		return fmt.Errorf("Run manifest 缺少 execution")
	}
	seen := make(map[string]struct{}, len(m.Executions))
	seenSubjects := make(map[string]struct{}, len(m.Executions))
	rootOwner := m.Executions[0].Binding.Owner
	for _, execution := range m.Executions {
		if execution.Binding.Owner.RunID != m.RunID {
			return fmt.Errorf("execution %s 不属于 Run %s", execution.Binding.ID, m.RunID)
		}
		owner := execution.Binding.Owner
		if owner.TenantID != m.Scope.TenantID || owner.ProjectID != m.Scope.ProjectID || owner.UserID != m.Scope.UserID {
			return fmt.Errorf("execution %s 的 tenant/project/user scope 与 Run manifest 不一致", execution.Binding.ID)
		}
		if owner.AgentAppID != m.AgentApp.ID || owner.AgentAppVersion != m.AgentApp.Version {
			return fmt.Errorf("execution %s 的 Agent App 与 Run manifest 不一致", execution.Binding.ID)
		}
		if owner.ParentRunID != m.ParentRunID || owner.SessionID != rootOwner.SessionID {
			return fmt.Errorf("execution %s 的 parent/session 与 Run manifest 不一致", execution.Binding.ID)
		}
		if _, exists := seen[execution.Binding.ID]; exists {
			return fmt.Errorf("execution binding %s 重复", execution.Binding.ID)
		}
		seen[execution.Binding.ID] = struct{}{}
		if subject := executionSubjectKey(owner, execution.Backend); subject != "" {
			if _, exists := seenSubjects[subject]; exists {
				return fmt.Errorf("execution subject %s 重复", subject)
			}
			seenSubjects[subject] = struct{}{}
		}
		if err := execution.Binding.Validate(); err != nil {
			return fmt.Errorf("execution binding 无效: %w", err)
		}
		if err := execution.Backend.Validate(); err != nil {
			return fmt.Errorf("execution backend 无效: %w", err)
		}
		if err := execution.Workspace.ValidateFor(execution.Binding); err != nil {
			return fmt.Errorf("execution workspace 无效: %w", err)
		}
	}
	return nil
}

func executionSubjectKey(owner execmodel.ExecutionOwnerRef, backend execmodel.ExecutionBackendRef) string {
	if owner.Subject().Empty() {
		return ""
	}
	return owner.TaskID + "\x00" + owner.SubAgentInstanceID + "\x00" + owner.WorkflowStepID + "\x00" + owner.CollaborationSpaceID + "\x00" + owner.MemberID + "\x00" + string(backend.Kind) + "\x00" + backend.Authority
}
