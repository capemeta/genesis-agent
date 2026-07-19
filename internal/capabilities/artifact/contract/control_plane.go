package contract

import (
	"context"
	"errors"

	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

var (
	ErrNotFound            = errors.New("artifact control record not found")
	ErrAlreadyExists       = errors.New("artifact control record already exists")
	ErrRevisionConflict    = errors.New("artifact control revision conflict")
	ErrIdempotencyConflict = errors.New("artifact control idempotency conflict")
)

type DeliverableSpecStore interface {
	CreateDeliverable(context.Context, artifactmodel.DeliverableSpec) error
	ListDeliverables(context.Context, string, string) ([]artifactmodel.DeliverableSpec, error)
}

type DeliverableSelectionStore interface {
	CreateSelection(context.Context, string, string, artifactmodel.DeliverableSelection) error
	// ReplaceSelection 覆盖已有 selection；用于同 deliverable 在 produced head supersede 后重绑。
	ReplaceSelection(context.Context, string, string, artifactmodel.DeliverableSelection) error
	GetSelection(context.Context, string, string, string) (artifactmodel.DeliverableSelection, error)
}

type OutputReservationStore interface {
	CreateReservation(context.Context, artifactmodel.OutputReservation) error
	ListReservations(context.Context, string, string, string) ([]artifactmodel.OutputReservation, error)
}

// OutputReservationAllocator 由 Harness 在每次 execution attempt 前调用。
// 实现必须持久化 reservation，并只返回逻辑目标与受控 env 绑定，不得暴露物理路径。
type OutputReservationAllocator interface {
	Reserve(context.Context, ReserveRequest) (ReserveResult, error)
}

// ReserveRequest / ReserveResult 放在 contract 以便 skill Harness 不依赖 artifact service 包。
type ReserveRequest struct {
	TenantID  string
	RunID     string
	BindingID string
	AttemptID string
}

type ReserveResult struct {
	Reservations []artifactmodel.OutputReservation
	EnvBindings  map[string]string
}

type QAEvidenceStore interface {
	CreateQAEvidence(context.Context, artifactmodel.QAEvidenceRecord) error
	ListQAEvidence(context.Context, string, string, string) ([]artifactmodel.QAEvidenceRecord, error)
}

type ArtifactPublicationStore interface {
	CreatePublication(context.Context, artifactmodel.ArtifactPublicationRecord) (artifactmodel.ArtifactPublicationRecord, bool, error)
	GetPublication(context.Context, string, string) (artifactmodel.ArtifactPublicationRecord, error)
	GetPublicationByIdempotencyKey(context.Context, string, string) (artifactmodel.ArtifactPublicationRecord, error)
	UpdatePublication(context.Context, uint64, artifactmodel.ArtifactPublicationRecord) (artifactmodel.ArtifactPublicationRecord, error)
	ListPublications(context.Context, string, string, string) ([]artifactmodel.ArtifactPublicationRecord, error)
}

type DeliveryRecordStore interface {
	CreateDelivery(context.Context, artifactmodel.DeliveryRecord) (artifactmodel.DeliveryRecord, bool, error)
	GetDelivery(context.Context, string, string) (artifactmodel.DeliveryRecord, error)
	GetDeliveryByIdempotencyKey(context.Context, string, string) (artifactmodel.DeliveryRecord, error)
	UpdateDelivery(context.Context, uint64, artifactmodel.DeliveryRecord) (artifactmodel.DeliveryRecord, error)
	ListDeliveries(context.Context, string, string, string) ([]artifactmodel.DeliveryRecord, error)
}

type CompletionDecision struct {
	Complete              bool     `json:"complete"`
	MissingDeliverableIDs []string `json:"missing_deliverable_ids,omitempty"`
	PendingQAIDs          []string `json:"pending_qa_ids,omitempty"`
	FailureCodes          []string `json:"failure_codes,omitempty"`
}

type CompletionPolicy interface {
	EvaluateCompletion(context.Context, string, string) (CompletionDecision, error)
}

// DeclaredDeliverable 是 API / CLI / App Template 在 Run 创建时提交的显式交付声明。
// 控制面据此持久化 DeliverableSpec；不得从 produced[] 反推。
type DeclaredDeliverable struct {
	ID             string   `json:"id,omitempty"`
	Required       bool     `json:"required"`
	Role           string   `json:"role,omitempty"`
	DesiredName    string   `json:"desired_name,omitempty"`
	AcceptedMIMEs  []string `json:"accepted_mimes,omitempty"`
	AcceptedSuffix []string `json:"accepted_suffixes,omitempty"`
	QAPolicy       string   `json:"qa_policy,omitempty"`
	DeliveryPolicy string   `json:"delivery_policy,omitempty"`
}

// RunInitializationRequest 是 App 控制面在 Run 创建后提交的可信任务事实。
// Prompt 只供无显式声明时的确定性启发式使用，不能据此授予文件系统权限。
type RunInitializationRequest struct {
	TenantID         string
	RunID            string
	Prompt           string
	ArtifactRequired bool
	// Deliverables 非空时优先持久化显式声明，不再用 Prompt 猜测交付契约。
	Deliverables []DeclaredDeliverable
}

// RunInitializer 在模型开始执行前持久化具体 DeliverableSpec。
type RunInitializer interface {
	InitializeRun(context.Context, RunInitializationRequest) error
}

type RunInitializerFunc func(context.Context, RunInitializationRequest) error
func (f RunInitializerFunc) InitializeRun(ctx context.Context, req RunInitializationRequest) error { return f(ctx, req) }

// RequiredDeliverableFinalizer 由 Harness 调用；自动选择只允许唯一匹配。
type RequiredDeliverableFinalizer interface {
	FinalizeRequired(context.Context, string, string) (artifactmodel.FinalizationResult, error)
	SelectAndFinalize(context.Context, string, string, string, string) (artifactmodel.DeliveryResult, error)
}

type QAPassRequest struct {
	TenantID  string
	RunID     string
	PolicyID  string
	Validator string
}

type QADegradeRequest struct {
	TenantID    string
	RunID       string
	PolicyID    string
	Validator   string
	FailureCode string
	Status      string // degraded | skipped；空则 degraded
}

type QAEvidenceRecorder interface {
	RecordPassed(context.Context, QAPassRequest) error
	RecordDegraded(context.Context, QADegradeRequest) error
}
