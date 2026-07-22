// Package contract 定义 Artifact 发布与交付端口。
package contract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
)

// ErrorCode 是 Artifact/Delivery 稳定错误码。
type ErrorCode string

const (
	ErrCodeArtifactPathInvalid           ErrorCode = "ARTIFACT_PATH_INVALID"
	ErrCodeArtifactInvalid               ErrorCode = "ARTIFACT_INVALID"
	ErrCodeArtifactNameConflict          ErrorCode = "ARTIFACT_NAME_CONFLICT"
	ErrCodeDeliveryTargetDenied          ErrorCode = "DELIVERY_TARGET_DENIED"
	ErrCodeDeliveryTargetConflict        ErrorCode = "DELIVERY_TARGET_CONFLICT"
	ErrCodeDeliveryMaterializeFailed     ErrorCode = "DELIVERY_MATERIALIZE_FAILED"
	ErrCodeDeliveryInProgress            ErrorCode = "DELIVERY_IN_PROGRESS"
	ErrCodeArtifactDeliveryRequired      ErrorCode = "ARTIFACT_DELIVERY_REQUIRED"
	ErrCodeDeliverableNotProduced        ErrorCode = "DELIVERABLE_NOT_PRODUCED"
	ErrCodeDeliverableSelectionAmbiguous ErrorCode = "DELIVERABLE_SELECTION_AMBIGUOUS"
	ErrCodeArtifactPublicationConflict   ErrorCode = "ARTIFACT_PUBLICATION_CONFLICT"
	ErrCodeQARequired                    ErrorCode = "QA_REQUIRED"
)

type Error struct {
	Code      ErrorCode
	Err       error
	Artifact  *artifactmodel.ArtifactRef
	Validator string // Gate 拒绝时的 validator 名；非 Gate 错误为空
	Reason    string // Gate 拒绝时的稳定 reason；非 Gate 错误为空
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Validator) != "" || strings.TrimSpace(e.Reason) != "" {
		return fmt.Sprintf("%s: validator=%s reason=%s: %v", e.Code, e.Validator, e.Reason, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}
func (e *Error) Unwrap() error { return e.Err }

func NewError(code ErrorCode, err error) error {
	if err == nil {
		err = errors.New(string(code))
	}
	return &Error{Code: code, Err: err}
}

// NewGateError 构造带结构化 validator/reason 的 ARTIFACT_INVALID。
func NewGateError(validator, reason string, err error) error {
	if err == nil {
		err = errors.New(string(ErrCodeArtifactInvalid))
	}
	return &Error{Code: ErrCodeArtifactInvalid, Err: err, Validator: strings.TrimSpace(validator), Reason: strings.TrimSpace(reason)}
}

// StagedObject 是不可见 quarantine 对象。
type StagedObject struct {
	ID   string
	Name string
}

// Store 实现 Artifact 两阶段提交。
type Store interface {
	Stage(ctx context.Context, artifactID, name string, content io.Reader) (StagedObject, error)
	OpenStaged(ctx context.Context, object StagedObject) (io.ReadCloser, error)
	Commit(ctx context.Context, object StagedObject, manifest artifactmodel.Manifest) (artifactmodel.ArtifactRef, error)
	Abort(ctx context.Context, object StagedObject) error
	Open(ctx context.Context, artifact artifactmodel.ArtifactRef) (io.ReadCloser, error)
}

// TransactionalStore 扩展两阶段 Store 的恢复能力。新版 PublicationService 只依赖该强端口，
// 以便在 Artifact 已提交但 ledger 尚未更新时按确定性 Artifact ID 恢复。
type TransactionalStore interface {
	Store
	GetCommitted(ctx context.Context, artifactID string) (artifactmodel.ArtifactRef, bool, error)
}

// Gate 对 quarantine 内容执行格式、安全和业务基础校验。
type Gate interface {
	Version() string
	Validate(ctx context.Context, name, declaredMIME string, size int64, content io.Reader) (kind, detectedMIME string, err error)
}

// Materializer 将已发布 Artifact 原子导出到用户目标。
type Materializer interface {
	Materialize(ctx context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, error)
}

// RecoverableMaterializer 支持在响应丢失或进程恢复后确认目标是否已由同一 Artifact 完成物化。
type RecoverableMaterializer interface {
	Materializer
	GetMaterialized(ctx context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, bool, error)
	// ReplaceMaterialize 原子覆盖已存在的同名目标。
	// 仅允许 DeliveryService 在「同一 deliverable 先前已成功交付到该目标」时调用（supersede 重交付）。
	ReplaceMaterialize(ctx context.Context, artifact artifactmodel.ArtifactRef, target artifactmodel.DeliveryTarget) (artifactmodel.DeliveryResult, error)
}

// DeliveryTargetPlanner 只根据受信 DeliverableSpec policy 解析目标；模型不参与路径选择。
type DeliveryTargetPlanner interface {
	PlanDelivery(ctx context.Context, spec artifactmodel.DeliverableSpec, artifact artifactmodel.ArtifactRef) (artifactmodel.DeliveryTarget, error)
}
