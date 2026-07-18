package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	execmodel "genesis-agent/internal/capabilities/execution/model"
	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
	fsmodel "genesis-agent/internal/capabilities/filesystem/model"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
)

type SessionFileResolver struct {
	files    sandboxcontract.FileSystemClient
	sessions SessionBindingStore
	store    RemoteLocatorStore
	ids      LocatorIDGenerator
	now      func() time.Time
}

func NewSessionFileResolver(files sandboxcontract.FileSystemClient, sessions SessionBindingStore, store RemoteLocatorStore, ids LocatorIDGenerator) (*SessionFileResolver, error) {
	if files == nil || sessions == nil || store == nil || ids == nil {
		return nil, fmt.Errorf("session-file resolver 缺少 files/sessions/store/ids")
	}
	return &SessionFileResolver{files: files, sessions: sessions, store: store, ids: ids, now: time.Now}, nil
}

func (r *SessionFileResolver) ResolveProducedResource(ctx context.Context, req workcontract.BackendResourceRequest) (workmodel.ResourceRef, error) {
	if err := validateRemoteBackend(req.Execution.Backend); err != nil {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, err)
	}
	if req.Execution.Binding.Owner.RunID != req.RunID || req.Execution.Binding.Owner.TenantID != req.TenantID {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("session-file request 与 execution owner 不一致"))
	}
	if req.Availability != workmodel.ResourceAvailabilityLeased || req.ExpiresAt == nil || !req.ExpiresAt.After(r.now()) {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("session-file lease 已过期或缺少 expires_at"))
	}
	if err := req.ObservedPath.Validate(); err != nil {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, err)
	}
	sessionBinding, err := r.sessions.GetSessionExecution(ctx, req.TenantID, req.RunID, req.Execution.Binding.ID)
	if err != nil {
		return workmodel.ResourceRef{}, err
	}
	if !sessionBinding.ExpiresAt.After(r.now()) {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("execution session binding 已过期"))
	}
	if req.ExpiresAt.After(sessionBinding.ExpiresAt) {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("produced lease 超过 execution session lease"))
	}
	workspace := cloneWorkspaceRef(sessionBinding.Workspace)
	if workspace.Provider == "" {
		workspace.Provider = req.Execution.Backend.Provider
	} else if req.Execution.Backend.Provider != "" && workspace.Provider != req.Execution.Backend.Provider {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, fmt.Errorf("session workspace provider 与 execution backend 不一致"))
	}
	resolved := remoteResolvedPath(req.ObservedPath, workspace.ID)
	stat, err := r.files.Stat(ctx, sandboxcontract.FileRequest{Workspace: workspace, Path: resolved})
	if err != nil {
		return workmodel.ResourceRef{}, err
	}
	if stat == nil || stat.Type != fsmodel.EntryTypeFile || stat.IsSymlink || stat.Size != req.Size {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("session-file identity/size 与 discovery 不一致"))
	}
	version, err := sessionFileVersion(ctx, r.files, workspace, resolved, stat)
	if err != nil {
		return workmodel.ResourceRef{}, err
	}
	scope := workmodel.ResourceScope{TenantID: req.Execution.Binding.Owner.TenantID, ProjectID: req.Execution.Binding.Owner.ProjectID, UserID: req.Execution.Binding.Owner.UserID}
	if scope.TenantID != req.TenantID {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeCrossExecutionResourceDenied, fmt.Errorf("remote produced scope 与请求 tenant 不一致"))
	}
	if ref, ok, reuseErr := r.reuseSessionFileLocator(ctx, req, workspace, scope, version, stat.Size); reuseErr != nil {
		return workmodel.ResourceRef{}, reuseErr
	} else if ok {
		return ref, nil
	}
	id := "remote-locator-" + r.ids.Generate()
	locator := RemoteLocator{ID: id, Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme, Workspace: workspace, Path: req.ObservedPath, Scope: scope, Version: version, MediaType: req.MediaType, Size: stat.Size, Availability: req.Availability, ExpiresAt: cloneRemoteTime(req.ExpiresAt)}
	if err := r.store.Create(ctx, locator); err != nil {
		return workmodel.ResourceRef{}, err
	}
	return workmodel.ResourceRef{Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme, ID: id, Version: version, MediaType: req.MediaType, Scope: locator.Scope}, nil
}

