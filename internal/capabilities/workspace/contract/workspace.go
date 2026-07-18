// Package contract 定义工作空间能力端口。
package contract

import (
	"context"
	"errors"
	"fmt"
	"io"

	agentappmodel "genesis-agent/internal/capabilities/agentapp/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ErrorCode 是工作空间能力稳定错误码。
type ErrorCode string

const (
	ErrCodeStateRootUnavailable         ErrorCode = "STATE_ROOT_UNAVAILABLE"
	ErrCodeWorkspaceNotAvailable        ErrorCode = "WORKSPACE_NOT_AVAILABLE"
	ErrCodePathNamespaceMismatch        ErrorCode = "PATH_NAMESPACE_MISMATCH"
	ErrCodeResourceVersionConflict           ErrorCode = "RESOURCE_VERSION_CONFLICT"
	ErrCodeProducedResourceVersionConflict   ErrorCode = "PRODUCED_RESOURCE_VERSION_CONFLICT"
	ErrCodeInputPermissionDenied             ErrorCode = "INPUT_PERMISSION_DENIED"
	ErrCodeInputNameConflict            ErrorCode = "INPUT_NAME_CONFLICT"
	ErrCodeInputReservedPathConflict    ErrorCode = "INPUT_RESERVED_PATH_CONFLICT"
	ErrCodeInputTooLarge                ErrorCode = "INPUT_TOO_LARGE"
	ErrCodeCrossExecutionResourceDenied ErrorCode = "CROSS_EXECUTION_RESOURCE_DENIED"
	ErrCodeProducedResourceInvalid      ErrorCode = "PRODUCED_RESOURCE_INVALID"
	ErrCodeProducedResourceConflict     ErrorCode = "PRODUCED_RESOURCE_CONFLICT"
	ErrCodeProducedResourceNotFound          ErrorCode = "PRODUCED_RESOURCE_NOT_FOUND"
	ErrCodeResourceBackendMismatch           ErrorCode = "RESOURCE_BACKEND_MISMATCH"
	ErrCodeProducedResourceBackendMismatch   ErrorCode = "PRODUCED_RESOURCE_BACKEND_MISMATCH"
	ErrCodeResourceReaderNotFound            ErrorCode = "RESOURCE_READER_NOT_FOUND"
	ErrCodeProducedResourceExpired           ErrorCode = "PRODUCED_RESOURCE_EXPIRED"
)

// Error 携带稳定分类和根因。
type Error struct {
	Code ErrorCode
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Code, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// NewError 创建工作空间分类错误。
func NewError(code ErrorCode, err error) error {
	if err == nil {
		err = errors.New(string(code))
	}
	return &Error{Code: code, Err: err}
}

// StateRootRequest 是产品在 Run 创建时提交给 resolver 的可信输入。
type StateRootRequest struct {
	RunID       string
	Mode        execmodel.WorkspaceMode
	Scope       workmodel.ResourceScope
	ProjectRoot *workmodel.ResourceRef
}

// StateRootResolver 在 Run 创建时一次性解析稳定状态根。
type StateRootResolver interface {
	ResolveStateRoot(ctx context.Context, req StateRootRequest) (workmodel.StateRoot, error)
}

// PrepareRequest 描述一次 binding 的物理工作空间准备请求。
type PrepareRequest struct {
	StateRoot workmodel.StateRoot
	Binding   execmodel.ExecutionBinding
	// Backend 由可信 Harness 在执行前选择；Provisioner 不得静默改写。
	Backend    execmodel.ExecutionBackendRef
	ProjectDir string
	SkillDir   string
}

// PreparedExecution 是控制面生成的 binding 与 backend 实际路径映射。
type PreparedExecution struct {
	Binding   execmodel.ExecutionBinding
	Backend   execmodel.ExecutionBackendRef
	Workspace execmodel.ExecutionWorkspace
}

// Provisioner 准备执行工作空间；实现必须保持 binding 不变。
type Provisioner interface {
	Prepare(ctx context.Context, req PrepareRequest) (PreparedExecution, error)
}

// ResourceHandle 是已打开且绑定版本的输入资源流。
type ResourceHandle struct {
	Reader    io.ReadCloser
	Size      int64
	Version   string
	MediaType string
}

// ResourceReader 按稳定 ResourceRef 打开资源，禁止接受裸路径。
type ResourceReader interface {
	Open(ctx context.Context, ref workmodel.ResourceRef) (ResourceHandle, error)
}

// InputSnapshotStore 写入不可变 Run 输入快照。
type InputSnapshotStore interface {
	Put(ctx context.Context, runID, inputID, name string, content io.Reader) (workmodel.WorkspacePath, error)
	Remove(ctx context.Context, stagedPath workmodel.WorkspacePath) error
}

// InputSnapshotReader 只按 Stager 返回的 WorkspacePath 读取不可变输入快照。
type InputSnapshotReader interface {
	OpenSnapshot(ctx context.Context, stagedPath workmodel.WorkspacePath) (io.ReadCloser, error)
}

// InputStager 对输入执行权限后版本校验、限额、hash、重命名与快照。
type InputStager interface {
	Stage(ctx context.Context, req StageRequest) (workmodel.InputManifest, error)
}

// RunManifestStore 持久化 Run 工作空间控制面快照。
// Create 必须是排他创建，禁止覆盖同 ID 的既有 manifest。
type RunManifestStore interface {
	Create(ctx context.Context, manifest workmodel.RunManifest) error
	Get(ctx context.Context, tenantID, runID string) (workmodel.RunManifest, error)
	AddExecution(ctx context.Context, tenantID, runID string, expectedRevision uint64, execution workmodel.PreparedExecutionSnapshot) (workmodel.RunManifest, error)
}

// RunPreparer 在 Run Engine 启动前完成 ID、有效 App、binding、state root 与物理映射固化。
type RunPreparer interface {
	PrepareRun(ctx context.Context, req PrepareRunRequest) (workmodel.PreparedRun, error)
}

// ExecutionPreparer 为同一 Run 内的 Hook、Skill、Workflow step 等派生主体创建独立 binding。
type ExecutionPreparer interface {
	PrepareExecution(ctx context.Context, req PrepareExecutionRequest) (workmodel.PreparedExecutionSnapshot, error)
}

// ControlPlane 是产品注入 Run 上下文的完整工作空间控制面。
type ControlPlane interface {
	RunPreparer
	ExecutionPreparer
	GetRunManifest(ctx context.Context, tenantID, runID string) (workmodel.RunManifest, error)
}

// IntentResolver 在 Run 创建前把产品已接收的请求解析为可信业务执行意图。
// 它只能收窄执行模式与声明完成条件，不能授予项目、路径或写权限。
type IntentResolver interface {
	ResolveIntent(ctx context.Context, req ResolveIntentRequest) (ExecutionIntent, error)
}

type ResolveIntentRequest struct {
	Prompt     string
	Supplied   ExecutionIntent
	HasProject bool
}

// PrepareExecutionRequest 只允许声明派生主体身份和收窄需求，Run/App/资源上界从 manifest 继承。
type PrepareExecutionRequest struct {
	Subject         execmodel.ExecutionSubjectRef
	Backend         execmodel.ExecutionBackendRef
	Intent          ExecutionIntent
	RequestedAccess execmodel.WorkspaceAccess
	SkillDir        string
}

// PrepareRunRequest 只接收产品入口已经鉴权的资源与任务意图。
type PrepareRunRequest struct {
	Scope           workmodel.ResourceScope
	SessionID       string
	ParentRunID     string
	AgentID         string
	Subject         execmodel.ExecutionSubjectRef
	App             agentappmodel.EffectiveProfile
	Intent          ExecutionIntent
	ProjectRoot     *workmodel.ResourceRef
	ProjectDir      string
	ProductModes    []execmodel.WorkspaceMode
	PolicyModes     []execmodel.WorkspaceMode
	BackendModes    []execmodel.WorkspaceMode
	MaximumAccess   execmodel.WorkspaceAccess
	RequestedAccess execmodel.WorkspaceAccess
}

// ExecutionIntent 是跨 app/workspace 契约传播的业务意图。
type ExecutionIntent struct {
	ExplicitMode       execmodel.WorkspaceMode
	RequiredMode       execmodel.WorkspaceMode
	ModifyProject      bool
	BoundedInputs      bool
	BoundedOutputs     bool
	NeedsPersistentRun bool
	HasProject         bool
	ArtifactRequired   bool // 任务承诺生成用户可见文件，完成前必须正式发布 Artifact
}

// StageRequest 描述一次 execution 的输入集合。
type StageRequest struct {
	Binding     execmodel.ExecutionBinding
	Sources     []workmodel.ResourceRef
	MaxFileSize int64
	MaxTotal    int64
}
