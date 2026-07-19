package model

import (
	"fmt"
	"strings"
	"time"

	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type DeliverableRole string

const (
	DeliverableRolePrimary    DeliverableRole = "primary"
	DeliverableRoleSupporting DeliverableRole = "supporting"
)

type PublicationStatus string

const (
	PublicationPending   PublicationStatus = "pending"
	PublicationStaging   PublicationStatus = "staging"
	PublicationGated     PublicationStatus = "gated"
	PublicationCommitted PublicationStatus = "committed"
	PublicationFailed    PublicationStatus = "failed"
)

type DeliveryStatus string

const (
	DeliveryPending    DeliveryStatus = "pending"
	DeliveryDelivering DeliveryStatus = "delivering"
	DeliverySucceeded  DeliveryStatus = "succeeded"
	DeliveryFailed     DeliveryStatus = "failed"
)

type QAEvidenceStatus string

const (
	QAEvidencePending  QAEvidenceStatus = "pending"
	QAEvidencePassed   QAEvidenceStatus = "passed"
	QAEvidenceFailed   QAEvidenceStatus = "failed"
	QAEvidenceDegraded QAEvidenceStatus = "degraded"
	QAEvidenceSkipped  QAEvidenceStatus = "skipped"
)

type DeliverableResolution struct {
	DeliverableID string         `json:"deliverable_id"`
	Status        string         `json:"status"`
	CandidateIDs  []string       `json:"candidate_ids,omitempty"`
	SelectedID    string         `json:"selected_id,omitempty"`
	Delivery      DeliveryResult `json:"delivery,omitempty"`
	// Warning 承载可恢复交付提示（如冲突改名）；不表示终态失败。
	Warning string `json:"warning,omitempty"`
}

type FinalizationResult struct {
	Resolutions []DeliverableResolution `json:"resolutions,omitempty"`
}

// DeliverableSpec 是由任务契约声明的交付要求，而不是从执行输出反推的猜测。
type DeliverableSpec struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	RunID          string          `json:"run_id"`
	Required       bool            `json:"required"`
	Role           DeliverableRole `json:"role"`
	DesiredName    string          `json:"desired_name,omitempty"`
	AcceptedMIMEs  []string        `json:"accepted_mimes,omitempty"`
	AcceptedSuffix []string        `json:"accepted_suffixes,omitempty"`
	QAPolicy       string          `json:"qa_policy,omitempty"`
	DeliveryPolicy string          `json:"delivery_policy"`
	CreatedAt      time.Time       `json:"created_at"`
}

func (v DeliverableSpec) Validate() error {
	if missing(v.ID, v.TenantID, v.RunID, string(v.Role), v.DeliveryPolicy) {
		return fmt.Errorf("deliverable spec 缺少 id/tenant/run/role/delivery policy")
	}
	if v.Role != DeliverableRolePrimary && v.Role != DeliverableRoleSupporting {
		return fmt.Errorf("deliverable role 无效: %q", v.Role)
	}
	if strings.ContainsAny(v.DesiredName, "/\\\x00") {
		return fmt.Errorf("desired name 必须是文件名")
	}
	if v.CreatedAt.IsZero() {
		return fmt.Errorf("deliverable spec 缺少 created_at")
	}
	if err := validateStringSet("accepted mime", v.AcceptedMIMEs); err != nil {
		return err
	}
	return validateStringSet("accepted suffix", v.AcceptedSuffix)
}