// reuseSessionFileLocator 在 PreferSource 内容指纹仍匹配时复用 locator，并刷新 lease。
func (r *SessionFileResolver) reuseSessionFileLocator(ctx context.Context, req workcontract.BackendResourceRequest, workspace sandboxcontract.WorkspaceRef, scope workmodel.ResourceScope, version string, size int64) (workmodel.ResourceRef, bool, error) {
	prefer := req.PreferSource
	if prefer == nil {
		return workmodel.ResourceRef{}, false, nil
	}
	if prefer.Authority != RemoteExecutorAuthority || prefer.Scheme != SessionFileScheme || prefer.Version != version || prefer.Scope != scope || strings.TrimSpace(prefer.ID) == "" {
		return workmodel.ResourceRef{}, false, nil
	}
	existing, err := r.store.Get(ctx, prefer.ID, scope)
	if err != nil {
		if workspaceErrorCode(err) == workcontract.ErrCodeProducedResourceNotFound {
			return workmodel.ResourceRef{}, false, nil
		}
		return workmodel.ResourceRef{}, false, err
	}
	if existing.Path != req.ObservedPath || existing.Size != size || existing.Workspace.ID != workspace.ID || existing.Version != version {
		return workmodel.ResourceRef{}, false, nil
	}
	existing.ExpiresAt = cloneRemoteTime(req.ExpiresAt)
	if media := strings.TrimSpace(req.MediaType); media != "" {
		existing.MediaType = media
	}
	if err := r.store.Put(ctx, existing); err != nil {
		return workmodel.ResourceRef{}, false, err
	}
	return workmodel.ResourceRef{Authority: RemoteExecutorAuthority, Scheme: SessionFileScheme, ID: existing.ID, Version: version, MediaType: existing.MediaType, Scope: scope}, true, nil
}

// ExecutorObjectResolver persists transport output-object identity separately
// from the ProducedResource descriptor.
type ExecutorObjectResolver struct {
	store RemoteLocatorStore
	ids   LocatorIDGenerator
}

func NewExecutorObjectResolver(store RemoteLocatorStore, ids LocatorIDGenerator) (*ExecutorObjectResolver, error) {
	if store == nil || ids == nil {
		return nil, fmt.Errorf("executor-object resolver 缺少 store/ids")
	}
	return &ExecutorObjectResolver{store: store, ids: ids}, nil
}

func (r *ExecutorObjectResolver) Resolve(ctx context.Context, backend execmodel.ExecutionBackendRef, scope workmodel.ResourceScope, output execmodel.ExecutorOutputObject) (workmodel.ResourceRef, error) {
	return r.resolveObject(ctx, backend, scope, output, nil)
}

func (r *ExecutorObjectResolver) resolveObject(ctx context.Context, backend execmodel.ExecutionBackendRef, scope workmodel.ResourceScope, output execmodel.ExecutorOutputObject, prefer *workmodel.ResourceRef) (workmodel.ResourceRef, error) {
	if err := validateRemoteBackend(backend); err != nil {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceBackendMismatch, err)
	}
	version := strings.TrimSpace(output.Version)
	if version == "" && strings.TrimSpace(output.SHA256) != "" {
		version = "sha256:" + normalizeSHA256(output.SHA256)
	}
	if strings.TrimSpace(output.ID) == "" || output.ID != strings.TrimSpace(output.ID) || version == "" || output.Size < 0 || output.Availability != string(workmodel.ResourceAvailabilityDurable) {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("executor output object 的 id/version/size/durable availability 无效"))
	}
	if output.ExpiresAt != nil && !output.ExpiresAt.After(time.Now()) {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceExpired, fmt.Errorf("executor output object 已过期"))
	}
	if prefer != nil && prefer.Authority == RemoteExecutorAuthority && prefer.Scheme == ExecutorObjectScheme && prefer.Version == version && prefer.Scope == scope && strings.TrimSpace(prefer.ID) != "" {
		existing, err := r.store.Get(ctx, prefer.ID, scope)
		if err == nil && existing.ObjectID == output.ID && existing.Size == output.Size && existing.Version == version {
			return workmodel.ResourceRef{Authority: RemoteExecutorAuthority, Scheme: ExecutorObjectScheme, ID: existing.ID, Version: version, MediaType: existing.MediaType, Scope: scope}, nil
		}
		if err != nil && workspaceErrorCode(err) != workcontract.ErrCodeProducedResourceNotFound {
			return workmodel.ResourceRef{}, err
		}
	}
	id := "remote-locator-" + r.ids.Generate()
	locator := RemoteLocator{ID: id, Authority: RemoteExecutorAuthority, Scheme: ExecutorObjectScheme, ObjectID: output.ID, Scope: scope, Version: version, MediaType: output.MediaType, Size: output.Size, Availability: workmodel.ResourceAvailabilityDurable, ExpiresAt: cloneRemoteTime(output.ExpiresAt)}
	if err := r.store.Create(ctx, locator); err != nil {
		return workmodel.ResourceRef{}, err
	}
	return workmodel.ResourceRef{Authority: RemoteExecutorAuthority, Scheme: ExecutorObjectScheme, ID: id, Version: version, MediaType: output.MediaType, Scope: scope}, nil
}

