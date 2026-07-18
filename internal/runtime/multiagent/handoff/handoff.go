// Package handoff 提供跨 execution 的显式资源交接控制面。
package handoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// BindingLocator 只用控制面身份定位 binding，不携带可伪造 owner。
type BindingLocator struct {
	RunID     string `json:"run_id"`
	BindingID string `json:"binding_id"`
}

// Request 描述一次从 source execution 到 target execution 的资源交接。
type Request struct {
	TenantID       string                      `json:"tenant_id"`
	IdempotencyKey string                      `json:"idempotency_key"`
	Source         BindingLocator              `json:"source"`
	Target         BindingLocator              `json:"target"`
	Resources      []workmodel.ResourceRef     `json:"resources,omitempty"`
	Artifacts      []artifactmodel.ArtifactRef `json:"artifacts,omitempty"`
}

// Receipt 是不可变 Handoff 证据；其中只保存稳定引用，不保存物理 cwd 或 credential。
type Receipt struct {
	ID             string                      `json:"id"`
	TenantID       string                      `json:"tenant_id"`
	IdempotencyKey string                      `json:"idempotency_key"`
	Source         BindingLocator              `json:"source"`
	Target         BindingLocator              `json:"target"`
	Resources      []workmodel.ResourceRef     `json:"resources,omitempty"`
	Artifacts      []artifactmodel.ArtifactRef `json:"artifacts,omitempty"`
	Fingerprint    string                      `json:"fingerprint"`
	CreatedAt      time.Time                   `json:"created_at"`
}

// AuthorizationRequest 是 source 导出与 target 接收的逐资源重新鉴权请求。
type AuthorizationRequest struct {
	Source   execmodel.ExecutionBinding
	Target   execmodel.ExecutionBinding
	Resource *workmodel.ResourceRef
	Artifact *artifactmodel.ArtifactRef
}

type Authorizer interface {
	// AuthorizeTransfer 必须同时验证 source 对精确资源版本的导出权和 target 的接收权。
	AuthorizeTransfer(ctx context.Context, req AuthorizationRequest) error
}

// Store 必须按 tenant/idempotency key 原子排他写入。
type Store interface {
	PutIfAbsent(ctx context.Context, receipt Receipt) (stored Receipt, created bool, err error)
}

type IDGenerator interface{ Generate() string }

type Service struct {
	control workcontract.ControlPlane
	auth    Authorizer
	store   Store
	ids     IDGenerator
	now     func() time.Time
}

func New(control workcontract.ControlPlane, auth Authorizer, store Store, ids IDGenerator) (*Service, error) {
	if control == nil || auth == nil || store == nil || ids == nil {
		return nil, fmt.Errorf("handoff service 缺少 control/authorizer/store/id generator")
	}
	return &Service{control: control, auth: auth, store: store, ids: ids, now: time.Now}, nil
}

