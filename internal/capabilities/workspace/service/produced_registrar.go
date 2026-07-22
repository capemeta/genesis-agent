package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

// ProducedResourceRegistrar validates trusted discovery facts and persists an immutable descriptor.
// 同 logical_ref：内容指纹相同则幂等返回原 descriptor；内容变更则 UpsertCurrent 推进 head。
type ProducedResourceRegistrar struct {
	manifests workcontract.RunManifestStore
	store     workcontract.ProducedResourceStore
	resolver  workcontract.BackendResourceResolver
	ids       IDGenerator
	now       func() time.Time
}

func NewProducedResourceRegistrar(manifests workcontract.RunManifestStore, store workcontract.ProducedResourceStore, resolver workcontract.BackendResourceResolver, ids IDGenerator) (*ProducedResourceRegistrar, error) {
	if manifests == nil || store == nil || resolver == nil || ids == nil {
		return nil, fmt.Errorf("produced resource registrar 缺少 manifests/store/resolver/id generator")
	}
	return &ProducedResourceRegistrar{manifests: manifests, store: store, resolver: resolver, ids: ids, now: time.Now}, nil
}

func (r *ProducedResourceRegistrar) RegisterProducedResource(ctx context.Context, req workcontract.RegisterProducedResourceRequest) (workmodel.ProducedResourceDescriptor, error) {
	if err := validateProducedRegistration(req, r.now().UTC()); err != nil {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	manifest, err := r.manifests.Get(ctx, req.TenantID, req.RunID)
	if err != nil {
		return workmodel.ProducedResourceDescriptor{}, err
	}
	if manifest.Scope.TenantID != req.TenantID || manifest.RunID != req.RunID {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("produced resource 不属于请求的 tenant/run"))
	}
	execution, ok := findProducedExecution(manifest, req.BindingID)
	if !ok {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("produced resource binding 不在 Run manifest"))
	}
	var existing *workmodel.ProducedResourceDescriptor
	if head, getErr := r.store.GetByLogicalRef(ctx, req.TenantID, req.RunID, req.LogicalRef); getErr == nil {
		existing = &head
	} else if !workspaceErrorIs(getErr, workcontract.ErrCodeProducedResourceNotFound) {
		return workmodel.ProducedResourceDescriptor{}, getErr
	}
	resolverReq := workcontract.BackendResourceRequest{
		TenantID: req.TenantID, RunID: req.RunID, Execution: execution,
		LogicalRef: req.LogicalRef, ObservedPath: req.ObservedPath, ObservedName: req.ObservedName,
		MediaType: req.MediaType, Size: req.Size, Availability: req.Availability, ExpiresAt: cloneTime(req.ExpiresAt),
	}
	if existing != nil {
		src := existing.Source
		resolverReq.PreferSource = &src
	}
	source, err := r.resolver.ResolveProducedResource(ctx, resolverReq)
	if err != nil {
		return workmodel.ProducedResourceDescriptor{}, err
	}
	if source.Authority != execution.Backend.Authority {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, fmt.Errorf("source authority %q 与 backend authority %q 不一致", source.Authority, execution.Backend.Authority))
	}
	if source.Scope != manifest.Scope {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("source scope 与 Run manifest 不一致"))
	}
	// 不再「创建即全局登记」：跨 Run 可读性只来自父子边界的一次显式 Adopt（见 artifactservice.AdoptionStore）。
	if existing != nil && producedContentEqual(*existing, req, source) {
		return *existing, nil
	}
	descriptor := workmodel.ProducedResourceDescriptor{
		ID: "produced-" + r.ids.Generate(), TenantID: req.TenantID, RunID: req.RunID, BindingID: req.BindingID,
		LogicalRef: req.LogicalRef, Source: source, ObservedName: req.ObservedName, MediaType: req.MediaType, Size: req.Size,
		Availability: req.Availability, ExpiresAt: cloneTime(req.ExpiresAt), CreatedAt: r.now().UTC(),
	}
	if err := descriptor.Validate(); err != nil {
		return workmodel.ProducedResourceDescriptor{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	if err := r.store.UpsertCurrent(ctx, descriptor); err != nil {
		return workmodel.ProducedResourceDescriptor{}, err
	}
	return descriptor, nil
}

// producedContentEqual 用内容指纹判断是否同一产出版本。
// 不比较 Source.ID：resolver 每次可能分配新 locator，但 Version/Size 才代表文件内容。
// leased 的 ExpiresAt 必须一致；租约被拉长时应走 UpsertCurrent，避免 head 仍挂旧快照。
func producedContentEqual(existing workmodel.ProducedResourceDescriptor, req workcontract.RegisterProducedResourceRequest, source workmodel.ResourceRef) bool {
	if existing.BindingID != req.BindingID ||
		existing.ObservedName != req.ObservedName ||
		existing.Size != req.Size ||
		existing.Availability != req.Availability ||
		!strings.EqualFold(strings.TrimSpace(existing.MediaType), strings.TrimSpace(req.MediaType)) ||
		existing.Source.Version != source.Version ||
		existing.Source.Authority != source.Authority ||
		existing.Source.Scheme != source.Scheme {
		return false
	}
	if req.Availability == workmodel.ResourceAvailabilityLeased {
		return timePtrEqual(existing.ExpiresAt, req.ExpiresAt)
	}
	return true
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func validateProducedRegistration(req workcontract.RegisterProducedResourceRequest, now time.Time) error {
	if strings.TrimSpace(req.TenantID) != req.TenantID || strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.BindingID) == "" {
		return fmt.Errorf("registration tenant/run/binding 无效")
	}
	if req.LogicalRef != strings.TrimSpace(req.LogicalRef) || !strings.HasPrefix(req.LogicalRef, "run:/") {
		return fmt.Errorf("registration logical_ref 无效")
	}
	logicalPath := strings.TrimPrefix(req.LogicalRef, "run:/")
	if err := workmodel.WorkspacePath(logicalPath).Validate(); err != nil {
		return fmt.Errorf("registration logical_ref 无效: %w", err)
	}
	parts := strings.Split(logicalPath, "/")
	if len(parts) < 3 || parts[0] != "work" || parts[1] != req.BindingID {
		return fmt.Errorf("registration logical_ref 与 binding 不一致")
	}
	if err := req.ObservedPath.Validate(); err != nil {
		return err
	}
	if req.Size < 0 || strings.TrimSpace(req.ObservedName) == "" || req.ObservedName != strings.TrimSpace(req.ObservedName) {
		return fmt.Errorf("registration name/size 无效")
	}
	if req.Availability == workmodel.ResourceAvailabilityLeased && (req.ExpiresAt == nil || !req.ExpiresAt.After(now)) {
		return fmt.Errorf("leased resource 必须具有未来 expires_at")
	}
	if req.Availability != workmodel.ResourceAvailabilityLeased && req.Availability != workmodel.ResourceAvailabilityDurable {
		return fmt.Errorf("registration availability 无效")
	}
	return nil
}

func findProducedExecution(manifest workmodel.RunManifest, bindingID string) (workmodel.PreparedExecutionSnapshot, bool) {
	for _, execution := range manifest.Executions {
		if execution.Binding.ID == bindingID {
			return execution, true
		}
	}
	return workmodel.PreparedExecutionSnapshot{}, false
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func workspaceErrorIs(err error, code workcontract.ErrorCode) bool {
	var classified *workcontract.Error
	return errors.As(err, &classified) && classified.Code == code
}