// MatchesObserved 判断观测到的文件名/MIME 是否满足本交付契约。
// 未声明 MIME/后缀约束时视为不限制对应维度。
func (v DeliverableSpec) MatchesObserved(name, mediaType string) bool {
	if len(v.AcceptedMIMEs) > 0 {
		ok := false
		for _, item := range v.AcceptedMIMEs {
			if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(mediaType)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(v.AcceptedSuffix) > 0 {
		ok := false
		lower := strings.ToLower(strings.TrimSpace(name))
		for _, item := range v.AcceptedSuffix {
			if strings.HasSuffix(lower, strings.ToLower(strings.TrimSpace(item))) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

type DeliverableSelection struct {
	DeliverableID      string    `json:"deliverable_id"`
	ProducedResourceID string    `json:"produced_resource_id"`
	SelectedBy         string    `json:"selected_by"`
	CreatedAt          time.Time `json:"created_at"`
}

func (v DeliverableSelection) Validate() error {
	if missing(v.DeliverableID, v.ProducedResourceID, v.SelectedBy) || v.CreatedAt.IsZero() {
		return fmt.Errorf("deliverable selection 信息不完整")
	}
	return nil
}

type OutputReservation struct {
	ID            string                  `json:"id"`
	TenantID      string                  `json:"tenant_id"`
	RunID         string                  `json:"run_id"`
	BindingID     string                  `json:"binding_id"`
	DeliverableID string                  `json:"deliverable_id"`
	AttemptID     string                  `json:"attempt_id"`
	LogicalTarget workmodel.WorkspacePath `json:"logical_target"`
	DesiredName   string                  `json:"desired_name,omitempty"`
	MediaType     string                  `json:"media_type,omitempty"`
	CreatedAt     time.Time               `json:"created_at"`
	ExpiresAt     *time.Time              `json:"expires_at,omitempty"`
}

func (v OutputReservation) Validate() error {
	if missing(v.ID, v.TenantID, v.RunID, v.BindingID, v.DeliverableID, v.AttemptID) || v.CreatedAt.IsZero() {
		return fmt.Errorf("output reservation 信息不完整")
	}
	if err := v.LogicalTarget.Validate(); err != nil {
		return err
	}
	if v.ExpiresAt != nil && !v.ExpiresAt.After(v.CreatedAt) {
		return fmt.Errorf("reservation expiry 必须晚于创建时间")
	}
	return nil
}

type QAEvidenceRecord struct {
	ID                  string           `json:"id"`
	TenantID            string           `json:"tenant_id"`
	RunID               string           `json:"run_id"`
	DeliverableID       string           `json:"deliverable_id"`
	ProducedResourceID  string           `json:"produced_resource_id"`
	PublicationID       string           `json:"publication_id,omitempty"`
	SubjectVersion      string           `json:"subject_version"`
	SubjectSHA256       string           `json:"subject_sha256"`
	PolicyID            string           `json:"policy_id"`
	Validator           string           `json:"validator"`
	ValidatorVersion    string           `json:"validator_version"`
	Status              QAEvidenceStatus `json:"status"`
	FailureCode         string           `json:"failure_code,omitempty"`
	EvidenceResourceIDs []string         `json:"evidence_resource_ids,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
}

func (v QAEvidenceRecord) Validate() error {
	if missing(v.ID, v.TenantID, v.RunID, v.DeliverableID, v.ProducedResourceID, v.SubjectVersion, v.SubjectSHA256, v.PolicyID, v.Validator, v.ValidatorVersion) || v.CreatedAt.IsZero() {
		return fmt.Errorf("qa evidence 信息不完整")
	}
	if v.Status != QAEvidencePending && v.Status != QAEvidencePassed && v.Status != QAEvidenceFailed &&
		v.Status != QAEvidenceDegraded && v.Status != QAEvidenceSkipped {
		return fmt.Errorf("qa evidence status 无效")
	}
	if (v.Status == QAEvidenceFailed || v.Status == QAEvidenceDegraded) && strings.TrimSpace(v.FailureCode) == "" {
		return fmt.Errorf("失败 evidence 缺少 failure code")
	}
	return validateStringSet("evidence resource id", v.EvidenceResourceIDs)
}

type ArtifactPublicationRecord struct {
	ID                 string            `json:"id"`
	TenantID           string            `json:"tenant_id"`
	RunID              string            `json:"run_id"`
	ProducedResourceID string            `json:"produced_resource_id"`
	DeliverableID      string            `json:"deliverable_id"`
	DesiredName        string            `json:"desired_name"`
	ArtifactID         string            `json:"artifact_id,omitempty"`
	StagedObjectID     string            `json:"staged_object_id,omitempty"`
	ArtifactKind       string            `json:"artifact_kind,omitempty"`
	DetectedMIME       string            `json:"detected_mime,omitempty"`
	SubjectVersion     string            `json:"subject_version,omitempty"`
	SubjectSHA256      string            `json:"subject_sha256,omitempty"`
	GateVersion        string            `json:"gate_version"`
	IdempotencyKey     string            `json:"idempotency_key"`
	Status             PublicationStatus `json:"status"`
	FailureCode        string            `json:"failure_code,omitempty"`
	// FailureValidator / FailureReason 仅 Gate 拒绝时可选写入，供 Run 历史与失败诊断；Validate 不强制。
	FailureValidator string `json:"failure_validator,omitempty"`
	FailureReason    string `json:"failure_reason,omitempty"`
	Revision         uint64 `json:"revision"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (v ArtifactPublicationRecord) Validate() error {
	if missing(v.ID, v.TenantID, v.RunID, v.ProducedResourceID, v.DeliverableID, v.DesiredName, v.GateVersion, v.IdempotencyKey) || v.Revision == 0 || v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() {
		return fmt.Errorf("publication record 信息不完整")
	}
	if !validPublicationStatus(v.Status) {
		return fmt.Errorf("publication status 无效: %q", v.Status)
	}
	if v.Status == PublicationCommitted && missing(v.ArtifactID, v.SubjectVersion, v.SubjectSHA256) {
		return fmt.Errorf("committed publication 缺少 artifact/source version/hash")
	}
	if v.Status == PublicationFailed && strings.TrimSpace(v.FailureCode) == "" {
		return fmt.Errorf("failed publication 缺少 failure code")
	}
	return nil
}

type DeliveryRecord struct {
	ID             string                `json:"id"`
	TenantID       string                `json:"tenant_id"`
	RunID          string                `json:"run_id"`
	DeliverableID  string                `json:"deliverable_id"`
	PublicationID  string                `json:"publication_id"`
	ArtifactID     string                `json:"artifact_id"`
	Target         workmodel.ResourceRef `json:"target"`
	TargetKind     DeliveryTargetKind    `json:"target_kind"`
	TargetName     string                `json:"target_name"`
	ResultResource workmodel.ResourceRef `json:"result_resource,omitempty"`
	Display        string                `json:"display,omitempty"`
	IdempotencyKey string                `json:"idempotency_key"`
	Status         DeliveryStatus        `json:"status"`
	FailureCode    string                `json:"failure_code,omitempty"`
	Revision       uint64                `json:"revision"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

func (v DeliveryRecord) Validate() error {
	if missing(v.ID, v.TenantID, v.RunID, v.DeliverableID, v.PublicationID, v.ArtifactID, string(v.TargetKind), v.TargetName, v.IdempotencyKey) || v.Revision == 0 || v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() {
		return fmt.Errorf("delivery record 信息不完整")
	}
	if missing(v.Target.Authority, v.Target.Scheme, v.Target.ID) {
		return fmt.Errorf("delivery target 无效")
	}
	if v.Status != DeliveryPending && v.Status != DeliveryDelivering && v.Status != DeliverySucceeded && v.Status != DeliveryFailed {
		return fmt.Errorf("delivery status 无效")
	}
	if v.Status == DeliveryFailed && strings.TrimSpace(v.FailureCode) == "" {
		return fmt.Errorf("failed delivery 缺少 failure code")
	}
	if v.Status == DeliverySucceeded && missing(v.ResultResource.Authority, v.ResultResource.Scheme, v.ResultResource.ID) {
		return fmt.Errorf("succeeded delivery 缺少 result resource")
	}
	return nil
}

func validPublicationStatus(v PublicationStatus) bool {
	return v == PublicationPending || v == PublicationStaging || v == PublicationGated || v == PublicationCommitted || v == PublicationFailed
}

func missing(values ...string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return true
		}
	}
	return false
}
func validateStringSet(name string, values []string) error {
	seen := map[string]struct{}{}
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			return fmt.Errorf("%s 不能为空", name)
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%s 重复: %s", name, value)
		}
		seen[key] = struct{}{}
	}
	return nil
}