// Transfer 解析双方权威 binding、逐项重新鉴权并原子创建 receipt。
func (s *Service) Transfer(ctx context.Context, req Request) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.Source = normalizeLocator(req.Source)
	req.Target = normalizeLocator(req.Target)
	if req.TenantID == "" || req.IdempotencyKey == "" {
		return Receipt{}, fmt.Errorf("handoff tenant_id/idempotency_key 不能为空")
	}
	if len(req.Resources) == 0 && len(req.Artifacts) == 0 {
		return Receipt{}, fmt.Errorf("handoff 至少需要一个 ResourceRef 或 ArtifactRef")
	}
	source, err := s.resolveBinding(ctx, req.TenantID, req.Source)
	if err != nil {
		return Receipt{}, fmt.Errorf("解析 handoff source: %w", err)
	}
	target, err := s.resolveBinding(ctx, req.TenantID, req.Target)
	if err != nil {
		return Receipt{}, fmt.Errorf("解析 handoff target: %w", err)
	}
	if source.ID == target.ID && source.Owner.RunID == target.Owner.RunID {
		return Receipt{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("handoff source 与 target 不能是同一 execution"))
	}
	resources := append([]workmodel.ResourceRef(nil), req.Resources...)
	artifacts := append([]artifactmodel.ArtifactRef(nil), req.Artifacts...)
	for i := range resources {
		if err := validateResource(source, resources[i]); err != nil {
			return Receipt{}, err
		}
		item := resources[i]
		if err := s.auth.AuthorizeTransfer(ctx, AuthorizationRequest{Source: source, Target: target, Resource: &item}); err != nil {
			return Receipt{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("ResourceRef %s 交接未获双方授权: %w", item.ID, err))
		}
	}
	for i := range artifacts {
		if err := validateArtifact(source, artifacts[i]); err != nil {
			return Receipt{}, err
		}
		item := artifacts[i]
		if err := s.auth.AuthorizeTransfer(ctx, AuthorizationRequest{Source: source, Target: target, Artifact: &item}); err != nil {
			return Receipt{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("ArtifactRef %s 交接未获双方授权: %w", item.ID, err))
		}
	}
	sort.Slice(resources, func(i, j int) bool { return resourceKey(resources[i]) < resourceKey(resources[j]) })
	sort.Slice(artifacts, func(i, j int) bool { return artifactKey(artifacts[i]) < artifactKey(artifacts[j]) })
	fingerprint, err := requestFingerprint(req.TenantID, req.IdempotencyKey, req.Source, req.Target, resources, artifacts)
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{ID: "handoff-" + s.ids.Generate(), TenantID: req.TenantID, IdempotencyKey: req.IdempotencyKey, Source: req.Source, Target: req.Target, Resources: resources, Artifacts: artifacts, Fingerprint: fingerprint, CreatedAt: s.now().UTC()}
	stored, created, err := s.store.PutIfAbsent(ctx, receipt)
	if err != nil {
		return Receipt{}, fmt.Errorf("持久化 handoff receipt: %w", err)
	}
	if !created && stored.Fingerprint != fingerprint {
		return Receipt{}, workcontract.NewError(workcontract.ErrCodeResourceVersionConflict, fmt.Errorf("handoff idempotency key 已绑定不同载荷"))
	}
	if err := validateStoredReceipt(stored, receipt); err != nil {
		return Receipt{}, fmt.Errorf("持久化 handoff receipt: %w", err)
	}
	return cloneReceipt(stored), nil
}

func validateStoredReceipt(stored, expected Receipt) error {
	if !canonicalText(stored.ID) || stored.TenantID != expected.TenantID || stored.IdempotencyKey != expected.IdempotencyKey || stored.Source != expected.Source || stored.Target != expected.Target {
		return fmt.Errorf("store 返回的 handoff receipt 身份与请求不一致")
	}
	fingerprint, err := requestFingerprint(stored.TenantID, stored.IdempotencyKey, stored.Source, stored.Target, stored.Resources, stored.Artifacts)
	if err != nil {
		return err
	}
	if stored.Fingerprint != fingerprint {
		return fmt.Errorf("store 返回的 handoff receipt 指纹校验失败")
	}
	return nil
}

func cloneReceipt(receipt Receipt) Receipt {
	receipt.Resources = append([]workmodel.ResourceRef(nil), receipt.Resources...)
	receipt.Artifacts = append([]artifactmodel.ArtifactRef(nil), receipt.Artifacts...)
	return receipt
}

func (s *Service) resolveBinding(ctx context.Context, tenantID string, locator BindingLocator) (execmodel.ExecutionBinding, error) {
	if locator.RunID == "" || locator.BindingID == "" {
		return execmodel.ExecutionBinding{}, fmt.Errorf("run_id/binding_id 不能为空")
	}
	manifest, err := s.control.GetRunManifest(ctx, tenantID, locator.RunID)
	if err != nil {
		return execmodel.ExecutionBinding{}, err
	}
	for _, execution := range manifest.Executions {
		if execution.Binding.ID == locator.BindingID {
			return execution.Binding, nil
		}
	}
	return execmodel.ExecutionBinding{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("binding %s 不属于 Run %s", locator.BindingID, locator.RunID))
}