// ResolveProducedResource 把 Harness 登记的 durable produced 请求转换为 executor-object locator。
// ObservedPath 须为规范化相对路径 object/<opaque-id>；版本 token 由 Registrar 传入的 Size/MediaType 约束，
// 完整 hash 须由提升端口在调用 Resolve 前写入 ExecutorOutputObject。
func (r *ExecutorObjectResolver) ResolveProducedResource(ctx context.Context, req workcontract.BackendResourceRequest) (workmodel.ResourceRef, error) {
	if req.Availability != workmodel.ResourceAvailabilityDurable {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("executor-object resolver 只接受 durable availability"))
	}
	objectID, ok := parseExecutorObjectObservedPath(req.ObservedPath)
	if !ok {
		return workmodel.ResourceRef{}, workcontract.NewError(workcontract.ErrCodeProducedResourceInvalid, fmt.Errorf("durable executor object 缺少 object/<id> ObservedPath"))
	}
	scope := workmodel.ResourceScope{
		TenantID:  req.Execution.Binding.Owner.TenantID,
		ProjectID: req.Execution.Binding.Owner.ProjectID,
		UserID:    req.Execution.Binding.Owner.UserID,
	}
	version := fmt.Sprintf("size:%d", req.Size)
	return r.resolveObject(ctx, req.Execution.Backend, scope, execmodel.ExecutorOutputObject{
		ID:           objectID,
		Name:         req.ObservedName,
		Size:         req.Size,
		MediaType:    req.MediaType,
		Version:      version,
		Availability: string(workmodel.ResourceAvailabilityDurable),
		ExpiresAt:    cloneRemoteTime(req.ExpiresAt),
	}, req.PreferSource)
}

func parseExecutorObjectObservedPath(value workmodel.WorkspacePath) (string, bool) {
	path := strings.TrimPrefix(strings.TrimSpace(string(value)), "/")
	if !strings.HasPrefix(path, "object/") {
		return "", false
	}
	objectID := strings.TrimSpace(strings.TrimPrefix(path, "object/"))
	if objectID == "" || objectID != strings.TrimSpace(objectID) || strings.Contains(objectID, "/") {
		return "", false
	}
	return objectID, true
}

func sessionFileVersion(ctx context.Context, files sandboxcontract.FileSystemClient, workspace sandboxcontract.WorkspaceRef, resolved fsmodel.ResolvedPath, stat *fsmodel.FileStat) (string, error) {
	hash := strings.TrimPrefix(strings.TrimSpace(stat.Hash), "sha256:")
	if hash != "" {
		return "sha256:" + strings.ToLower(hash), nil
	}
	content, err := files.ReadFile(ctx, sandboxcontract.FileRequest{Workspace: workspace, Path: resolved}, fscontract.ReadOptions{MaxBytes: stat.Size + 1})
	if err != nil {
		return "", err
	}
	if int64(len(content)) != stat.Size {
		return "", workcontract.NewError(workcontract.ErrCodeProducedResourceVersionConflict, fmt.Errorf("session-file size 在版本化期间变化"))
	}
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func remoteResolvedPath(value workmodel.WorkspacePath, workspaceID string) fsmodel.ResolvedPath {
	path := string(value)
	return fsmodel.ResolvedPath{DisplayPath: path, WorkspaceRel: path, WorkspaceID: workspaceID, Scope: fsmodel.PathScopeWorkspace, RawPath: path}
}

func cloneRemoteTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func workspaceErrorCode(err error) workcontract.ErrorCode {
	var classified *workcontract.Error
	if errors.As(err, &classified) {
		return classified.Code
	}
	return ""
}

func cloneWorkspaceRef(value sandboxcontract.WorkspaceRef) sandboxcontract.WorkspaceRef {
	out := sandboxcontract.WorkspaceRef{ID: strings.TrimSpace(value.ID), Provider: strings.TrimSpace(value.Provider)}
	// Persist only backend identity fields needed to reopen WorkspaceFS. Arbitrary
	// metadata can contain credentials or request-local data and must not enter a
	// durable locator.
	for _, key := range []string{"session_id", "workspace_id", "sandbox_id"} {
		if item := strings.TrimSpace(value.Metadata[key]); item != "" {
			if out.Metadata == nil {
				out.Metadata = make(map[string]string, 3)
			}
			out.Metadata[key] = item
		}
	}
	return out
}