func validateResource(source execmodel.ExecutionBinding, ref workmodel.ResourceRef) error {
	if !canonicalText(ref.Authority) || !canonicalText(ref.Scheme) || !canonicalText(ref.ID) || !canonicalText(ref.Version) {
		return workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("handoff ResourceRef 缺少 authority/scheme/id/version"))
	}
	if !scopeOwnedBy(ref.Scope, source.Owner) {
		return workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("ResourceRef %s 不属于 source execution scope", ref.ID))
	}
	if err := validateLogicalPath(ref.Path); err != nil {
		return workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("ResourceRef %s: %w", ref.ID, err))
	}
	return nil
}

func validateArtifact(source execmodel.ExecutionBinding, artifact artifactmodel.ArtifactRef) error {
	if !canonicalText(artifact.ID) || !validArtifactName(artifact.Name) || !canonicalText(artifact.SHA256) || !canonicalText(artifact.RunID) || !canonicalText(artifact.Producer) || artifact.Size < 0 {
		return workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("handoff ArtifactRef 缺少 id/name/hash/run_id"))
	}
	if artifact.RunID != source.Owner.RunID {
		return workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("ArtifactRef %s 不是 source Run 产物", artifact.ID))
	}
	if !scopeOwnedBy(artifact.Scope, source.Owner) || artifact.StorageRef.Scope != artifact.Scope {
		return workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("ArtifactRef %s scope 不属于 source execution", artifact.ID))
	}
	if !canonicalText(artifact.StorageRef.Authority) || !canonicalText(artifact.StorageRef.Scheme) || !canonicalText(artifact.StorageRef.ID) || !canonicalText(artifact.StorageRef.Version) {
		return workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("ArtifactRef %s 缺少稳定 StorageRef", artifact.ID))
	}
	if !validSHA256(artifact.SHA256) || artifact.StorageRef.Version != "sha256:"+strings.ToLower(artifact.SHA256) {
		return workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("ArtifactRef %s 哈希或 StorageRef 版本无效", artifact.ID))
	}
	if err := validateLogicalPath(artifact.StorageRef.Path); err != nil {
		return workcontract.NewError(workcontract.ErrCodePathNamespaceMismatch, fmt.Errorf("ArtifactRef %s storage path: %w", artifact.ID, err))
	}
	return nil
}

func validArtifactName(value string) bool {
	return canonicalText(value) && value != "." && value != ".." && !strings.ContainsAny(value, `/\`)
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func scopeOwnedBy(scope workmodel.ResourceScope, owner execmodel.ExecutionOwnerRef) bool {
	if scope.TenantID != owner.TenantID {
		return false
	}
	if scope.ProjectID != "" && scope.ProjectID != owner.ProjectID {
		return false
	}
	return scope.UserID == "" || scope.UserID == owner.UserID
}

func validateLogicalPath(value string) error {
	if value != strings.TrimSpace(value) || strings.Contains(value, `\`) {
		return fmt.Errorf("handoff path 必须使用规范化相对路径")
	}
	if value == "" {
		return nil
	}
	if filepath.IsAbs(value) || (len(value) >= 2 && value[1] == ':') {
		return fmt.Errorf("handoff 禁止物理绝对路径")
	}
	return workmodel.WorkspacePath(value).Validate()
}

func normalizeLocator(locator BindingLocator) BindingLocator {
	return BindingLocator{RunID: strings.TrimSpace(locator.RunID), BindingID: strings.TrimSpace(locator.BindingID)}
}

func canonicalText(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func resourceKey(ref workmodel.ResourceRef) string {
	return ref.Authority + "\x00" + ref.Scheme + "\x00" + ref.ID + "\x00" + ref.Version
}

func artifactKey(ref artifactmodel.ArtifactRef) string { return ref.ID + "\x00" + ref.SHA256 }

func requestFingerprint(tenantID, key string, source, target BindingLocator, resources []workmodel.ResourceRef, artifacts []artifactmodel.ArtifactRef) (string, error) {
	payload := struct {
		TenantID  string
		Key       string
		Source    BindingLocator
		Target    BindingLocator
		Resources []workmodel.ResourceRef
		Artifacts []artifactmodel.ArtifactRef
	}{tenantID, key, source, target, resources, artifacts}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("编码 handoff fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
